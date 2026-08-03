package station

import (
	"errors"
	"sync"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/station/ports"
)

// The drivers a configuration change rebuilds, and the promise that none of them can
// stall the screen that asked: serial to manual and back, a Start that failed before
// its goroutine, a Close that never returns, and a printer that stays in service when
// its replacement cannot be built.

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
