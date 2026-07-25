package printing

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/station/ports"
)

// testEpoch is where every clock in this file starts. Any instant does; a fixed one
// keeps a failure message reproducible.
var testEpoch = time.Date(2026, 7, 25, 14, 32, 5, 0, time.UTC)

// transientError and permanentError are the two answers of the §8.5 taxonomy this
// service actually branches on, built as the ONE type a driver raises since the two
// copies were merged into ports.PrintError.
//
// They are named for the POLICY rather than for the kind, because the policy is what
// these tests are about: only a transient failure is tried again, and the choice of
// KindTemplate for the permanent one is arbitrary — any kind but transient would do,
// which is exactly the property under test.
func transientError(message string) error {
	return &ports.PrintError{Kind: ports.KindTransient, Op: "stub.Print", Message: message}
}

func permanentError(message string) error {
	return &ports.PrintError{Kind: ports.KindTemplate, Op: "stub.Print", Message: message}
}

// stubPrinter is a ports.Printer that records what it was asked and answers what a test
// told it to answer.
type stubPrinter struct {
	id string

	mu        sync.Mutex
	jobs      []ports.PrintJob
	selfTests []string
	// failures is consumed one per Print, front first. An exhausted list means success.
	failures    []error
	status      ports.PrinterStatus
	statusCalls int
	closes      int
	// hangs makes Print block until the context is done, which is failure test 6.
	hangs bool
	// attempts receives one token per Print call, so a test can step through retries
	// without polling anything.
	attempts chan struct{}
}

func newStub(id string) *stubPrinter {
	return &stubPrinter{id: id, attempts: make(chan struct{}, 8),
		status: ports.PrinterStatus{Health: ports.PrinterUnknown}}
}

func (p *stubPrinter) Descriptor() domain.PrinterDescriptor {
	return domain.PrinterDescriptor{ID: p.id, Label: "stub " + p.id}
}

func (p *stubPrinter) Print(ctx context.Context, job ports.PrintJob) (ports.PrintReceipt, error) {
	p.mu.Lock()
	hangs := p.hangs
	var err error
	if len(p.failures) > 0 {
		err, p.failures = p.failures[0], p.failures[1:]
	}
	if err == nil && !hangs {
		p.jobs = append(p.jobs, job)
	}
	p.mu.Unlock()

	select {
	case p.attempts <- struct{}{}:
	default:
	}
	if hangs {
		<-ctx.Done()
		return ports.PrintReceipt{}, ctx.Err()
	}
	if err != nil {
		return ports.PrintReceipt{}, err
	}
	return ports.PrintReceipt{JobID: job.Label.JobID, Bytes: 16310}, nil
}

func (p *stubPrinter) Status(context.Context) ports.PrinterStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.statusCalls++
	return p.status
}

func (p *stubPrinter) SelfTest(_ context.Context, what string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.selfTests = append(p.selfTests, what)
	if len(p.failures) > 0 {
		var err error
		err, p.failures = p.failures[0], p.failures[1:]
		return err
	}
	return nil
}

func (p *stubPrinter) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closes++
	return nil
}

func (p *stubPrinter) printed() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.jobs)
}

func (p *stubPrinter) statusAsked() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.statusCalls
}

func (p *stubPrinter) setStatus(s ports.PrinterStatus) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status = s
}

// serviceUnderTest wires a service over one main printer, with the clock, the counter
// and the journal a test can look into.
type serviceUnderTest struct {
	*Service
	main     *stubPrinter
	fallback *stubPrinter
	clock    *fake.Clock
	log      *recordedLog
	roll     *memoryRoll
}

func newService(t *testing.T, withFallback bool) *serviceUnderTest {
	t.Helper()
	s := &serviceUnderTest{
		main:  newStub("main"),
		clock: fake.NewClock(testEpoch),
		log:   &recordedLog{},
		roll:  &memoryRoll{},
	}
	options := ServiceOptions{
		Main:     s.main,
		MainName: "file « SATO WS408_2 »",
		Clock:    s.clock,
		Roll:     NewRollCounter(s.roll, 1000, s.log),
		Log:      s.log,
	}
	if withFallback {
		s.fallback = newStub("fallback")
		options.Fallback = s.fallback
		options.FallbackName = "file « SATO WS408_3 »"
	}
	service, err := NewService(options)
	if err != nil {
		t.Fatalf("NewService : %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	s.Service = service
	return s
}

// aJob is one label to print. Nothing in this package looks inside it.
func aJob() ports.PrintJob {
	return ports.PrintJob{Label: domain.Label{JobID: "01J9F2ABC"}}
}

// waitForClockWaiters blocks until at least n waits are registered on the injected
// clock. Advancing before the code under test has asked the clock for anything delivers
// the tick to nobody.
func waitForClockWaiters(t *testing.T, clk *fake.Clock, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if waiters, _ := clk.Pending(); waiters >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d attente(s) sur l'horloge injectée, attendu %d : le délai est mesuré ailleurs",
				func() int { w, _ := clk.Pending(); return w }(), n)
		}
		runtime.Gosched()
	}
}

// --- The roll counter never blocks a print ---------------------------------

// TestARollBelievedEmptyStILLPrints is the requirement, stated plainly.
//
// A counter that refused to print because it BELIEVED the roll empty would be worse
// than no counter at all: a volunteer changed the roll and told nobody — which is the
// ordinary case — and the station would refuse to serve a customer standing in front of
// it, on the strength of a number nobody maintains. What the end of a roll costs is one
// label; what a wrong veto costs is the till.
func TestARollBelievedEmptySTILLPrints(t *testing.T) {
	ctx := context.Background()
	s := newService(t, false)

	// Zero labels left, and then some: the roll is believed long gone.
	if err := s.Roll().SetPrinted(ctx, 1000); err != nil {
		t.Fatalf("recalage : %v", err)
	}
	if state := s.Roll().State(); state.Remaining > 0 || state.Level != domain.LevelWarn {
		t.Fatalf("le compteur devait se croire épuisé : %+v", state)
	}

	for label := 1; label <= 3; label++ {
		if _, err := s.Print(ctx, aJob()); err != nil {
			t.Fatalf("étiquette %d refusée alors que le rouleau est CRU vide : %v", label, err)
		}
	}
	if got := s.main.printed(); got != 3 {
		t.Errorf("%d étiquettes remises au driver, attendu 3", got)
	}
	if got := s.Roll().State().Printed; got != 1003 {
		t.Errorf("compteur = %d, attendu 1003 : il continue de compter, il ne décide de rien", got)
	}
}

// TestABrokenCounterNeverReachesTheCustomer: the persistence is down, the print still
// goes out, and the failure is a line in the technical journal.
func TestABrokenCounterNeverReachesTheCustomer(t *testing.T) {
	ctx := context.Background()
	s := newService(t, false)
	s.roll.mu.Lock()
	s.roll.addErr = errors.New("database is locked")
	s.roll.mu.Unlock()

	if _, err := s.Print(ctx, aJob()); err != nil {
		t.Fatalf("une base en panne a fait échouer une impression : %v", err)
	}
	found := false
	for _, line := range s.log.all() {
		if strings.Contains(line, "compteur de rouleau") {
			found = true
		}
	}
	if !found {
		t.Error("rien au journal technique : un compteur qui n'enregistre plus doit se dire")
	}
}

// TestSeveralCopiesAreSeveralLabels — the roll does not care that they came from one
// job.
func TestSeveralCopiesAreSeveralLabels(t *testing.T) {
	ctx := context.Background()
	s := newService(t, false)

	job := aJob()
	job.Copies = 3
	if _, err := s.Print(ctx, job); err != nil {
		t.Fatalf("Print : %v", err)
	}
	if _, err := s.Print(ctx, aJob()); err != nil { // Copies = 0: the driver substitutes its own
		t.Fatalf("Print : %v", err)
	}
	if got := s.Roll().State().Printed; got != 4 {
		t.Errorf("compteur = %d, attendu 4 (3 exemplaires + 1)", got)
	}
}

// TestAFailedPrintCountsNoLabel.
func TestAFailedPrintCountsNoLabel(t *testing.T) {
	ctx := context.Background()
	s := newService(t, false)
	s.main.failures = []error{permanentError("file introuvable")}

	if _, err := s.Print(ctx, aJob()); err == nil {
		t.Fatal("une impression échouée a été rendue comme un succès")
	}
	if got := s.Roll().State().Printed; got != 0 {
		t.Errorf("compteur = %d : une étiquette qui n'est pas sortie ne se compte pas", got)
	}
}

// --- The label in flight when the roll runs out ----------------------------

// TestTheLabelInFlightWhenTheRollRunsOutIsStillASuccess is failure test 5 and
// important-9 in one place.
//
// The sequence is the one that used to cost money: the last label comes out, THEN the
// printer says media-empty. The customer got a valid label AND a red screen telling
// them to fetch a volunteer, so they stuck two on, or weighed again — and the till
// counted twice.
//
// What must hold: the receipt comes back, the error is nil, the label is counted, the
// consumable state reaches the screen as an AMBER maintenance light, and the next
// weighing goes through as if nothing had happened.
func TestTheLabelInFlightWhenTheRollRunsOutIsStillASuccess(t *testing.T) {
	ctx := context.Background()
	s := newService(t, false)

	receipt, err := s.Print(ctx, aJob())
	if err != nil {
		t.Fatalf("l'étiquette en vol a été perdue : %v", err)
	}
	if receipt.JobID != "01J9F2ABC" {
		t.Errorf("reçu = %+v : le travail rendu doit être identifiable", receipt)
	}
	// A successful print never asks for a status: that is important-9 made structural
	// rather than remembered.
	if asked := s.main.statusAsked(); asked != 0 {
		t.Fatalf("Print a interrogé le statut %d fois : après un succès, aucun statut ne peut "+
			"plus réaffecter err (important-9)", asked)
	}

	// The printer now reports the end of the roll.
	s.main.setStatus(ports.PrinterStatus{Health: ports.PrinterConsumable, Detail: "media-empty"})
	report := s.Observe(ctx)

	if report.Health != ports.PrinterConsumable {
		t.Fatalf("santé = %d, attendu consommable : la pesée reste un succès et le feu passe "+
			"à l'orange", report.Health)
	}
	if report.Ready() {
		t.Error("l'imprimante est annoncée prête alors qu'elle n'a plus de papier")
	}
	if got := s.Roll().State().Printed; got != 1 {
		t.Errorf("compteur = %d, attendu 1 : l'étiquette EST sortie", got)
	}
	// And the station keeps selling.
	if _, err := s.Print(ctx, aJob()); err != nil {
		t.Errorf("la pesée suivante a été refusée : %v", err)
	}
}

// TestTheEndOfARollIsJournalledOnceWithTheCodeOf154 — ERR-PRN-06, « media-empty »,
// amber, and one line rather than six a minute (the status is re-read every ten
// seconds, §8.5).
func TestTheEndOfARollIsJournalledOnceWithTheCodeOf154(t *testing.T) {
	ctx := context.Background()
	s := newService(t, false)
	s.main.setStatus(ports.PrinterStatus{Health: ports.PrinterConsumable, Detail: "media-empty"})

	for probe := 0; probe < 3; probe++ {
		s.Observe(ctx)
	}

	// « ERR-PRN-06 » is written out rather than read from codeMediaEmpty: the code comes
	// from §15.4, not from this package, and a test that reads the constant agrees with
	// whatever the constant happens to say.
	lines := 0
	for _, line := range s.log.all() {
		if strings.Contains(line, "ERR-PRN-06") {
			lines++
			if !strings.HasPrefix(line, domain.LevelWarn+"|printer|") {
				t.Errorf("ligne « %s » : le rouleau est une maintenance, donc warn", line)
			}
		}
	}
	if lines != 1 {
		t.Errorf("%d ligne(s) ERR-PRN-06 au journal, attendu 1 : on journalise un CHANGEMENT, "+
			"pas une répétition — le statut est réinterrogé toutes les 10 s (§8.5). Journal : %v",
			lines, s.log.all())
	}
}

// TestAnUnreachablePrinterIsJournalledWithTheCodeOf154 — ERR-PRN-01, the row whose
// remedy is « Imprimer sur l'imprimante du poste N ».
func TestAnUnreachablePrinterIsJournalledWithTheCodeOf154(t *testing.T) {
	ctx := context.Background()
	s := newService(t, false)
	s.main.failures = []error{permanentError("accès refusé")}

	if _, err := s.Print(ctx, aJob()); err == nil {
		t.Fatal("Print a réussi")
	}
	// « ERR-PRN-01 » is written out for the same reason as ERR-PRN-06 above: §15.4 is
	// the source of the number, not this package.
	found := false
	for _, line := range s.log.all() {
		if strings.Contains(line, "ERR-PRN-01") {
			found = true
			if !strings.HasPrefix(line, domain.LevelError+"|printer|") {
				t.Errorf("ligne « %s » : une imprimante injoignable est une erreur", line)
			}
		}
	}
	if !found {
		t.Errorf("aucune ligne ERR-PRN-01 au journal : %v", s.log.all())
	}
	if report := s.Report(); report.Health != ports.PrinterFaulted || report.Level != LevelN1 {
		t.Errorf("rapport = %d au niveau %s : une écriture qui échoue EST une preuve, "+
			"et elle est de niveau N1", report.Health, report.Level)
	}
}

// --- The fallback printer, both ways ---------------------------------------

// TestTheFallbackIsAskedForAndComesBackTheSameWay covers the switch and the return.
//
// Both directions are a HUMAN decision (§8.4): the station cannot honestly observe
// either event. What it sees when the main printer dies is a write that failed, and a
// write fails on a cable knocked loose for two seconds as readily as on a dead printer;
// an automatic switch would send a customer's label two metres away while they watch an
// empty slot. And nothing at all tells the station that the printer has been FIXED —
// the volunteer who changed the roll is the only one who knows.
func TestTheFallbackIsAskedForAndComesBackTheSameWay(t *testing.T) {
	ctx := context.Background()
	s := newService(t, true)

	if r := s.Routing(); r.Fallback || r.Banner != "" || !r.Available {
		t.Fatalf("routage initial : %+v — le poste démarre sur son imprimante, et le bouton "+
			"« Imprimer sur l'imprimante du poste N » est offert puisqu'un secours est configuré", r)
	}
	if _, err := s.Print(ctx, aJob()); err != nil {
		t.Fatalf("Print : %v", err)
	}

	// --- towards the neighbour
	if err := s.UseFallback(ctx); err != nil {
		t.Fatalf("UseFallback : %v", err)
	}
	routing := s.Routing()
	if !routing.Fallback || !strings.Contains(routing.Banner, "SATO WS408_3") {
		t.Fatalf("routage après bascule : %+v — le bandeau est PERMANENT et il nomme "+
			"l'imprimante (§8.4)", routing)
	}
	if s.Descriptor().ID != "fallback" {
		t.Errorf("le descripteur montre %q : l'écran doit montrer la machine qui imprime",
			s.Descriptor().ID)
	}
	if _, err := s.Print(ctx, aJob()); err != nil {
		t.Fatalf("Print sur le secours : %v", err)
	}
	if s.main.printed() != 1 || s.fallback.printed() != 1 {
		t.Errorf("étiquettes : principale %d, secours %d — attendu 1 et 1",
			s.main.printed(), s.fallback.printed())
	}

	// --- and back
	if err := s.UseMain(ctx); err != nil {
		t.Fatalf("UseMain : %v", err)
	}
	routing = s.Routing()
	if routing.Fallback || routing.Banner != "" {
		t.Fatalf("routage après retour : %+v — le bandeau disparaît quand il n'y a plus rien "+
			"à signaler", routing)
	}
	if _, err := s.Print(ctx, aJob()); err != nil {
		t.Fatalf("Print après retour : %v", err)
	}
	if s.main.printed() != 2 || s.fallback.printed() != 1 {
		t.Errorf("étiquettes : principale %d, secours %d — attendu 2 et 1",
			s.main.printed(), s.fallback.printed())
	}

	// Both switches are journalled: somebody has to be able to answer « depuis quand
	// est-ce qu'on imprime chez le voisin ? ».
	var switched, returned bool
	for _, line := range s.log.all() {
		switched = switched || strings.Contains(line, "basculées sur l'imprimante de secours")
		returned = returned || strings.Contains(line, "repassent sur l'imprimante du poste")
	}
	if !switched || !returned {
		t.Errorf("journal : bascule=%v retour=%v — %v", switched, returned, s.log.all())
	}
}

// TestSwitchingPrinterForgetsWhatWasKnownAboutTheOtherOne.
//
// What this station knew about one printer says NOTHING about another one. Carrying a
// green light across the switch would be inventing a measurement, which is the same
// mistake as announcing « prête » at level N1.
// The observation that has to be dropped is the LEVEL N1 one — what the last write did
// — because nothing else overwrites it: the next probe of N2 and N3 speaks to the new
// printer, but a write outcome just sits there and would go on describing a machine
// this station is no longer printing on.
func TestSwitchingPrinterForgetsWhatWasKnownAboutTheOtherOne(t *testing.T) {
	ctx := context.Background()
	s := newService(t, true)
	// Both printers stay mute at N3, so that what the report shows can only come from
	// the write that just happened — which is exactly the observation at stake.
	printAndCheck := func(step string) {
		t.Helper()
		if _, err := s.Print(ctx, aJob()); err != nil {
			t.Fatalf("%s : %v", step, err)
		}
		if got := s.Report().Level; got != LevelN1 {
			t.Fatalf("%s : niveau = %s après une écriture réussie, attendu N1", step, got)
		}
	}
	forgotten := func(step string) {
		t.Helper()
		report := s.Report()
		if report.Level != LevelNone || report.Ready() {
			t.Fatalf("%s : rapport = %+v, attendu « rien n'a été observé ». Ce que le poste "+
				"savait d'une imprimante ne dit RIEN d'une autre, et le résultat de la dernière "+
				"écriture est ce qu'aucune sonde ne vient remplacer", step, report)
		}
	}

	printAndCheck("impression sur la principale")
	if err := s.UseFallback(ctx); err != nil {
		t.Fatalf("UseFallback : %v", err)
	}
	forgotten("après la bascule vers le secours")

	printAndCheck("impression sur le secours")
	if err := s.UseMain(ctx); err != nil {
		t.Fatalf("UseMain : %v", err)
	}
	forgotten("après le retour à la principale")
}

// TestAGreenLightDoesNotFollowTheSwitch: the neighbour's printer has not been looked at
// yet, and carrying « prête » across would be inventing a measurement — the same
// mistake as announcing « prête » at level N1.
func TestAGreenLightDoesNotFollowTheSwitch(t *testing.T) {
	ctx := context.Background()
	s := newService(t, true)
	s.main.setStatus(ports.PrinterStatus{Health: ports.PrinterReady, Detail: "file vide"})

	if report := s.Observe(ctx); !report.Ready() || report.Level != LevelN3 {
		t.Fatalf("l'imprimante principale devait être connue prête : %+v", report)
	}
	if err := s.UseFallback(ctx); err != nil {
		t.Fatalf("UseFallback : %v", err)
	}
	if report := s.Report(); report.Ready() {
		t.Fatalf("le feu vert de la principale a suivi la bascule : %+v", report)
	}
}

// TestAStationWithNoFallbackSaysSoInFrench.
func TestAStationWithNoFallbackSaysSoInFrench(t *testing.T) {
	s := newService(t, false)

	if r := s.Routing(); r.Available {
		t.Error("un secours est annoncé disponible alors qu'aucun n'est configuré : " +
			"le bouton de §14.4 ne doit pas apparaître")
	}
	err := s.UseFallback(context.Background())
	if err == nil {
		t.Fatal("la bascule a réussi sans imprimante de secours")
	}
	if !strings.Contains(err.Error(), "printer.options.fallback") {
		t.Errorf("message « %s » : il doit nommer la clé de configuration à renseigner", err)
	}
}

// TestSwitchingTwiceInTheSameDirectionIsANoOp: a volunteer pressing a button twice must
// not produce two journal lines and two forgotten states.
func TestSwitchingTwiceInTheSameDirectionIsANoOp(t *testing.T) {
	ctx := context.Background()
	s := newService(t, true)

	if err := s.UseMain(ctx); err != nil { // already on main
		t.Fatalf("UseMain sur la principale : %v", err)
	}
	if len(s.log.all()) != 0 {
		t.Errorf("journal non vide alors que rien n'a changé : %v", s.log.all())
	}
	for press := 1; press <= 2; press++ {
		if err := s.UseFallback(ctx); err != nil {
			t.Fatalf("UseFallback, appui %d : %v", press, err)
		}
	}
	lines := 0
	for _, line := range s.log.all() {
		if strings.Contains(line, "basculées") {
			lines++
		}
	}
	if lines != 1 {
		t.Errorf("%d ligne(s) de bascule, attendu 1", lines)
	}
}

// TestAFallbackWithNoNameIsRefusedAtConstruction: a permanent banner that cannot say
// where the labels are coming out sends a volunteer looking at four printers.
func TestAFallbackWithNoNameIsRefusedAtConstruction(t *testing.T) {
	_, err := NewService(ServiceOptions{
		Main:     newStub("main"),
		Fallback: newStub("fallback"),
		Clock:    fake.NewClock(testEpoch),
	})
	if err == nil {
		t.Fatal("une imprimante de secours sans nom a été acceptée")
	}
	if !strings.Contains(err.Error(), "bandeau") {
		t.Errorf("message « %s » : il doit dire à quoi sert le nom", err)
	}
}

// TestAServiceWithoutItsCollaboratorsIsRefused.
func TestAServiceWithoutItsCollaboratorsIsRefused(t *testing.T) {
	for _, c := range []struct {
		name    string
		options ServiceOptions
		says    string
	}{
		{"sans imprimante", ServiceOptions{Clock: fake.NewClock(testEpoch)}, "main printer"},
		{"sans horloge", ServiceOptions{Main: newStub("main")}, "INJECTED clock"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewService(c.options)
			if err == nil || !strings.Contains(err.Error(), c.says) {
				t.Errorf("erreur = %v, elle doit contenir « %s »", err, c.says)
			}
		})
	}
}

// --- Retries, on the injected clock ----------------------------------------

// documentedRetryDelays are the figures of §8.2 and of failure test 4, WRITTEN OUT
// HERE rather than read from retryDelays.
//
// Reading the production table would make this test agree with whatever the code says,
// which is the one thing a test must never do: the 300*(n+1) formula produced 300 ms
// then 600 ms and would have passed. These two numbers come from the document.
var documentedRetryDelays = []time.Duration{300 * time.Millisecond, 1000 * time.Millisecond}

// TestTwoRetriesAt300MsThen1S is the table of §8.2, checked as a table.
//
// The wait happens BEFORE a retry and never after the last failure — the previous loop
// slept 900 ms before returning the error, for nothing.
//
// Each step advances the clock by the DOCUMENTED delay minus one millisecond first, and
// checks that nothing moved: that is what tells 300 ms from 299 ms, and it is the only
// way a delay can be asserted rather than merely consumed.
func TestTwoRetriesAt300MsThen1S(t *testing.T) {
	if len(retryDelays) != len(documentedRetryDelays) {
		t.Fatalf("%d réessais, le document en annonce %d (§8.2, panne n° 4)",
			len(retryDelays), len(documentedRetryDelays))
	}
	s := newService(t, false)
	s.main.failures = []error{
		transientError("1"),
		transientError("2"),
		transientError("3"),
	}

	done := make(chan error, 1)
	go func() {
		_, err := s.Print(context.Background(), aJob())
		done <- err
	}()

	for step, delay := range documentedRetryDelays {
		<-s.main.attempts
		waitForClockWaiters(t, s.clock, 2) // the 8 s budget, plus this retry
		s.clock.Advance(delay - time.Millisecond)
		select {
		case <-s.main.attempts:
			t.Fatalf("réessai %d relancé avant %s : le délai annoncé par §8.2 n'est pas tenu",
				step+1, delay)
		case <-time.After(20 * time.Millisecond):
		}
		s.clock.Advance(time.Millisecond)
	}
	<-s.main.attempts // third and last attempt

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "3") {
			t.Fatalf("erreur rendue = %v, attendu celle du dernier essai", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Print attend encore après le troisième essai : le dernier échec ne s'accompagne " +
			"d'aucune attente")
	}
	if extra := len(s.main.attempts); extra != 0 {
		t.Errorf("%d essai(s) de plus que les trois attendus", extra)
	}
}

// TestAPermanentFailureIsNeverRetried: a template fault tried twice is two more seconds
// of a customer in front of a screen that was never going to print.
func TestAPermanentFailureIsNeverRetried(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
	}{
		{"faute classée permanente", permanentError("gabarit refusé")},
		{"erreur non classée", errors.New("quelque chose")},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newService(t, false)
			s.main.failures = []error{c.err, c.err, c.err}

			// Print runs aside so that a service which DID decide to retry is caught by
			// this assertion instead of hanging on an injected clock nobody advances. A
			// test that deadlocks reports the same fault, ten minutes later and unreadably.
			done := make(chan error, 1)
			go func() {
				_, err := s.Print(context.Background(), aJob())
				done <- err
			}()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("Print a réussi")
				}
			case <-time.After(200 * time.Millisecond):
				t.Fatal("Print attend un délai de réessai : seul le transitoire se réessaie " +
					"(§8.5), et une erreur que personne n'a classée n'est pas transitoire")
			}
			if attempts := len(s.main.attempts); attempts != 1 {
				t.Errorf("%d essais, attendu 1 : seul le transitoire se réessaie (§8.5)", attempts)
			}
		})
	}
}

// TestRetryableAsksTheErrorAndDefaultsToNo.
func TestRetryableAsksTheErrorAndDefaultsToNo(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want bool
	}{
		{"transitoire", transientError("transitoire"), true},
		{"permanente", permanentError("définitive"), false},
		{"non classée", errors.New("nue"), false},
		{"enveloppée", fmtWrap(transientError("transitoire")), true},
		{"nulle", nil, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := Retryable(c.err); got != c.want {
				t.Errorf("Retryable(%v) = %v, attendu %v", c.err, got, c.want)
			}
		})
	}
}

// fmtWrap hides an error behind one level of wrapping, the way a driver wraps the
// failure of its transport.
func fmtWrap(err error) error { return wrapped{err} }

type wrapped struct{ err error }

func (w wrapped) Error() string { return "enveloppée : " + w.err.Error() }
func (w wrapped) Unwrap() error { return w.err }

// TestAPrinterHangingIsBoundedByTheInjectedClock — failure test 6. The budget is spent
// on the fake clock, so the whole thing takes microseconds of wall time instead of the
// eight seconds a real context.WithTimeout would burn (§8.2, §16.4).
func TestAPrinterHangingIsBoundedByTheInjectedClock(t *testing.T) {
	started := time.Now()
	s := newService(t, false)
	s.main.hangs = true

	done := make(chan error, 1)
	go func() {
		_, err := s.Print(context.Background(), aJob())
		done <- err
	}()

	<-s.main.attempts
	waitForClockWaiters(t, s.clock, 1)
	s.clock.Advance(printBudget)

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("une imprimante qui pend a rendu un succès")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("le budget de 8 s n'est pas mesuré sur l'horloge injectée : le Hub resterait bloqué")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Errorf("durée murale %s : le budget doit être dépensé sur l'horloge factice", elapsed)
	}
}

// --- Self-tests and life cycle ---------------------------------------------

// TestSelfTestsGoThroughTheCatalogueAndBurnALabel.
func TestSelfTestsGoThroughTheCatalogueAndBurnALabel(t *testing.T) {
	ctx := context.Background()
	s := newService(t, false)

	for _, test := range SelfTests() {
		if err := s.SelfTest(ctx, string(test.ID)); err != nil {
			t.Fatalf("auto-test %q : %v", test.ID, err)
		}
	}
	if got := s.Roll().State().Printed; got != 3 {
		t.Errorf("compteur = %d, attendu 3 : un auto-test consomme une étiquette comme un autre", got)
	}
	if err := s.SelfTest(ctx, "barcode-frame"); err == nil {
		t.Error("l'auto-test supprimé par A2 a été lancé")
	}
	if err := s.SelfTest(ctx, "reglette"); err == nil {
		t.Error("un nom français a été accepté : le glossaire fixe label|alignment|ruler")
	}
	if got := s.Roll().State().Printed; got != 3 {
		t.Errorf("compteur = %d : un auto-test refusé n'imprime rien", got)
	}
}

// TestASelfTestThatFailsIsReportedAtLevelN1.
func TestASelfTestThatFailsIsReportedAtLevelN1(t *testing.T) {
	s := newService(t, false)
	s.main.failures = []error{permanentError("hors ligne")}

	if err := s.SelfTest(context.Background(), string(SelfTestAlignment)); err == nil {
		t.Fatal("l'auto-test a été rendu comme un succès")
	}
	if report := s.Report(); report.Health != ports.PrinterFaulted {
		t.Errorf("santé = %d après un auto-test échoué, attendu en panne", report.Health)
	}
	if got := s.Roll().State().Printed; got != 0 {
		t.Errorf("compteur = %d : rien n'est sorti", got)
	}
}

// TestCloseReleasesBothPrintersAndIsIdempotent — the Hub closes on a configuration
// reload and again on shutdown (§11.4, §13.4), and a handle already released is not
// news.
func TestCloseReleasesBothPrintersAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := newService(t, true)

	for call := 1; call <= 3; call++ {
		if err := s.Close(); err != nil {
			t.Fatalf("Close, appel %d : %v", call, err)
		}
	}
	if s.main.closes != 1 || s.fallback.closes != 1 {
		t.Errorf("fermetures : principale %d, secours %d — attendu 1 et 1 ; les DEUX sont "+
			"relâchées, une fois", s.main.closes, s.fallback.closes)
	}
	if _, err := s.Print(ctx, aJob()); err == nil {
		t.Error("une impression a été acceptée après la fermeture")
	}
	if err := s.SelfTest(ctx, string(SelfTestLabel)); err == nil {
		t.Error("un auto-test a été accepté après la fermeture")
	}
}

// TestTheReportIsReadableWithoutTouchingADevice: the dashboard renders it on every
// refresh and every state broadcast carries it, so it must cost nothing.
func TestTheReportIsReadableWithoutTouchingADevice(t *testing.T) {
	s := newService(t, false)

	report := s.Report()
	if report.Level != LevelNone || report.Ready() {
		t.Errorf("rapport au démarrage : %+v — rien n'a encore été observé", report)
	}
	if asked := s.main.statusAsked(); asked != 0 {
		t.Errorf("Report a interrogé le driver %d fois", asked)
	}
}

// TestStatusIsTheReportInTheShapeTheHubConsumes.
func TestStatusIsTheReportInTheShapeTheHubConsumes(t *testing.T) {
	ctx := context.Background()
	s := newService(t, false)
	s.main.setStatus(ports.PrinterStatus{Health: ports.PrinterUnknown, Raw: []byte{0x30, 0x41},
		Detail: "elle est vivante"})

	status := s.Status(ctx)
	if status.Health != ports.PrinterUnknown {
		t.Errorf("santé = %d : vivante n'est pas prête tant que la trame n'est pas décodée",
			status.Health)
	}
	if len(status.Raw) != 2 {
		t.Errorf("Raw = %d octets : la trame brute remonte jusqu'à l'écran d'administration",
			len(status.Raw))
	}
}

// TestTheQueueProbeIsOptionalAndItsFailureIsNotAFault: no probe means level N2 is
// simply never reached, and a probe that errors has observed nothing — neither is a
// reason to call a printer broken.
func TestTheQueueProbeIsOptionalAndItsFailureIsNotAFault(t *testing.T) {
	ctx := context.Background()
	s := newService(t, false)
	if _, err := s.Print(ctx, aJob()); err != nil {
		t.Fatalf("Print : %v", err)
	}
	if report := s.Observe(ctx); report.Level != LevelN1 {
		t.Errorf("niveau = %s sans sonde de file, attendu N1", report.Level)
	}

	broken, err := NewService(ServiceOptions{
		Main: s.main, Clock: s.clock, Queue: failingProbe{}, Log: s.log,
	})
	if err != nil {
		t.Fatalf("NewService : %v", err)
	}
	if report := broken.Observe(ctx); report.Health == ports.PrinterFaulted {
		t.Errorf("une sonde de file en panne est devenue une imprimante en panne : %+v", report)
	}
}

// failingProbe is a level N2 that cannot answer.
type failingProbe struct{}

func (failingProbe) Queue(context.Context) (Queue, error) {
	return Queue{}, errors.New("EnumJobs a échoué")
}

// TestTheQueueProbeRaisesTheLevelWhenItAnswers.
func TestTheQueueProbeRaisesTheLevelWhenItAnswers(t *testing.T) {
	s := newService(t, false)
	service, err := NewService(ServiceOptions{
		Main: s.main, Clock: s.clock, Log: s.log,
		Queue: staticProbe{Queue{Condition: Condition{PaperOut: true}, PendingJobs: 2}},
	})
	if err != nil {
		t.Fatalf("NewService : %v", err)
	}

	report := service.Observe(context.Background())
	if report.Level != LevelN2 || report.Health != ports.PrinterConsumable {
		t.Errorf("rapport = %+v, attendu N2 / consommable", report)
	}
	if report.PendingJobs != 2 {
		t.Errorf("travaux en attente = %d, attendu 2 : c'est le chiffre que seul N2 produit",
			report.PendingJobs)
	}
}

// staticProbe answers the same thing every time.
type staticProbe struct{ answer Queue }

func (p staticProbe) Queue(context.Context) (Queue, error) { return p.answer, nil }

// TestACancelledPrintStopsWaitingBetweenTwoAttempts.
//
// The Hub cancels: a shutdown (§13.4), a customer who walked away. The service must not
// sit through the rest of a 1 s backoff for a label nobody is waiting for, and it must
// report the cancellation rather than the printer's own error — the printer did not
// fail, we stopped asking.
func TestACancelledPrintStopsWaitingBetweenTwoAttempts(t *testing.T) {
	s := newService(t, false)
	s.main.failures = []error{transientError("1"), transientError("2")}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := s.Print(ctx, aJob())
		done <- err
	}()

	<-s.main.attempts
	waitForClockWaiters(t, s.clock, 2) // the budget, plus the 300 ms backoff
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("erreur rendue = %v, attendu l'annulation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Print attend encore : une annulation doit interrompre le délai entre deux essais")
	}
	if attempts := len(s.main.attempts); attempts != 0 {
		t.Errorf("%d essai(s) de plus après l'annulation", attempts)
	}
	if report := s.Report(); report.Health != ports.PrinterFaulted {
		t.Errorf("santé = %d : la dernière étiquette n'est pas partie", report.Health)
	}
}

// TestAServiceWithNoJournalDoesNotCrash — the reason nopLog exists, and the reason
// ports.NopTechnicalLog exists one floor up: no caller of this package should have to
// check whether its journal is nil.
func TestAServiceWithNoJournalDoesNotCrash(t *testing.T) {
	ctx := context.Background()
	main := newStub("main")
	service, err := NewService(ServiceOptions{Main: main, Clock: fake.NewClock(testEpoch)})
	if err != nil {
		t.Fatalf("NewService : %v", err)
	}
	defer func() { _ = service.Close() }()

	if got := service.Routing().Name; got != "stub main" {
		t.Errorf("nom = %q : sans MainName, c'est le libellé du driver qui sert", got)
	}
	main.setStatus(ports.PrinterStatus{Health: ports.PrinterConsumable})
	service.Observe(ctx) // journals a change of health through the discarding log
	if _, err := service.Print(ctx, aJob()); err != nil {
		t.Fatalf("Print : %v", err)
	}
	NewRollCounter(nil, 1000, nil).Printed(ctx, 1)
}
