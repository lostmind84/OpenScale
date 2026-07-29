package station

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/station/ports"
)

// scaleForge hands out one fake scale per instantiation, and remembers them all.
type scaleForge struct {
	clock ports.Clock
	mu    sync.Mutex
	built []*fake.Scale
	// prepare is run on each new driver, so a test can decide how the NEXT one
	// misbehaves.
	prepare func(*fake.Scale)
	err     error
}

func (f *scaleForge) New(domain.Config) (ports.Scale, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	s := fake.NewScale(f.clock)
	if f.prepare != nil {
		f.prepare(s)
	}
	f.built = append(f.built, s)
	return s, nil
}

func (f *scaleForge) last() *fake.Scale {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.built) == 0 {
		return nil
	}
	return f.built[len(f.built)-1]
}

func (f *scaleForge) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.built)
}

// TestAnInstantBlockNeverCutsAnything is the first line of the table of §11.4:
// limits, tiers, template, UI and journal apply through an atomic store, with no
// interruption at all.
func TestAnInstantBlockNeverCutsAnything(t *testing.T) {
	forge := &scaleForge{}
	b := newBench(t, func(o *benchOptions) { o.newScale = forge.New })
	forge.clock = b.clock

	next := b.hub.Config()
	next.Limits.MinWeight = 42
	next.UI.ReprintWindowSeconds = 120

	outcome, err := b.station.Reload(ReloadRequest{Next: next})
	if err != nil {
		t.Fatalf("Reload : %v", err)
	}
	if len(outcome.Changed) != 0 {
		t.Fatalf("blocs redémarrés %v, attendu aucun", outcome.Changed)
	}
	if !outcome.ConfirmBefore.IsZero() {
		t.Fatal("une confirmation est demandée pour un bloc qui ne coupe rien")
	}
	if got := b.hub.Config().Limits.MinWeight; got != 42 {
		t.Fatalf("min_weight_g = %d, attendu 42 : le bloc instantané n'a pas pris", got)
	}
	if forge.count() != 0 {
		t.Fatalf("%d balances instanciées : un bloc instantané a coupé le port série", forge.count())
	}
}

// TestSerialToManualAndBack is failure test 1 ter (a): two successive reloads,
// both directions work, and no channel is lost.
//
// The measurement channel belongs to the Hub FOR THE LIFETIME OF THE PROCESS: the
// re-instantiated driver writes into the SAME one. That is what makes the degraded
// mode reversible (bloquant-2).
func TestSerialToManualAndBack(t *testing.T) {
	forge := &scaleForge{}
	b := newBench(t, func(o *benchOptions) { o.newScale = forge.New })
	forge.clock = b.clock

	// serial -> manual
	manual := b.hub.Config()
	manual.Scale.Present = false
	manual.Scale.Type = ""
	if _, err := b.station.Reload(ReloadRequest{Next: manual}); err != nil {
		t.Fatalf("Reload vers manuel : %v", err)
	}
	if !b.scale.Closed() {
		t.Fatal("la balance de départ n'a pas été fermée")
	}
	b.tick()
	if got := b.hub.State().State; got != domain.ManualMode {
		t.Fatalf("état %s, attendu manual_mode", got)
	}

	// manual -> serial
	serial := b.hub.Config()
	serial.Scale.Present = true
	serial.Scale.Type = "gram-xfoc-plus"
	if _, err := b.station.Reload(ReloadRequest{Next: serial}); err != nil {
		t.Fatalf("Reload vers série : %v", err)
	}
	if forge.count() != 1 {
		t.Fatalf("%d balances instanciées, attendu 1", forge.count())
	}

	// The SAME channel still carries measurements, from the NEW driver.
	forge.last().Push(1236, domain.Stable)
	b.awaitIntake()
	b.tick()
	if got := b.hub.State().Weight.Gross; got != 1236 {
		t.Fatalf("poids %d g après l'aller-retour, attendu 1236 g : le canal a été perdu", got)
	}
}

// TestAStartThatFailsBeforeItsGoroutineStillAnswers is failure test 1 ter (b).
//
// The driver fails before it ever launched anything, and it still closes done —
// the mandatory corollary of §5.3. The configuration write answers, and the
// station falls back to manual entry with an amber light.
func TestAStartThatFailsBeforeItsGoroutineStillAnswers(t *testing.T) {
	forge := &scaleForge{prepare: func(s *fake.Scale) {
		s.FailToStart(errors.New("accès refusé au port COM8"))
	}}
	b := newBench(t, func(o *benchOptions) { o.newScale = forge.New })
	forge.clock = b.clock

	next := b.hub.Config()
	next.Scale.Options = mustOptions(t, `{"port":"COM9"}`)

	started := time.Now()
	if _, err := b.station.Reload(ReloadRequest{Next: next}); err != nil {
		t.Fatalf("Reload : %v", err)
	}
	if elapsed := time.Since(started); elapsed > 20*time.Millisecond {
		t.Fatalf("le rechargement a pris %s de temps mural", elapsed)
	}

	assertFallbackToManual(t, b, codeScaleUnavailable)
	if got := b.hub.Config().Scale.Options; !hasOption(got, "port", `"COM9"`) {
		t.Fatal("la configuration demandée n'est pas en service : seul le repli doit différer")
	}
}

// TestACloseThatNeverReturnsIsBounded is failure test 1 ter (c), and it is the
// hard point of the reload.
//
// The wait is bounded at 3 s of FAKE clock — under twenty milliseconds of wall
// time — the configuration is applied anyway, ERR-SCL-08 is journalled and the
// fallback is manual entry with an amber light.
func TestACloseThatNeverReturnsIsBounded(t *testing.T) {
	forge := &scaleForge{err: errors.New("le port ne se rouvre pas")}
	b := newBench(t, func(o *benchOptions) { o.newScale = forge.New })
	forge.clock = b.clock

	b.scale.HangOnClose()
	defer b.scale.Release()

	next := b.hub.Config()
	next.Scale.Options = mustOptions(t, `{"port":"COM9"}`)

	started := time.Now()
	done := make(chan error, 1)
	go func() { _, err := b.station.Reload(ReloadRequest{Next: next}); done <- err }()

	// Nothing moves until the INJECTED clock does.
	select {
	case <-done:
		t.Fatal("le rechargement n'a pas attendu la fermeture du port")
	case <-time.After(20 * time.Millisecond):
	}
	b.clock.Advance(scaleCloseBudget)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Reload : %v", err)
		}
	case <-time.After(hang):
		t.Fatal("le rechargement n'est pas borné : la configuration ne peut pas s'écrire")
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("le rechargement a pris %s de temps mural : le budget n'est pas sur l'horloge injectée", elapsed)
	}

	// The technical line travels through the journal WORKER, on its own goroutine, so
	// it is not there the instant Reload returns. A single tick was enough on a quiet
	// machine and not on a loaded CI runner, which is the definition of a flaky test.
	b.tick()
	awaitCondition(t, func() bool { return b.technical.has("ERR-SCL-08") },
		"ERR-SCL-08 n'a pas été journalisé alors que la fermeture n'a pas été confirmée")
	if got := b.station.Counters().UnconfirmedScaleCloses.Load(); got != 1 {
		t.Fatalf("fermetures non confirmées = %d, attendu 1", got)
	}
	assertFallbackToManual(t, b, codeScaleUnavailable)
}

// assertFallbackToManual checks the fallback of §11.4: a STATE, entered
// automatically, with its cause and its instant.
func assertFallbackToManual(t *testing.T, b *bench, code string) {
	t.Helper()
	cfg := b.hub.Config()
	if cfg.Scale.Present {
		t.Fatal("le poste se croit encore équipé d'une balance")
	}
	if !cfg.Scale.ManualEntryAllowed {
		t.Fatal("le repli n'autorise pas la saisie manuelle : le poste ne peut plus peser")
	}
	b.tick()
	s := b.hub.State()
	if s.Degraded == nil {
		t.Fatal("aucune dégradation publiée : le bandeau ne peut pas dire pourquoi")
	}
	if s.Degraded.Code != code {
		t.Fatalf("code de dégradation %q, attendu %q", s.Degraded.Code, code)
	}
	if s.Degraded.Since.IsZero() {
		t.Fatal("la dégradation n'a pas d'horodate : « pourquoi ce poste est-il en saisie " +
			"manuelle ce matin ? » redevient indécidable")
	}
	if s.State != domain.ManualMode {
		t.Fatalf("état %s, attendu manual_mode", s.State)
	}
}

// TestAHardwareChangeArmsTheCountdownAndCanBeConfirmed covers the ordinary path of
// the three-stage guard.
func TestAHardwareChangeArmsTheCountdownAndCanBeConfirmed(t *testing.T) {
	forge := &scaleForge{}
	b := newBench(t, func(o *benchOptions) { o.newScale = forge.New })
	forge.clock = b.clock

	next := b.hub.Config()
	next.Scale.Options = mustOptions(t, `{"port":"COM9"}`)

	outcome, err := b.station.Reload(ReloadRequest{Next: next})
	if err != nil {
		t.Fatalf("Reload : %v", err)
	}
	if outcome.ConfirmBefore.IsZero() {
		t.Fatal("un bloc matériel n'a pas armé le compte à rebours")
	}
	if want := epoch.Add(confirmationWindow); !outcome.ConfirmBefore.Equal(want) {
		t.Fatalf("échéance %s, attendu %s", outcome.ConfirmBefore, want)
	}
	if err := b.station.Confirm(); err != nil {
		t.Fatalf("Confirm : %v", err)
	}
	if err := b.station.Confirm(); !errors.Is(err, ErrNoConfirmationPending) {
		t.Fatalf("un second Confirm rend %v, attendu ErrNoConfirmationPending", err)
	}

	// Sixty seconds later, nothing goes back: the change was confirmed.
	b.advance(confirmationWindow + time.Second)
	if got := b.hub.Config().Scale.Options; !hasOption(got, "port", `"COM9"`) {
		t.Fatal("une configuration confirmée a été annulée")
	}
}

// TestAnUnconfirmedHardwareChangeGoesBack is the « ip route sous SSH » of §11.4.
func TestAnUnconfirmedHardwareChangeGoesBack(t *testing.T) {
	forge := &scaleForge{}
	b := newBench(t, func(o *benchOptions) { o.newScale = forge.New })
	forge.clock = b.clock

	before := b.hub.Config()
	next := before
	next.Scale.Options = mustOptions(t, `{"port":"COM9"}`)
	if _, err := b.station.Reload(ReloadRequest{Next: next}); err != nil {
		t.Fatalf("Reload : %v", err)
	}

	// The supervisor is the only goroutine that watches deadlines — no timer
	// goroutine is added to the inventory of §13.1.
	b.clock.Advance(confirmationWindow + time.Second)
	awaitCondition(t, func() bool {
		return hasOption(b.hub.Config().Scale.Options, "port", `"COM8"`)
	}, "la configuration non confirmée n'est jamais revenue en arrière")

}

// The two bounds of the wait below. They are POLLING intervals and not budgets: no
// decision of this application rests on them, and nothing they measure is business time.
const (
	// spinsBeforeSleeping is how many yields are tried before the wait goes to sleep.
	// Sixty-four costs a few microseconds and covers every case where the goroutine
	// that satisfies the condition is already runnable, which is the nominal one.
	spinsBeforeSleeping = 64
	// minPollDelay and maxPollDelay bound the sleep that follows. The ceiling is what
	// keeps a genuinely broken wait from taking the full five seconds to notice, and
	// the floor is what keeps the first retry after the spins nearly free.
	minPollDelay = 50 * time.Microsecond
	maxPollDelay = 2 * time.Millisecond
)

// awaitCondition yields until a condition holds, and fails rather than hanging.
//
// IT REALLY GIVES THE PROCESSOR BACK, and that is the whole of it. A bare loop of
// runtime.Gosched stays RUNNABLE: it keeps its P for the entire wait, and under
// `go test ./...`, where packages run side by side on a machine that has other work,
// it starves the very goroutine it is waiting for. TestAClockJumpIsReported failed that
// way on code that was right, and passed alone in a millisecond.
//
// So it spins first and sleeps afterwards. The spin is what keeps the NOMINAL cost
// unchanged — a condition satisfied by a goroutine that is already runnable holds
// within the first few yields, so no passing test gets slower. The sleep is what makes
// the loaded case cheap: it takes the waiter OFF the processor instead of competing
// with what it waits for.
//
// It sleeps, and the injected clock is not the answer here. « Aucun test ne dort » is
// about TIME THE APPLICATION MEASURES — a stability window, an expiry, a print budget —
// and every one of those is on fake.Clock. What this waits for is the Go SCHEDULER
// running a goroutine of the process under test, which no fake clock drives and no
// station budget describes.
func awaitCondition(t *testing.T, holds func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(hang)
	delay := minPollDelay
	for attempt := 0; time.Now().Before(deadline); attempt++ {
		if holds() {
			return
		}
		if attempt < spinsBeforeSleeping {
			runtime.Gosched()
			continue
		}
		time.Sleep(delay)
		if delay < maxPollDelay {
			delay *= 2
		}
	}
	t.Fatal(message)
}

// skipUnderShort leaves out a test whose verdict depends on WHEN another goroutine of
// the process is scheduled, and not on what the station decides.
//
// The tests it guards assert on a budget posted on the injected clock by a worker the
// test does not drive. They are deterministic as written — each one waits for the effect
// itself and not for a count that anybody could have produced — but the family has cost
// this repository three red runs and one publication, and a loaded two-core runner is
// where it costs them, never a development machine.
//
// `make test` runs the WHOLE suite, this family included, and the publication workflow
// trusts a green CI over the very same revision. So the guard moves where these tests
// run; it does not remove them. Run them before you tag.
func skipUnderShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("dépend de l'ordonnancement d'une autre goroutine : lancé par « make test », pas en intégration continue")
	}
}

// TestBlockFingerprintIsSemanticAndNotTextual is what keeps a reload from cutting
// a serial port because somebody reordered two JSON keys.
func TestBlockFingerprintIsSemanticAndNotTextual(t *testing.T) {
	first := domain.ScaleConfig{
		Type: "gram-xfoc-plus", Present: true, ManualEntryAllowed: true,
		Options: mustOptions(t, `{"port":"COM8","baud":9600}`),
	}
	second := first
	second.Options = mustOptions(t, `{"baud":9600,"port":"COM8"}`)

	if BlockFingerprint(first) != BlockFingerprint(second) {
		t.Fatal("deux configurations sémantiquement identiques ont des empreintes différentes : " +
			"un réordonnancement de clés couperait le port série en plein service")
	}

	// The case that really needs canonicalising: a NESTED object. Driver options
	// hold raw JSON, so the bytes of printer.options.fallback travel exactly as
	// they were typed — key order included.
	nested := domain.PrinterConfig{
		Type: "raster", Template: "weighing_identical",
		Options: mustOptions(t, `{"fallback":{"enabled":false,"queue":"SATO WS408_3"}}`),
	}
	reordered := nested
	reordered.Options = mustOptions(t, `{"fallback":{"queue":"SATO WS408_3","enabled":false}}`)
	if BlockFingerprint(nested) != BlockFingerprint(reordered) {
		t.Fatal("un objet imbriqué réordonné change l'empreinte : la file d'impression " +
			"serait reconstruite pour un espace de plus dans le fichier")
	}

	third := first
	third.Options = mustOptions(t, `{"port":"COM9","baud":9600}`)
	if BlockFingerprint(first) == BlockFingerprint(third) {
		t.Fatal("deux configurations différentes ont la même empreinte : le changement passerait inaperçu")
	}

	if got := len(BlockFingerprint(first)); got != 8 {
		t.Fatalf("empreinte de %d caractères, attendu 8 : c'est ce que lit l'écran d'administration", got)
	}
}

// TestANumberKeepsItsLiteralInTheFingerprint guards the canonicalisation: a figure
// re-read as a float and printed back in exponent form would change an
// fingerprint, and restart hardware, for nothing.
func TestANumberKeepsItsLiteralInTheFingerprint(t *testing.T) {
	options := mustOptions(t, `{"max_amount":99999999999999999999}`)
	first := BlockFingerprint(domain.ScaleConfig{Options: options})
	second := BlockFingerprint(domain.ScaleConfig{Options: mustOptions(t, `{"max_amount":99999999999999999999}`)})
	if first != second {
		t.Fatal("un grand entier ne survit pas à la canonicalisation")
	}
}

// mustOptions parses driver options from the JSON an operator would have typed.
func mustOptions(t *testing.T, raw string) domain.DriverOptions {
	t.Helper()
	var options domain.DriverOptions
	if err := json.Unmarshal([]byte(raw), &options); err != nil {
		t.Fatalf("options illisibles : %v", err)
	}
	return options
}

// hasOption reports whether an option carries a given raw JSON value.
func hasOption(options domain.DriverOptions, key, want string) bool {
	raw, ok := options[key]
	return ok && string(raw) == want
}

// printerForge hands out one fake printer per instantiation.
type printerForge struct {
	mu    sync.Mutex
	built []*fake.Printer
	err   error
}

func (f *printerForge) New(domain.Config) (ports.Printer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	p := fake.NewPrinter()
	f.built = append(f.built, p)
	return p, nil
}

func (f *printerForge) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.built)
}

// TestThePrinterBlockRebuildsAndReleasesTheOldOne is the second line of the
// hardware table of §11.4: close, rebuild, self-test, about 200 ms.
func TestThePrinterBlockRebuildsAndReleasesTheOldOne(t *testing.T) {
	forge := &printerForge{}
	b := newBench(t)
	b.station.newPrinter = forge.New

	next := b.hub.Config()
	next.Printer.Options = mustOptions(t, `{"transport":"winspool","queue":"SATO WS408_3"}`)

	outcome, err := b.station.Reload(ReloadRequest{Next: next})
	if err != nil {
		t.Fatalf("Reload : %v", err)
	}
	if len(outcome.Changed) != 1 || outcome.Changed[0] != blockPrinter {
		t.Fatalf("blocs redémarrés %v, attendu [%s]", outcome.Changed, blockPrinter)
	}
	if outcome.ConfirmBefore.IsZero() {
		t.Fatal("un changement d'imprimante n'a pas armé le compte à rebours")
	}
	if forge.count() != 1 {
		t.Fatalf("%d imprimantes instanciées, attendu 1", forge.count())
	}
	if !b.printer.Closed() {
		t.Fatal("l'imprimante précédente n'a pas été relâchée")
	}
}

// TestAPrinterThatCannotBeRebuiltKeepsTheOneThatWorks: losing a working printer
// over a setting that was refused anyway would take the station out of service for
// nothing.
func TestAPrinterThatCannotBeRebuiltKeepsTheOneThatWorks(t *testing.T) {
	forge := &printerForge{err: errors.New("file d'impression introuvable")}
	b := newBench(t)
	b.station.newPrinter = forge.New

	next := b.hub.Config()
	next.Printer.Options = mustOptions(t, `{"transport":"winspool","queue":"inconnue"}`)
	if _, err := b.station.Reload(ReloadRequest{Next: next}); err != nil {
		t.Fatalf("Reload : %v", err)
	}
	if b.printer.Closed() {
		t.Fatal("l'imprimante qui marche a été fermée pour une configuration refusée")
	}
	b.feed(1236, 2)
	if ack := b.tap("still-printing", 1236); !ack.Accepted {
		t.Fatalf("le poste n'imprime plus après un rechargement refusé : %s", ack.Message)
	}
	b.awaitPrint()
	awaitCondition(t, func() bool { return b.technical.has("ERR-PRN-01") },
		"le refus de reconstruction n'a pas été journalisé")
}

// TestTheCatalogBlockFollowsTheStationNumber is the fourth line of the table:
// station.number is reloaded WITH the catalog, because the name of the watched
// file — flv_<n>.csv — is its only real consumer.
func TestTheCatalogBlockFollowsTheStationNumber(t *testing.T) {
	first := newDropSource(nil)
	second := newDropSource(nil)
	b := newBench(t)
	b.station.swapCatalogSource(first)
	b.station.newCatalogSource = func(domain.Config) (ports.CatalogSource, error) { return second, nil }

	next := b.hub.Config()
	next.Station.Number = 3

	outcome, err := b.station.Reload(ReloadRequest{Next: next})
	if err != nil {
		t.Fatalf("Reload : %v", err)
	}
	if len(outcome.Changed) != 1 || outcome.Changed[0] != blockCatalog {
		t.Fatalf("blocs redémarrés %v, attendu [%s]", outcome.Changed, blockCatalog)
	}
	if outcome.ConfirmBefore.IsZero() != true {
		t.Fatal("un changement de catalogue arme un compte à rebours : il ne coupe rien")
	}
	if b.station.currentCatalogSource() != second {
		t.Fatal("la veille n'a pas été relancée sur la nouvelle source")
	}
}

// parkingSource blocks in Next until its context is cancelled, and announces every
// entry.
//
// Announcing the ENTRY is the whole point: a test that only knows a source yielded
// once cannot tell whether the watch has gone back inside it, and the property below
// is about a watch that is provably parked in the source a reload replaces.
type parkingSource struct{ entries chan struct{} }

func newParkingSource() *parkingSource {
	return &parkingSource{entries: make(chan struct{}, 4)}
}

func (s *parkingSource) Name() string { return domain.CatalogSourceLocalDrop }

func (s *parkingSource) Next(ctx context.Context) (*ports.Batch, error) {
	s.entries <- struct{}{}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *parkingSource) Acknowledge(context.Context, *ports.Batch, ports.BatchResult) error {
	return nil
}

func (s *parkingSource) Close() error { return nil }

var _ ports.CatalogSource = (*parkingSource)(nil)

// awaitEntry waits for the watch to be inside the source.
func awaitEntry(t *testing.T, source *parkingSource, message string) {
	t.Helper()
	select {
	case <-source.entries:
	case <-time.After(hang):
		t.Fatal(message)
	}
}

// TestTheWatchLeavesTheSourceAReloadReplaced is what the pointer swap of
// TestTheCatalogBlockFollowsTheStationNumber does NOT prove.
//
// The watch reads the source into a local variable and then blocks inside its Next,
// which returns on a batch, an error or a cancellation and on nothing else. Swapping
// the pointer under a goroutine parked in the old source changes what a getter
// answers and leaves the watch exactly where it was: the station went on watching an
// empty drop folder after being pointed at a share, and only a restart of the service
// ever moved it. « Recharger le catalogue » made it worse rather than better — it
// wakes the source in service, which is the one nobody is reading.
func TestTheWatchLeavesTheSourceAReloadReplaced(t *testing.T) {
	first, second := newParkingSource(), newParkingSource()
	b := newBench(t, func(o *benchOptions) { o.source = first })
	awaitEntry(t, first, "la veille n'est jamais entrée dans la source de départ")

	b.station.newCatalogSource = func(domain.Config) (ports.CatalogSource, error) { return second, nil }
	next := b.hub.Config()
	next.Station.Number = 3
	if _, err := b.station.Reload(ReloadRequest{Next: next}); err != nil {
		t.Fatalf("Reload : %v", err)
	}

	awaitEntry(t, second, "la veille est restée dans la source remplacée : "+
		"la nouvelle n'a jamais été lue, et un poste dans cet état n'importe plus rien")
}

// TestReplacingTheSourceIsNotAReadFailure keeps ERR-CAT-03 worth reading.
//
// The cancellation that ends the read is the station's own doing, and a journal that
// reported it as « Lecture du catalogue impossible » would put a red line under every
// ordinary change of source — which is how a code stops being read.
func TestReplacingTheSourceIsNotAReadFailure(t *testing.T) {
	first, second := newParkingSource(), newParkingSource()
	b := newBench(t, func(o *benchOptions) { o.source = first })
	awaitEntry(t, first, "la veille n'est jamais entrée dans la source de départ")

	b.station.newCatalogSource = func(domain.Config) (ports.CatalogSource, error) { return second, nil }
	next := b.hub.Config()
	next.Station.Number = 3
	if _, err := b.station.Reload(ReloadRequest{Next: next}); err != nil {
		t.Fatalf("Reload : %v", err)
	}
	awaitEntry(t, second, "la veille est restée dans la source remplacée")

	// A BARRIER, and it is what makes the assertion below mean something: a technical
	// line is enqueued on a channel the journal drains on its own goroutine, so asking
	// « is ERR-CAT-03 there ? » right away asks before the writer had to answer. This
	// second reload cannot rebuild anything and says so — after the watch enqueued
	// whatever it was going to enqueue, one FIFO, one consumer. ERR-CAT-05 in sight
	// therefore means ERR-CAT-03 would be in sight too.
	b.station.newCatalogSource = func(domain.Config) (ports.CatalogSource, error) {
		return nil, errors.New("partage inaccessible")
	}
	next.Station.Number = 4
	if _, err := b.station.Reload(ReloadRequest{Next: next}); err != nil {
		t.Fatalf("Reload : %v", err)
	}
	awaitCondition(t, func() bool { return b.technical.has("ERR-CAT-05") },
		"la barrière n'est jamais arrivée dans le journal")

	if b.technical.has("ERR-CAT-03") {
		t.Fatal("le remplacement d'une source a été journalisé comme une lecture impossible")
	}
}

// TestTheWatchPicksUpASourceItStartedWithout is the other half of the same
// property, and the one an installation meets first.
//
// A source that cannot be built is an amber light and never a station that refuses to
// start (serve.go), so a station whose share was unreachable at boot runs with no
// source at all. The watch used to wait on the process context in that case — which
// is to say for good: the volunteer repairs the address on the screen, the station
// answers « configuration enregistrée », and nothing is ever watched again.
func TestTheWatchPicksUpASourceItStartedWithout(t *testing.T) {
	arriving := newParkingSource()
	b := newBench(t) // no source: this is a station whose share was unreachable at boot
	b.station.newCatalogSource = func(domain.Config) (ports.CatalogSource, error) { return arriving, nil }

	next := b.hub.Config()
	next.Station.Number = 3
	if _, err := b.station.Reload(ReloadRequest{Next: next}); err != nil {
		t.Fatalf("Reload : %v", err)
	}

	awaitEntry(t, arriving, "la veille n'a jamais pris la source que le rechargement a mise "+
		"en service : ce poste ne peut plus importer sans redémarrage")
}

// TestACatalogSourceThatCannotBeRebuiltIsJournalled keeps the memory catalog in
// service: there is no gap, and the failure is named.
func TestACatalogSourceThatCannotBeRebuiltIsJournalled(t *testing.T) {
	b := newBench(t)
	b.station.newCatalogSource = func(domain.Config) (ports.CatalogSource, error) {
		return nil, errors.New("partage inaccessible")
	}
	next := b.hub.Config()
	next.Station.Number = 4
	if _, err := b.station.Reload(ReloadRequest{Next: next}); err != nil {
		t.Fatalf("Reload : %v", err)
	}
	if b.hub.Catalog() == nil {
		t.Fatal("le catalogue en mémoire a été perdu : le rechargement d'une source ne coupe rien")
	}
	awaitCondition(t, func() bool { return b.technical.has("ERR-CAT-05") },
		"l'échec de reconstruction de la source n'a pas été journalisé")
}

// TestTheListenAddressArmsTheCountdownWithoutRestartingAProcess is ADR-027: a
// net.Listener closes and reopens in three lines, so no configuration block
// demands a process restart.
func TestTheListenAddressArmsTheCountdownWithoutRestartingAProcess(t *testing.T) {
	b := newBench(t)
	next := b.hub.Config()
	next.Network.Listen = "127.0.0.1:8086"

	outcome, err := b.station.Reload(ReloadRequest{Next: next})
	if err != nil {
		t.Fatalf("Reload : %v", err)
	}
	if len(outcome.Changed) != 1 || outcome.Changed[0] != blockNetwork {
		t.Fatalf("blocs modifiés %v, attendu [%s]", outcome.Changed, blockNetwork)
	}
	if outcome.ConfirmBefore.IsZero() {
		t.Fatal("un changement d'adresse d'écoute doit armer le compte à rebours : " +
			"c'est le ip route sous SSH")
	}
}
