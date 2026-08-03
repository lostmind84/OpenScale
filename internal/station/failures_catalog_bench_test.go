package station

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"openscale/internal/catalog"
	"openscale/internal/catalog/importer"
	"openscale/internal/catalog/localdrop"
	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/station/ports"
	"openscale/internal/store"
)

// The bench of the REAL chain: the local drop of §10.1 watching a real directory, the
// Odoo parser of §10.2 reading the two authentic files, the qualification of §10.3, the
// guards of §10.4, the quarantine of §10.5 in SQLite, the image store of §10.7 on disk
// and the transaction of §10.9 — assembled once, driven by the tests of
// failures_catalog_real_test.go.

// --- The same seven, against the real chain ---------------------------------

// fixtures is where the two authentic exchange files live (CLAUDE.md).
const fixtures = "../../testdata/catalog/"

// dropInterval is catalog.options.poll_interval_s of the shipped configuration, and
// advancing the injected clock by that much is one scan of the drop directory.
const dropInterval = 5 * time.Second

// The two inventories, MEASURED on the files of the repository and not copied from the
// document. §16.2 line 12 bis states « 331 tuiles dont 174 sans photo » and §18 « 181
// avec photo et 174 sans » — but 181 + 174 = 355, which is the number of ROWS. The tile
// split is 177 with a photo and 154 without, and both are asserted below so that
// nobody has to take that on trust.
const (
	realRows, realTiles           = 355, 331
	realNotWeighable, realIssues  = 8, 16
	realUnitMismatch              = 1
	realPhotoRows, realPhotoFiles = 181, 165
	realTilesWithPhoto            = 177
	realTilesWithoutPhoto         = 154
	realOtherTiles                = 126

	firstRows, firstTiles = 153, 107
)

// technicalBridge lets a driver of internal/catalog write into the very sink the
// station writes to, so that "no red light" is one assertion and not two.
type technicalBridge struct{ sink *recordingTechnical }

// Technical records one line.
func (b technicalBridge) Technical(level, source, code, message, detail string) {
	_ = b.sink.RecordTechnical(context.Background(), TechnicalEntry{
		Level: level, Source: source, Code: code, Message: message, Detail: detail,
	})
}

// realOptions is what a test wants to change about the standard real bench.
type realOptions struct {
	// catalog is what is in service before anything is dropped. Nil is a virgin
	// station, whose grid says "Catalogue vide".
	catalog *domain.Catalog
	// wrap decorates the real source. It exists for failure test 11 and for nothing
	// else: it is where the one unreproducible syscall is injected.
	wrap func(ports.CatalogSource) ports.CatalogSource
}

// realBench is a whole station wired to the whole of L7: a real drop directory, the
// real parser, the real qualification, the real guards, a real SQLite database and a
// real image store. Only the clock is a double.
type realBench struct {
	t         *testing.T
	hub       *Hub
	clock     *fake.Clock
	db        *store.DB
	images    *catalog.ImageStore
	path      string
	archives  string
	technical *recordingTechnical
	returned  chan struct{}
}

// newRealBench starts one.
func newRealBench(t *testing.T, tweak ...func(*realOptions)) *realBench {
	t.Helper()

	o := realOptions{}
	for _, f := range tweak {
		f(&o)
	}
	cfg := loadConfig(t)
	// The shipped file declares webdav, because the production share is one. What is
	// exercised here is the drop directory, which is the source a volunteer uses and
	// the one the drag and drop of the administration screen writes into (A4).
	cfg.Catalog.Type = domain.CatalogSourceLocalDrop

	clock := fake.NewClock(epoch)
	dataDir := t.TempDir()
	db := store.OpenTest(t)
	sink := &recordingTechnical{}

	images, err := catalog.NewImageStore(dataDir)
	if err != nil {
		t.Fatalf("puits d'images : %v", err)
	}
	drop, err := localdrop.New(catalog.SourceConfig{
		Catalog: cfg.Catalog, StationNumber: cfg.Station.Number, DataDir: dataDir,
		Clock: clock, Log: technicalBridge{sink}, Images: images, Quarantine: db,
	})
	if err != nil {
		t.Fatalf("source de catalogue : %v", err)
	}
	applier, err := importer.New(importer.Options{
		Records: db, Clock: clock, Log: technicalBridge{sink},
	})
	if err != nil {
		t.Fatalf("applicateur : %v", err)
	}

	var source ports.CatalogSource = drop
	if o.wrap != nil {
		source = o.wrap(drop)
	}
	st, err := New(Options{
		Clock: clock, Config: cfg, Catalog: o.catalog,
		Scale: fake.NewScale(clock), Printer: fake.NewPrinter(),
		Journal: newRecordingJournal(), TechnicalSink: sink,
		CatalogSource: source, ApplyCatalog: applier.Apply,
	})
	if err != nil {
		t.Fatalf("station.New : %v", err)
	}

	b := &realBench{
		t: t, hub: st.Hub(), clock: clock, db: db, images: images,
		path:      drop.Path(),
		archives:  filepath.Join(dataDir, "catalog", "archives"),
		technical: sink,
		returned:  make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer close(b.returned)
		_ = st.Start(ctx)
	}()
	select {
	case <-st.Ready():
	case <-time.After(hang):
		t.Fatal("le poste n'a jamais fini de démarrer")
	}
	t.Cleanup(func() {
		st.Stop()
		cancel()
		<-b.returned
	})
	return b
}

// drop writes one of the authentic files into the watched directory.
func (b *realBench) drop(name string) {
	b.t.Helper()
	b.dropContent(fixtureBytes(b.t, name))
}

// dropContent writes arbitrary bytes into the watched directory, which is what a
// producer, a synchronisation tool and the administration drag and drop all do (A4).
func (b *realBench) dropContent(content []byte) {
	b.t.Helper()
	if err := os.WriteFile(b.path, content, 0o644); err != nil {
		b.t.Fatalf("dépôt du fichier : %v", err)
	}
}

// scan advances the injected clock by one polling interval and lets the Hub turn.
func (b *realBench) scan() {
	b.t.Helper()
	b.clock.Advance(dropInterval)
	b.flush()
	b.flush()
}

// flush runs one full turn of the loop and waits for its answer.
func (b *realBench) flush() {
	b.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), hang)
	defer cancel()
	if _, err := b.hub.Submit(ctx, domain.Tick{}, ""); err != nil {
		b.t.Fatalf("la boucle ne répond plus : %v", err)
	}
}

// awaitTiles scans until the grid in service holds exactly that many tiles.
func (b *realBench) awaitTiles(want int) *domain.Catalog {
	b.t.Helper()
	awaitCondition(b.t, func() bool {
		b.scan()
		return b.hub.Catalog() != nil && b.hub.Catalog().WeighableCount() == want
	}, fmt.Sprintf("la grille n'a jamais compté %d tuiles", want))
	return b.hub.Catalog()
}

// awaitFileGone scans until the dropped file has been acknowledged, which is to say
// ARCHIVED AND REMOVED (ADR-004).
func (b *realBench) awaitFileGone() {
	b.t.Helper()
	awaitCondition(b.t, func() bool {
		b.scan()
		_, err := os.Stat(b.path)
		return errors.Is(err, os.ErrNotExist)
	}, "le fichier déposé n'a jamais disparu : l'acquittement EST la suppression")
}

// awaitImports scans until the history holds that many rows, most recent first.
func (b *realBench) awaitImports(want int) []domain.Import {
	b.t.Helper()
	awaitCondition(b.t, func() bool {
		b.scan()
		return len(b.imports()) >= want
	}, fmt.Sprintf("l'historique n'a jamais compté %d import(s)", want))
	return b.imports()
}

// imports reads the import history.
func (b *realBench) imports() []domain.Import {
	b.t.Helper()
	rows, err := b.db.Imports(context.Background(), 20, 0)
	if err != nil {
		b.t.Fatalf("Imports : %v", err)
	}
	return rows
}

// archived lists what the archive directory holds, sorted.
func (b *realBench) archived() []string {
	b.t.Helper()
	entries, err := os.ReadDir(b.archives)
	if err != nil {
		b.t.Fatalf("lecture des archives : %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		// A « .part » is a copy IN FLIGHT, not an archive: Archive.Begin opens it before
		// the parse and Commit is what turns it into one. Counting it as an archive made
		// this bench read « a file was archived » where the truth was « a reading has
		// started », and internal/catalog/archive.go already skips the same suffix when
		// it prunes.
		if strings.HasSuffix(entry.Name(), ".part") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

// photoFiles counts the photos really written under <data>/images.
func (b *realBench) photoFiles() int {
	b.t.Helper()
	n := 0
	err := filepath.WalkDir(b.images.Directory(), func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			n++
		}
		return nil
	})
	if err != nil {
		b.t.Fatalf("parcours du puits d'images : %v", err)
	}
	return n
}

// fixtureBytes reads one of the two authentic files.
func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	content, err := os.ReadFile(fixtures + name)
	if err != nil {
		t.Fatalf("lecture de la fixture %s : %v", name, err)
	}
	return content
}

// digest is the sha256 of what was dropped, which is the key of the quarantine (§10.5).
func digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// rows splits an exchange file into its lines, header included.
//
// Splitting on the separator is safe on these files and only on them: no name carries a
// quote or a semicolon, and the base64 alphabet has neither (§10.2).
func rows(t *testing.T, content []byte) []string {
	t.Helper()
	out := make([]string, 0, 400)
	for _, line := range strings.Split(string(content), "\r\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	if len(out) < 2 {
		t.Fatalf("%d ligne(s) dans le fichier : ce n'est pas un catalogue", len(out))
	}
	return out
}

// join puts the lines back together in the form the format declares: CRLF, and a final
// one, exactly as the producer writes it.
func join(lines []string) []byte {
	return []byte(strings.Join(lines, "\r\n") + "\r\n")
}

// identifier reports the Odoo id one line of the exchange file carries.
func identifier(line string) string {
	fields := strings.Split(line, ";")
	if len(fields) == 0 {
		return ""
	}
	return strings.Trim(fields[0], `"`)
}

// withoutProducts removes the lines of the products named, and only those.
func withoutProducts(t *testing.T, lines []string, ids map[string]bool) []string {
	t.Helper()
	out := make([]string, 0, len(lines))
	removed := 0
	for i, line := range lines {
		if i > 0 && ids[identifier(line)] {
			removed++
			continue
		}
		out = append(out, line)
	}
	if removed != len(ids) {
		t.Fatalf("%d ligne(s) retirée(s) pour %d produits nommés", removed, len(ids))
	}
	return out
}

// touchLastName changes one product NAME and nothing else.
//
// It is what makes a second import a real one: the sha of the file changes, the
// qualification of every row does not, so whatever the grid then loses it lost for a
// reason that is not the file.
func touchLastName(t *testing.T, lines []string) []string {
	t.Helper()
	out := append([]string(nil), lines...)
	last := len(out) - 1
	fields := strings.Split(out[last], ";")
	if len(fields) != 7 {
		t.Fatalf("la dernière ligne porte %d colonnes", len(fields))
	}
	fields[1] = strings.TrimSuffix(fields[1], `"`) + ` BIS"`
	out[last] = strings.Join(fields, ";")
	return out
}

// shiftColumns swaps `code-barre` and `prix` on every line, header included.
//
// That is what a column shift at the producer looks like, and it is the failure the
// relative guard exists for (§10.4b): every row is still perfectly readable, the file
// is still whole, the absolute guard sees nothing at all — and not one product is
// weighable any more.
func shiftColumns(t *testing.T, lines []string) []string {
	t.Helper()
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		fields := strings.Split(line, ";")
		if len(fields) != 7 {
			t.Fatalf("ligne à %d colonnes : %.40s", len(fields), line)
		}
		fields[2], fields[3] = fields[3], fields[2]
		out = append(out, strings.Join(fields, ";"))
	}
	return out
}

// linesWithLevel counts the technical lines carrying one code at one level.
func (b *realBench) linesWithLevel(code, level string) int {
	b.technical.mu.Lock()
	defer b.technical.mu.Unlock()
	n := 0
	for _, e := range b.technical.entries {
		if e.Code == code && e.Level == level {
			n++
		}
	}
	return n
}

// quarantine reads what stands against a content, or reports that nothing does.
func (b *realBench) quarantine(sha string) (domain.QuarantineEntry, bool) {
	b.t.Helper()
	entry, err := b.db.Quarantine(context.Background(), sha)
	return entry, err == nil
}

// pollByPoll is a REAL catalog source the test polls ONE scrutation at a time.
//
// # WHY THE TEST BELOW CANNOT USE THE CLOCK
//
// Failure test 8 writes a file that grows and asserts nothing was read. That assertion
// is only sound if EXACTLY ONE poll observes each size, and advancing the injected
// clock does not give that: it merely drops a tick into a channel of capacity one. The
// watch runs on its own goroutine (§13.1 n° 5), so nothing says the poll that tick
// triggers has happened before the test writes the NEXT size.
//
// When that poll lags one turn, two consecutive polls see the same bytes, the file is
// declared immobile, and what the test calls a violation is a perfectly correct read of
// a file that really had stopped moving. It turned CI red on 29/07/2026, with an
// archive AND its .reason.txt — the copy was being parsed while os.WriteFile truncated
// it underneath. No local stress reproduces it: it takes a loaded two-core runner.
//
// The rendezvous closes the window instead of widening it. `ask` is taken by the watch
// when it is about to poll and `done` is given back when that poll is over, so the test
// writes the next size while the watch is PARKED — which no timeout can promise.
type pollByPoll struct {
	ports.CatalogSource
	ask  chan struct{}
	done chan struct{}
}

// Next performs exactly one poll per request from the test.
//
// The context handed down is already spent, which is what makes the real Next poll once
// and come back rather than wait for its own ticker. It costs the refusal path its
// quarantine write — and that is acceptable HERE and only here, because a refusal is
// precisely what this scenario asserts never happens.
func (p *pollByPoll) Next(ctx context.Context) (*ports.Batch, error) {
	select {
	case <-p.ask:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	batch, err := p.CatalogSource.Next(spent(ctx))
	if errors.Is(err, context.Canceled) && ctx.Err() == nil {
		// The cancellation is OURS, and it means « the poll is over, nothing found ».
		err = nil
	}
	select {
	case p.done <- struct{}{}:
	case <-ctx.Done():
	}
	return batch, err
}

// once asks for one poll and does not return until that poll has finished.
func (p *pollByPoll) once(t *testing.T) {
	t.Helper()
	select {
	case p.ask <- struct{}{}:
	case <-time.After(hang):
		t.Fatal("la veille du catalogue n'a jamais redemandé de scrutation")
	}
	select {
	case <-p.done:
	case <-time.After(hang):
		t.Fatal("la scrutation demandée ne s'est jamais terminée")
	}
}

// spent returns a child context that is already done.
func spent(parent context.Context) context.Context {
	ctx, cancel := context.WithCancel(parent)
	cancel()
	return ctx
}
