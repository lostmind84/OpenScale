package station

import (
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

	outcome, err := b.station.Reload(next)
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
	if _, err := b.station.Reload(manual); err != nil {
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
	if _, err := b.station.Reload(serial); err != nil {
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
	if _, err := b.station.Reload(next); err != nil {
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
	go func() { _, err := b.station.Reload(next); done <- err }()

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

	if !b.technical.has("ERR-SCL-08") {
		b.tick() // the technical line travels through the journal worker
	}
	if !b.technical.has("ERR-SCL-08") {
		t.Fatal("ERR-SCL-08 n'a pas été journalisé alors que la fermeture n'a pas été confirmée")
	}
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

	outcome, err := b.station.Reload(next)
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
	if _, err := b.station.Reload(next); err != nil {
		t.Fatalf("Reload : %v", err)
	}

	// The supervisor is the only goroutine that watches deadlines — no timer
	// goroutine is added to the inventory of §13.1.
	b.clock.Advance(confirmationWindow + time.Second)
	awaitCondition(t, func() bool {
		return hasOption(b.hub.Config().Scale.Options, "port", `"COM8"`)
	}, "la configuration non confirmée n'est jamais revenue en arrière")

}

// awaitCondition yields until a condition holds, and fails rather than hanging.
func awaitCondition(t *testing.T, holds func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(hang)
	for time.Now().Before(deadline) {
		if holds() {
			return
		}
		runtime.Gosched()
	}
	t.Fatal(message)
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

	outcome, err := b.station.Reload(next)
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
	if _, err := b.station.Reload(next); err != nil {
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

	outcome, err := b.station.Reload(next)
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

// TestACatalogSourceThatCannotBeRebuiltIsJournalled keeps the memory catalog in
// service: there is no gap, and the failure is named.
func TestACatalogSourceThatCannotBeRebuiltIsJournalled(t *testing.T) {
	b := newBench(t)
	b.station.newCatalogSource = func(domain.Config) (ports.CatalogSource, error) {
		return nil, errors.New("partage inaccessible")
	}
	next := b.hub.Config()
	next.Station.Number = 4
	if _, err := b.station.Reload(next); err != nil {
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

	outcome, err := b.station.Reload(next)
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
