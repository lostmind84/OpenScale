package station

import (
	"context"
	"errors"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/station/ports"
	"openscale/internal/store"
)

// TestEveryPublishedFieldChangesTheRevision walks the snapshot field by field.
//
// It is the test that keeps sameContentAs honest: a field added to Snapshot and
// forgotten in the comparison is a change that would NEVER be published — the
// screen would keep showing the previous value until the next heartbeat, or for
// ever if the field is the only thing that moved.
func TestEveryPublishedFieldChangesTheRevision(t *testing.T) {
	product := domain.Product{ID: "4412"}
	label := domain.Label{JobID: "j-1"}
	other := domain.Label{JobID: "j-2"}
	catalog := garlicCatalog()
	instant := epoch.Add(time.Minute)

	mutations := map[string]func(*Snapshot){
		"State":             func(s *Snapshot) { s.State = domain.Printing },
		"HasWeight":         func(s *Snapshot) { s.HasWeight = true },
		"Expired":           func(s *Snapshot) { s.Expired = true },
		"Weight.Gross":      func(s *Snapshot) { s.Weight.Gross = 1 },
		"Weight.Tare":       func(s *Snapshot) { s.Weight.Tare = 1 },
		"Weight.Net":        func(s *Snapshot) { s.Weight.Net = 1 },
		"Weight.Quantity":   func(s *Snapshot) { s.Weight.Quantity = 2 },
		"Weight.Stability":  func(s *Snapshot) { s.Weight.Stability = domain.Unstable },
		"Weight.Latched":    func(s *Snapshot) { s.Weight.Latched = true },
		"Weight.Seq":        func(s *Snapshot) { s.Weight.Seq = 7 },
		"Weight.Expiry":     func(s *Snapshot) { s.Weight.Expiry = time.Second },
		"Product":           func(s *Snapshot) { s.Product = &product },
		"Tare":              func(s *Snapshot) { s.Tare = 30 },
		"Units":             func(s *Snapshot) { s.Units = 3 },
		"Label":             func(s *Snapshot) { s.Label = &label },
		"LastLabel":         func(s *Snapshot) { s.LastLabel = &other },
		"LastPrintedAt":     func(s *Snapshot) { s.LastPrintedAt = instant },
		"ReprintAvailable":  func(s *Snapshot) { s.ReprintAvailable = true },
		"Message":           func(s *Snapshot) { s.Message = &Message{Text: "Étiquette envoyée."} },
		"Sound":             func(s *Snapshot) { s.Sound = "ok" },
		"Diagnostics":       func(s *Snapshot) { s.Diagnostics = []domain.Diagnostic{{Code: domain.CodeWeightTooLow}} },
		"FaultCode":         func(s *Snapshot) { s.FaultCode = "ERR-PRN-01" },
		"ArmingExpiresAt":   func(s *Snapshot) { s.ArmingExpiresAt = instant },
		"Catalog":           func(s *Snapshot) { s.Catalog = catalog },
		"Scale.Connected":   func(s *Snapshot) { s.Scale.Connected = true },
		"Scale.Median":      func(s *Snapshot) { s.Scale.Median = 400 * time.Millisecond },
		"Scale.TooSlow":     func(s *Snapshot) { s.Scale.TooSlow = true },
		"Printer.Health":    func(s *Snapshot) { s.Printer.Health = ports.PrinterFaulted },
		"Printer.Detail":    func(s *Snapshot) { s.Printer.Detail = "Injoignable." },
		"Degraded":          func(s *Snapshot) { s.Degraded = &Degradation{Code: codeScaleUnavailable} },
		"Station":           func(s *Snapshot) { s.Station = 3 },
		"UnloggedWeighings": func(s *Snapshot) { s.UnloggedWeighings = 1 },
	}

	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			var base, changed Snapshot
			mutate(&changed)
			if base.sameContentAs(changed) {
				t.Fatalf("%s ne fait pas changer le snapshot : ce champ ne serait jamais publié", name)
			}
		})
	}
}

// TestTheInstantAndTheAgeAreNotAChange is the other direction: these two move on
// every tick, and comparing them would make the station publish ten times a second
// with nothing to say.
func TestTheInstantAndTheAgeAreNotAChange(t *testing.T) {
	base := Snapshot{At: epoch, Weight: Weight{Age: time.Second}}
	later := Snapshot{At: epoch.Add(time.Hour), Weight: Weight{Age: time.Hour}}
	if !base.sameContentAs(later) {
		t.Fatal("l'instant ou l'âge comptent comme un changement : le poste publierait sans rien dire")
	}
}

// TestTheIdempotencyCacheRemembersThirtyTwoKeys covers the ring and the two rules
// that go with it: an empty key is never remembered, and the same key twice does
// not consume two slots.
func TestTheIdempotencyCacheRemembersThirtyTwoKeys(t *testing.T) {
	var cache IdempotencyCache

	if _, seen := cache.Lookup(""); seen {
		t.Fatal("la clé vide est mémorisée : la première commande sans clé répondrait pour toutes")
	}
	cache.Store("", domain.Ack{Accepted: true})
	if _, seen := cache.Lookup(""); seen {
		t.Fatal("la clé vide a été mémorisée par Store")
	}

	for i := 0; i < idempotencyDepth; i++ {
		cache.Store(itoa(i), domain.Ack{JobID: itoa(i)})
	}
	for i := 0; i < idempotencyDepth; i++ {
		ack, seen := cache.Lookup(itoa(i))
		if !seen || ack.JobID != itoa(i) {
			t.Fatalf("clé %d oubliée alors que le cache en tient %d", i, idempotencyDepth)
		}
	}

	// The same key twice updates in place instead of pushing the oldest out.
	cache.Store("0", domain.Ack{JobID: "réponse rejouée"})
	if _, seen := cache.Lookup("1"); !seen {
		t.Fatal("réécrire une clé existante a chassé une autre clé")
	}
	ack, _ := cache.Lookup("0")
	if ack.JobID != "réponse rejouée" {
		t.Fatalf("accusé %q, attendu la valeur réécrite", ack.JobID)
	}

	// The thirty-third key pushes the oldest out.
	cache.Store("nouvelle", domain.Ack{})
	if _, seen := cache.Lookup("0"); seen {
		t.Fatal("la clé la plus ancienne n'a pas été chassée : l'anneau n'est pas borné")
	}
}

// TestTheRingKeepsTheLastFiveHundredOldestFirst covers the RAM safety net.
func TestTheRingKeepsTheLastFiveHundredOldestFirst(t *testing.T) {
	var r ring
	if got := r.Entries(); len(got) != 0 {
		t.Fatalf("%d entrées dans un anneau vide", len(got))
	}

	for i := 0; i < ringDepth+10; i++ {
		r.Add(domain.Weighing{JobID: itoa(i)})
	}
	entries := r.Entries()
	if len(entries) != ringDepth {
		t.Fatalf("%d entrées, attendu %d", len(entries), ringDepth)
	}
	if entries[0].JobID != itoa(10) {
		t.Fatalf("la plus ancienne est %q, attendu %q : l'ordre n'est pas le plus ancien d'abord",
			entries[0].JobID, itoa(10))
	}
	if entries[len(entries)-1].JobID != itoa(ringDepth+9) {
		t.Fatalf("la plus récente est %q, attendu %q", entries[len(entries)-1].JobID, itoa(ringDepth+9))
	}

	// Entries hands out a COPY: the caller is an HTTP handler on another goroutine.
	entries[0].JobID = "modifiée par l'appelant"
	if again := r.Entries(); again[0].JobID != itoa(10) {
		t.Fatal("Entries rend l'anneau lui-même : un handler peut le réécrire sous le Hub")
	}
}

// TestThePrintWorkerHandsTheJobOverExactlyOnce is the deliberate absence of a
// retry loop here.
//
// The policy of §8.5 — KindTransient only, twice, at 300 ms then 1 s — lives in
// printing.Service.Print, which is the ports.Printer a real station is wired with.
// A second loop here would turn three attempts into nine and a 1.3-second failure
// into a four-second one, with a customer standing at the scale.
func TestThePrintWorkerHandsTheJobOverExactlyOnce(t *testing.T) {
	counting := &countingPrinter{err: errors.New("imprimante injoignable")}
	clock := fake.NewClock(epoch)
	results := make(chan PrintResult, 1)
	worker := &printWorker{
		printer: counting, clock: clock, results: results,
		hubDone: make(chan struct{}), counters: &Counters{}, finished: make(chan struct{}),
	}
	jobs := make(chan job, 1)
	go worker.run(jobs)

	jobs <- job{Label: domain.Label{JobID: "j-1"}}
	result := <-results
	if result.JobID != "j-1" {
		t.Fatalf("job %q, attendu j-1", result.JobID)
	}
	if result.Err == nil {
		t.Fatal("l'échec de l'imprimante n'est pas remonté au Hub")
	}
	if got := counting.count(); got != 1 {
		t.Fatalf("%d appels à Print pour un travail : le worker rejoue la politique de réessai "+
			"que printing.Service applique déjà", got)
	}

	close(jobs)
	waitFor(t, func() { <-worker.finished },
		"le worker n'est pas mort à la fermeture de son canal")
}

// TestThePrintBudgetIsSpentOnTheInjectedClock is failure test 6 at the worker: a
// device that hangs is cut at eight seconds of FAKE clock.
func TestThePrintBudgetIsSpentOnTheInjectedClock(t *testing.T) {
	skipUnderShort(t)
	printer := fake.NewPrinter()
	printer.Hang()
	defer printer.Release()

	clock := fake.NewClock(epoch)
	results := make(chan PrintResult, 1)
	worker := &printWorker{
		printer: printer, clock: clock, results: results,
		hubDone: make(chan struct{}), counters: &Counters{}, finished: make(chan struct{}),
	}
	jobs := make(chan job, 1)
	go worker.run(jobs)
	defer close(jobs)

	started := time.Now()
	jobs <- job{Label: domain.Label{JobID: "j-hang"}}
	// A held job and not a waiter count: the worker posts its budget and THEN calls
	// Print, so this is exact where counting waiters is a guess. See the twin assertion
	// in failures_test.go, which that guess cost a publication.
	awaitCondition(t, func() bool { return printer.Held() > 0 },
		"l'imprimante n'a jamais reçu le travail : le budget d'impression n'est pas encore posé")
	clock.Advance(printBudget)

	select {
	case result := <-results:
		if result.Err == nil {
			t.Fatal("une impression coupée par son budget est remontée comme un succès")
		}
	case <-time.After(hang):
		t.Fatal("le budget d'impression n'a pas coupé une imprimante qui pend")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("%s de temps mural : le budget est resté sur l'horloge réelle", elapsed)
	}
}

// countingPrinter counts how many times Print was called.
type countingPrinter struct {
	*fake.Printer
	err   error
	calls int64
}

func (p *countingPrinter) Print(context.Context, ports.PrintJob) (ports.PrintReceipt, error) {
	p.calls++
	return ports.PrintReceipt{}, p.err
}

func (p *countingPrinter) count() int64 { return p.calls }

func (p *countingPrinter) Descriptor() domain.PrinterDescriptor { return domain.PrinterDescriptor{} }
func (p *countingPrinter) Status(context.Context) ports.PrinterStatus {
	return ports.PrinterStatus{}
}
func (p *countingPrinter) SelfTest(context.Context, string) error { return nil }
func (p *countingPrinter) Close() error                           { return nil }

// TestTheJournalPurgesOnceInFifty is §4, step 16: the purge happens as often as
// the journal grows, and never on a timer that would fire while a customer weighs.
func TestTheJournalPurgesOnceInFifty(t *testing.T) {
	journal := newRecordingJournal()
	worker := &journalWorker{
		journal: journal, counters: &Counters{}, finished: make(chan struct{}),
		log: func(level, source, code, message, detail string) {},
	}
	weighings := make(chan domain.Weighing, 8)
	technical := make(chan TechnicalEntry, 8)
	go worker.run(weighings, technical)

	for i := 0; i < purgeEvery*2; i++ {
		weighings <- domain.Weighing{JobID: itoa(i)}
		<-journal.written
	}
	close(weighings)
	waitFor(t, func() { <-worker.finished }, "le worker de journal n'est pas mort à la fermeture")

	if got := journal.purgeCount(); got != 2 {
		t.Fatalf("%d purges pour %d pesées, attendu 2", got, purgeEvery*2)
	}
}

// TestTheStoreSatisfiesTheJournalContract compiles the real database against the
// interface this package declares, and writes one row through it.
//
// The interface is declared on the CONSUMER's side (cut 3 of §5.2), so nothing
// guarantees by construction that *store.DB still fits it. This is what does.
func TestTheStoreSatisfiesTheJournalContract(t *testing.T) {
	db := store.OpenTest(t)
	var journal Journal = db

	// weighings.product_id is a REAL foreign key since §10.9: the product keeps its
	// identity across imports, so the catalog has to be there first.
	if _, err := db.ReplaceCatalog(context.Background(), store.Batch{
		Import: domain.Import{
			OccurredAt: epoch, Source: domain.CatalogSourceLocalDrop,
			FileName: "flv_2.csv", SHA256: "sha", Result: domain.ImportApplied,
		},
		Categories: []domain.Category{{Code: "vegetables", Label: "Légumes", Rank: 1, Visible: true}},
		Products:   garlicCatalog().Products(),
	}); err != nil {
		t.Fatalf("ReplaceCatalog : %v", err)
	}

	weighing := domain.Weighing{
		OccurredAt: epoch, Station: 2, JobID: "01J9F2ABC", IdempotencyKey: "01J9F2ABC",
		ProductID: garlicID, ProductName: "AIL", Reference: "0493021000003",
		Mode: domain.ByWeight, GrossWeight: 1236, NetWeight: 1236, Quantity: 1,
		Barcode: garlicBarcode, Source: domain.SourceScale, Stability: domain.Stable,
		Result: domain.ResultSent,
	}
	if err := journal.RecordWeighing(context.Background(), &weighing); err != nil {
		t.Fatalf("RecordWeighing : %v", err)
	}
	if weighing.ID == 0 {
		t.Fatal("la pesée n'a pas reçu d'identifiant : elle n'est pas persistée")
	}
	if _, err := journal.PurgeWeighings(context.Background()); err != nil {
		t.Fatalf("PurgeWeighings : %v", err)
	}
}

// TestNewRefusesAStationItCannotServe checks the three collaborators without which
// nothing works, refused AT CONSTRUCTION rather than with a customer at the scale.
func TestNewRefusesAStationItCannotServe(t *testing.T) {
	cfg := loadConfig(t)
	cases := map[string]Options{
		"sans horloge":    {Config: cfg, Printer: fake.NewPrinter(), Journal: newRecordingJournal()},
		"sans imprimante": {Clock: fakeClockAt(epoch), Config: cfg, Journal: newRecordingJournal()},
		"sans journal":    {Clock: fakeClockAt(epoch), Config: cfg, Printer: fake.NewPrinter()},
	}
	for name, options := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := New(options); err == nil {
				t.Fatal("station.New accepte une composition qui ne peut pas servir")
			}
		})
	}
}

// TestAStationWithoutACatalogStartsAnyway is guiding principle 7: a virgin station
// starts, shows its administration screen and says the catalog is empty.
func TestAStationWithoutACatalogStartsAnyway(t *testing.T) {
	b := newBench(t, func(o *benchOptions) { o.catalog = nil })
	b.tick()
	if got := b.hub.State().State; got != domain.Initializing {
		t.Fatalf("état %s, attendu initializing", got)
	}
	if b.hub.Catalog() != nil {
		t.Fatal("un poste vierge prétend avoir un catalogue")
	}

	// And the first catalog puts it in service.
	b.offerCatalog(&CatalogBatch{Catalog: garlicCatalog()})
}

// TestAnUnusableConfigurationStartsOutOfService is §11.3: the station always
// starts, and a broken configuration must never produce a black screen.
func TestAnUnusableConfigurationStartsOutOfService(t *testing.T) {
	h := newHub(Options{
		Clock: fakeClockAt(epoch), Config: domain.NeutralProfile(),
		Counters: &Counters{}, OutOfService: true,
	})
	if got := h.State().State; got != domain.OutOfService {
		t.Fatalf("état %s, attendu out_of_service", got)
	}
}

// TestARepairedConfigurationPutsTheStationBackInServiceWITHOUTARestart.
//
// This is the second half of §11.3, and it was missing. A station whose file carries a
// fault starts on the neutral profile, in the terminal state, and serves its
// administration screen so that somebody can repair it — and then §11.4 promises that no
// configuration block requires a restart of the process. It did require one: the
// repaired station kept saying « Poste hors service » until the service was restarted,
// which on a kiosk is a screen with no button and no prompt behind it.
func TestARepairedConfigurationPutsTheStationBackInServiceWITHOUTARestart(t *testing.T) {
	b := newBench(t, func(o *benchOptions) {
		// Exactly what the composition root does with an unusable file (§11.3): the
		// neutral profile, in memory, in the terminal state.
		o.config = func(c *domain.Config) { *c = domain.NeutralProfile() }
		o.outOfService = true
		o.registries = knownDrivers()
	})
	if got := b.hub.State().State; got != domain.OutOfService {
		t.Fatalf("état de départ %s, attendu out_of_service", got)
	}

	// And what is missing is ONE field. The neutral profile is valid in every other
	// respect — that is what makes it the profile a station falls back onto — so the
	// password the first access poses is literally the last fault standing (contrôle 31).
	if _, err := b.station.Reload(repairedProfile()); err != nil {
		t.Fatalf("Reload : %v", err)
	}
	// One turn of the loop, because the answer to a command is sent before the
	// publication: without it the test would read the snapshot of the turn before.
	b.flush()
	// The catalog was already in memory, so the station has a grid to show.
	if got := b.hub.State().State; got != domain.Idle {
		t.Fatalf("état après réparation %s, attendu idle", got)
	}
}

// TestAStillBrokenConfigurationLeavesTheStationOutOfService: coming back into service is
// the answer to « il n'y a plus AUCUNE faute », never to « on a enregistré quelque chose ».
func TestAStillBrokenConfigurationLeavesTheStationOutOfService(t *testing.T) {
	b := newBench(t, func(o *benchOptions) {
		o.config = func(c *domain.Config) { *c = domain.NeutralProfile() }
		o.outOfService = true
		o.registries = knownDrivers()
	})

	broken := repairedProfile()
	broken.Station.Number = 0 // hors bornes [1, 99] : le nom du fichier surveillé en dérive
	if _, err := b.station.Reload(broken); err != nil {
		t.Fatalf("Reload : %v", err)
	}
	b.flush()
	if got := b.hub.State().State; got != domain.OutOfService {
		t.Fatalf("état %s : une configuration encore fautive remet le poste en service", got)
	}
}

// repairedProfile is the factory configuration once the first access has posed a
// password, which is the only fault the neutral profile carries (§11.3 contrôle 31).
//
// The hash is SHAPED and not derived: this package proves what the machine does with a
// valid configuration, and deriving one here would spend 64 MiB of argon2 to assert
// nothing. What the string has to satisfy is the control, and the control reads its
// shape.
func repairedProfile() domain.Config {
	cfg := domain.NeutralProfile()
	// Une empreinte comme argon2id en produit : 32 octets dont plusieurs ne sont pas du
	// texte. Le corps lisible qui figurait ici est précisément ce que le contrôle 31
	// refuse désormais — c'est le remplissage qui avait enfermé un poste dehors.
	cfg.Admin.PasswordHash = "$argon2id$v=19$m=65536,t=3,p=2$" +
		"c2VsLXBvdXItbGUtdGVzdA$AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"
	return cfg
}

// knownDrivers is what THIS binary would be built with, as far as the neutral profile is
// concerned.
//
// internal/station imports no driver package (cut 2 of §5.2), so the descriptors are
// written out here rather than fetched from a registry. Only the identifiers matter:
// what the station asks is « ce type existe-t-il ? », and the option schemas are exercised
// where they live, in cmd/openscale.
func knownDrivers() domain.Registries {
	return domain.Registries{
		Scales:         []domain.DriverDescriptor{{ID: "gram-xfoc-plus", Label: "GRAM XFOC +"}},
		Printers:       []domain.DriverDescriptor{{ID: "preview", Label: "Aperçu (PDF ou PNG)"}},
		Transports:     []domain.DriverDescriptor{{ID: "winspool", Label: "File Windows"}},
		CatalogSources: []domain.DriverDescriptor{{ID: "local_drop", Label: "Dépôt local"}},
	}
}

// TestAnUnknownTemplateDoesNotStopAStationThatIsServing: a name that resolves to
// nothing yields the zero template rather than a panic. The configuration control
// refuses an unknown template long before a customer stands at the scale.
func TestAnUnknownTemplateDoesNotStopAStationThatIsServing(t *testing.T) {
	b := newBench(t, func(o *benchOptions) {
		o.config = func(c *domain.Config) { c.Printer.Template = "un gabarit qui n'existe pas" }
	})
	b.feed(1236, 2)
	if ack := b.tap("no-template", 1236); !ack.Accepted {
		t.Fatalf("pesée refusée pour un gabarit inconnu : %s", ack.Message)
	}
	b.awaitPrint()
}

// TestASaturatedJournalChannelFallsBackOnTheRAMRing is the second half of
// ADR-013, and the one a customer feels: the label came out, the row could not
// even be QUEUED, and the weighing lands in memory with a counter rather than
// stopping the service.
//
// It is driven through execute directly, for the same reason as the saturated
// print worker: getting there through the loop means holding the journal worker
// still while sixty-five cycles run, and the test would then prove the setup
// rather than the branch.
func TestASaturatedJournalChannelFallsBackOnTheRAMRing(t *testing.T) {
	counters := &Counters{}
	h := newHub(Options{
		Clock: fakeClockAt(epoch), Config: loadConfig(t), Catalog: garlicCatalog(),
		Counters: counters,
	})
	for len(h.journalEntries) < cap(h.journalEntries) {
		h.journalEntries <- domain.Weighing{}
	}

	lost := domain.Weighing{JobID: "01J9F2ABC", Barcode: garlicBarcode, Result: domain.ResultSent}
	if ev := h.execute(domain.RecordEffect{Weighing: lost}, epoch); ev != nil {
		t.Fatalf("un journal saturé réinjecte %T : il ne doit rien réinjecter", ev)
	}
	if got := counters.UnloggedWeighings.Load(); got != 1 {
		t.Fatalf("compteur de pesées non journalisées = %d, attendu 1", got)
	}
	entries := h.Entries()
	if len(entries) != 1 || entries[0].JobID != lost.JobID {
		t.Fatalf("l'anneau RAM tient %d entrées : la pesée est perdue pour de bon", len(entries))
	}
}
