package diag

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"openscale/internal/domain"
)

// diagnostic.zip is what a volunteer sends when they call for help (§15.4). One button, no
// password, and everything a diagnosis needs FROM A DISTANCE — which is the only realistic
// remote support mechanism for a team of volunteers.
//
// The two rules of this file:
//
//  1. NO SECRET LEAVES. Every text member goes through the scrubber of redact.go, and the
//     configuration is redacted by key name on top of that. It is a security requirement,
//     and a test looks for the values inside the produced archive.
//  2. A MEMBER THAT CANNOT BE BUILT IS NOT A FAILURE. An archive is worth having when the
//     base is corrupt, when the service is down and when the label directory does not
//     exist — those are the mornings somebody presses the button. Each member records its
//     own failure in errors.txt and the archive is still valid, still readable, still
//     complete enough to work from.
//
// This file decides WHAT the archive holds and in what quantities. What each member LOOKS
// LIKE is in archive_members.go, and HOW one is written — scrubbed, stamped, or recorded as
// a failure — is in archive_writer.go, which is where the second rule above is enforced.

// The quantities of §15.4, quoted from the document.
const (
	// archivedWeighings is « 200 dernières pesées ».
	archivedWeighings = 200
	// archivedTechnical is « 500 derniers événements techniques ».
	archivedTechnical = 500
	// archivedFrames is « 30 dernières trames brutes ».
	archivedFrames = 30
	// archivedSBPL is « 5 derniers .sbpl ».
	archivedSBPL = 5
	// archivedLabelImages is « 3 derniers PNG d'étiquette ».
	archivedLabelImages = 3
)

// maxLabelBytes bounds one captured label copied into the archive.
//
// A 40 × 25 mm raster at 203 dpi is a few tens of kilobytes; two megabytes is a ceiling that
// no real label reaches and that stops a stray file in the label directory from turning a
// support archive into something nobody can email.
const maxLabelBytes = 2 << 20

// TechnicalEntry is one line of the technical journal as the archive reads it.
//
// Declared HERE and not imported: internal/diag names no storage package (§5.2, cut 3). The
// composition root converts, which is a handful of lines and the price of the cut.
type TechnicalEntry struct {
	OccurredAt time.Time
	Level      string
	Source     string
	Code       string
	Message    string
	Detail     string
}

// CatalogCounts is the inventory of the catalog IN SERVICE, read from the base.
//
// It is not the inventory of the last IMPORT, which §14.4 publishes on the dashboard and
// which imports.csv carries: this one answers « what is in the grid right now », and it
// answers it even when the service is down.
type CatalogCounts struct {
	Products   int            `json:"products_count"`
	Weighable  int            `json:"weighable_count"`
	Withdrawn  int            `json:"withdrawn_count"`
	ByCategory map[string]int `json:"by_category"`
}

// Journal is what diagnostic.zip reads out of the station base.
//
// Declared HERE, on the consumer's side. Every method may fail, and a failure is written
// into the archive rather than aborting it.
type Journal interface {
	// Weighings returns the most recent weighings, newest first.
	Weighings(ctx context.Context, limit int) ([]domain.Weighing, error)
	// TechnicalEntries returns the most recent technical lines, newest first.
	TechnicalEntries(ctx context.Context, limit int) ([]TechnicalEntry, error)
	// Imports returns the import history, most recent first.
	Imports(ctx context.Context, limit int) ([]domain.Import, error)
	// CatalogCounts reports what the catalog in service holds.
	CatalogCounts(ctx context.Context) (CatalogCounts, error)
}

// Bundle builds diagnostic.zip. It is built once and is safe for concurrent use.
type Bundle struct {
	doctor  *Doctor
	journal Journal
	// labels is <data>/labels, where the `file` transport drops one copy per label
	// (§11.1). Empty means this station keeps none, which is the default.
	labels string
}

// NewBundle wires the archive over a doctor.
//
// journal may be nil — a station whose base will not open still produces an archive, and
// that archive is exactly the one somebody needs.
func NewBundle(d *Doctor, journal Journal, labelsDir string) (*Bundle, error) {
	if d == nil {
		return nil, errors.New("diag.NewBundle: pas de doctor ; le rapport est le premier membre de l'archive")
	}
	return &Bundle{doctor: d, journal: journal, labels: labelsDir}, nil
}

// Diagnostic writes the archive into w.
//
// It is what GET /admin/api/diagnostic.zip serves and what `openscale doctor --zip` writes
// to a file. The signature is the one internal/web asks of a diagnostician.
func (b *Bundle) Diagnostic(ctx context.Context, w io.Writer) error {
	report := b.doctor.Run(ctx)
	health, healthErr := b.doctor.Health(ctx)
	loaded := b.doctor.readConfiguration()
	// The scrubber is built from the configuration ON DISK, which is the only place the
	// literal secrets of this station are known. A station whose configuration cannot be
	// read has no secret to protect that this archive could carry.
	clean := newScrubber(loaded.Config)

	archive := zip.NewWriter(w)
	writer := &memberWriter{zip: archive, clean: clean, clock: b.doctor.o.Clock}

	writer.text("README.txt", readme(report))
	writer.text("doctor.txt", reportText(report))
	writer.json("doctor.json", report)
	writer.json("system.json", systemMember(report, health, healthErr))

	if loaded.Present && loaded.Parsed {
		// The decoding faults travel WITH the configuration: they are what tells a block the
		// station declared from a block this binary replaced, and after this point the two
		// look exactly alike (warnSubstitutedBlocks).
		redacted, err := Redact(loaded.Config, loaded.DecodeFaults)
		if err != nil {
			writer.fail("config.redacted.json", err)
		} else {
			writer.bytes("config.redacted.json", redacted)
		}
	} else {
		writer.fail("config.redacted.json", configFailure(loaded))
	}

	if healthErr != nil {
		writer.fail("health.json", healthErr)
	} else {
		writer.bytes("health.json", health.Raw)
	}

	b.writeJournalMembers(ctx, writer)
	b.writeLabels(writer)
	writer.errorsMember()

	if err := archive.Close(); err != nil {
		return fmt.Errorf("archive de diagnostic non refermée : %w", err)
	}
	return nil
}

// writeJournalMembers writes everything that comes out of the base.
func (b *Bundle) writeJournalMembers(ctx context.Context, writer *memberWriter) {
	if b.journal == nil {
		writer.fail("journal", errors.New("la base du poste n'a pas pu être ouverte : "+
			"ni les pesées, ni le journal technique, ni les imports ne sont dans cette archive"))
		return
	}

	weighings, err := b.journal.Weighings(ctx, archivedWeighings)
	if err != nil {
		writer.fail("weighings.csv", err)
	} else {
		writer.csv("weighings.csv", weighingHeader, weighingRows(weighings))
		writer.text("frames.txt", framesMember(weighings))
	}

	if lines, err := b.journal.TechnicalEntries(ctx, archivedTechnical); err != nil {
		writer.fail("technical.csv", err)
	} else {
		writer.csv("technical.csv", technicalHeader, technicalRows(lines))
	}

	if imports, err := b.journal.Imports(ctx, archivedImports); err != nil {
		writer.fail("imports.csv", err)
	} else {
		writer.csv("imports.csv", importHeader, importRows(imports))
	}

	if counts, err := b.journal.CatalogCounts(ctx); err != nil {
		writer.fail("catalog.json", err)
	} else {
		writer.json("catalog.json", counts)
	}
}

// archivedImports is how many imports the archive carries. Twenty, which is what §14.4 puts
// on the expert catalog page: enough to see a source that has been failing for a fortnight.
const archivedImports = 20

// writeLabels copies the last captured labels, which is what makes a printing complaint
// diagnosable without travelling to the shop.
func (b *Bundle) writeLabels(writer *memberWriter) {
	if b.labels == "" {
		return
	}
	entries, err := os.ReadDir(b.labels)
	if err != nil {
		// A station that keeps no label copies is the DEFAULT: the `file` transport is a
		// development and support transport, not the production one. Saying so is enough.
		writer.note("labels", fmt.Sprintf("aucune copie d'étiquette : %v", err))
		return
	}

	for _, group := range []struct {
		suffix string
		keep   int
	}{{".sbpl", archivedSBPL}, {".png", archivedLabelImages}} {
		for _, name := range lastFiles(entries, group.suffix, group.keep) {
			path := filepath.Join(b.labels, name)
			raw, err := readBounded(path, maxLabelBytes)
			if err != nil {
				writer.fail("labels/"+name, err)
				continue
			}
			// NOT scrubbed, and deliberately: a captured label carries a product name, a
			// weight and a barcode, and nothing from the configuration. Passing a raster
			// through a text substitution would corrupt it.
			writer.raw("labels/"+name, raw)
		}
	}
}

// lastFiles reports the newest n names with that suffix, newest first.
//
// The order is taken from the MODIFICATION TIME and not from the name: the file transport
// names its copies after the job identifier, which is sortable, but a station whose clock
// jumped — the very thing ERR-SYS-07 watches for — would have names that sort against the
// order the labels came out in.
func lastFiles(entries []os.DirEntry, suffix string, n int) []string {
	type candidate struct {
		name     string
		modified time.Time
	}
	var found []candidate
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), suffix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		found = append(found, candidate{name: entry.Name(), modified: info.ModTime()})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].modified.After(found[j].modified) })
	if len(found) > n {
		found = found[:n]
	}
	names := make([]string, 0, len(found))
	for _, c := range found {
		names = append(names, c.name)
	}
	return names
}

// readBounded reads at most limit bytes of a file.
func readBounded(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, limit))
}
