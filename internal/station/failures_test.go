package station

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/domain/frame"
	"openscale/internal/station/ports"
	"openscale/internal/store"
)

// The twenty-three failure tests of §16.2 — the recette criterion of this lot.
//
// The NUMBER of a line is its stable identifier, not its rank, so every test below
// names the line it answers and nothing else. Where the assertion needs an HTTP
// request or a socket it lives in internal/web, and where it was already written
// during the lot it is named here rather than duplicated: two tests asserting the
// same fact are two places to update and one place to forget.
//
//	   #   | test                                                  | where
//	-------+-------------------------------------------------------+--------------------------
//	 1     | TestScaleLossTriggeredByStatusAlone                    | hub_test.go
//	 1 bis | TestTheScaleComesBackInsideTwoHundredMilliseconds      | HERE
//	       | TestTheScaleComesBack                                  | hub_test.go
//	 1 ter | TestSerialToManualAndBack                           (a)| devices_test.go
//	       | TestAStartThatFailsBeforeItsGoroutineStillAnswers    (b)| devices_test.go
//	       | TestACloseThatNeverReturnsIsBounded                  (c)| devices_test.go
//	 2     | TestABabblingScaleYieldsNoMeasurement                  | HERE
//	 3     | TestAScaleThatNeverSaysStableStillPrintsInAdvisory     | HERE
//	 3 bis | TestASlowScaleIsCappedAtTheCeilingAndKeepsWeighing     | HERE
//	 3 ter | TestExpiredMeasurementRejectsWeighing                  | web + hub_test.go
//	 4     | TestAnUnreachablePrinterPrintsOnceAndFaultsTheCycle    | HERE
//	       | TestTwoRetriesAt300MsThen1S                            | internal/printing
//	 5     | TestAnEmptyRollAfterASuccessfulSendStaysASuccess       | HERE
//	 6     | TestAPrinterHangingIsCutAtEightSecondsOfInjectedClock  | HERE
//	 7     | TestAFullDiskPrintsAnywayAndKeepsTheRowInRAM           | HERE
//	       | TestAFullDiskLightsTheDashboardRedAndRefusesNobody     | web
//	 8     | TestACatalogFileStillGrowingIsNotRead                  | failures_catalog_test.go
//	 9     | TestACorruptedCatalogIsQuarantinedAndNMinusOneServesOn | failures_catalog_test.go
//	 10    | TestTheSameCatalogTwiceIsAppliedThenUnchanged          | failures_catalog_content_test.go
//	 11    | TestACatalogFileThatCannotBeDeletedIsAmberAndNotBanned | failures_catalog_test.go
//	 12    | TestAnAmputatedCatalogIsRefusedAndNamesItsReasons      | failures_catalog_content_test.go
//	 12 bis| TestAnOrdinaryCatalogLightsNothingRed                  | failures_catalog_content_test.go
//	 12 ter| TestAProductThatLeavesTheFileIsWithdrawnAndKeepsAll    | failures_catalog_content_test.go
//	 13    | TestTheCatalogNeverSwapsUnderAFinger                   | hub_test.go
//	 14    | TestALockedDatabaseNeverReachesTheCustomer             | HERE
//	 15    | TestDoubleTapPrintsOneLabel                            | hub_test.go
//	       | TestTheSameKeyTwiceYieldsOneJobAndTwoIdenticalAnswers  | web
//	 16    | TestASecondInstanceCannotTakeTheSocket                 | cmd/openscale
//	       | TestTheSocketIsTheSingleInstanceLock                   | web
//	 17    | TestArmingExpiresBeforeNextCustomerBag                 | hub_test.go
//
// None of them sleeps. The clock is injected, and every test that could be read as
// a wall-clock measurement asserts its own wall time so that a budget quietly moved
// back onto the real clock fails here rather than in the ten-second CI run (§16.4).

// --- 1 bis: the scale comes back ------------------------------------------

// TestTheScaleComesBackInsideTwoHundredMilliseconds is failure test 1 bis.
//
// The driver is closed, another one is instantiated in its place, and the first
// reading it emits reaches the Hub — through the SAME channel, which no driver ever
// closes (bloquant-2).
//
// « Moins de 200 ms » is asserted on the INJECTED clock, and the only delay on the
// path is the 100 ms publication throttle of §13.3 — nothing about the return waits
// for a timer of its own. The wall time is asserted too, so that a budget quietly
// moved back onto the real clock fails here.
//
// The first event a resumed driver emits is read as a RECONNECTION and its mass is
// dropped: ScaleEvent.Status is StatusConnected when it is left unset, and the switch
// of §13.2 tests the status before the measurement. It costs one frame — 400 ms on
// the parc — and it is why this test pushes an empty plate first and the mass after.
func TestTheScaleComesBackInsideTwoHundredMilliseconds(t *testing.T) {
	forge := &scaleForge{}
	b := newBench(t, func(o *benchOptions) { o.newScale = forge.New })
	forge.clock = b.clock

	b.disconnect(errors.New("câble débranché"))
	b.tick()
	if got := b.hub.State().State; got != domain.ScaleLost {
		t.Fatalf("état %s, attendu scale_lost avant le retour", got)
	}

	next := b.hub.Config()
	next.Scale.Options = mustOptions(t, `{"port":"COM9"}`)
	if _, err := b.station.Reload(ReloadRequest{Next: next}); err != nil {
		t.Fatalf("Reload : %v", err)
	}
	driver := forge.last()
	if driver == nil {
		t.Fatal("aucune balance réinstanciée : le poste reste sans source de poids")
	}

	injected, started := b.clock.Now(), time.Now()
	driver.Push(0, domain.Stable)
	b.awaitIntake()
	b.tick() // the publication throttle of §13.3, and nothing else

	if elapsed := b.clock.Now().Sub(injected); elapsed >= 200*time.Millisecond {
		t.Fatalf("le retour a coûté %s d'horloge injectée : la mesure attend un délai", elapsed)
	}
	if elapsed := time.Since(started); elapsed >= 200*time.Millisecond {
		t.Fatalf("le retour a coûté %s de temps mural", elapsed)
	}
	if got := b.hub.State().State; got != domain.Idle {
		t.Fatalf("état %s après le retour de la balance, attendu idle", got)
	}

	// And the SAME channel carries the weight: no channel was lost, no driver had
	// to be told which one to write into (bloquant-2).
	driver.Push(1236, domain.Stable)
	b.awaitIntake()
	b.tick()
	s := b.hub.State()
	if !s.HasWeight || s.Weight.Gross != 1236 {
		t.Fatalf("poids publié %d g (présent : %v), attendu 1236 g", s.Weight.Gross, s.HasWeight)
	}
}

// --- 2: a babbling scale ---------------------------------------------------

// TestABabblingScaleYieldsNoMeasurement is failure test 2.
//
// Six hundred bytes of noise, sliced at eighteen bytes — the read size of the legacy
// application, the one that turned a single lost byte into a permanent drift (§9.2).
// Nothing decodes, the buffer stays bounded, the accumulator resynchronises to its
// last 64 bytes rather than growing, and a real frame sent right afterwards is
// decoded: a noisy line costs measurements, never the port.
//
// It is the ACCUMULATOR that is driven here and not a driver, because that is where
// the property lives: below ports.Scale, no byte has become a measurement yet, and
// the Hub is only asked to prove that nothing reached it.
func TestABabblingScaleYieldsNoMeasurement(t *testing.T) {
	b := newBench(t)

	var accumulator frame.Accumulator
	var decoded []domain.Measurement
	noise := babble(600)
	for start := 0; start < len(noise); start += 18 {
		end := start + 18
		if end > len(noise) {
			end = len(noise)
		}
		decoded = append(decoded, accumulator.Feed(noise[start:end], b.clock.Now())...)
		if got := accumulator.Pending(); got > frame.MaxBuffer {
			t.Fatalf("%d octets en attente : le tampon fuit (borne %d)", got, frame.MaxBuffer)
		}
	}
	if len(decoded) != 0 {
		t.Fatalf("%d mesure(s) sorties de 600 octets de bruit : %v", len(decoded), decoded)
	}

	// A burst with NO terminator at all is what forces the resynchronisation: the
	// buffer has nothing to consume, grows past its bound, and keeps its last 64
	// bytes. « ST,GS,+ » is the worst case on purpose — a plausible prefix that
	// never completes.
	var stuck frame.Accumulator
	prefixes := make([]byte, 0, 616)
	for len(prefixes) < 600 {
		prefixes = append(prefixes, "ST,GS,+"...)
	}
	if got := stuck.Feed(prefixes, b.clock.Now()); len(got) != 0 {
		t.Fatalf("%d mesure(s) sorties de préfixes jamais terminés", len(got))
	}
	if stuck.Resyncs() != 1 {
		t.Fatalf("%d resynchronisation(s), attendu 1 : le tampon a grandi sans se recaler", stuck.Resyncs())
	}
	if got := stuck.Pending(); got > 64 {
		t.Fatalf("%d octets conservés après resynchronisation, attendu au plus 64", got)
	}

	// The line comes back, and the port was never lost.
	back := stuck.Feed([]byte("ST,GS,+  1.236KG\r\n"), b.clock.Now())
	if len(back) != 1 || back[0].Gross != 1236 {
		t.Fatalf("après le bruit, la trame réelle rend %v, attendu une mesure de 1236 g", back)
	}

	// And none of it ever became a weight on the screen.
	b.tick()
	if s := b.hub.State(); s.HasWeight {
		t.Fatalf("le poste affiche un poids (%d g) : du bruit est devenu une mesure", s.Weight.Gross)
	}
	if n := len(b.printer.Jobs()); n != 0 {
		t.Fatalf("%d étiquette(s) imprimées pendant le babillage", n)
	}
}

// babble returns n bytes of REPRODUCIBLE noise.
//
// Reproducible and not random: a corpus that changes from one run to the next makes
// a failure impossible to reproduce, and the grammar of §9.2 accepts frames as short
// as « 5G », so a draw of six hundred truly random bytes does occasionally contain a
// valid frame. What this test asserts is a property of a NAMED corpus.
func babble(n int) []byte {
	out := make([]byte, n)
	state := uint32(0x20260724)
	for i := range out {
		state ^= state << 13
		state ^= state >> 17
		state ^= state << 5
		out[i] = byte(state)
	}
	return out
}

// --- 3: a scale that never says ST ----------------------------------------

// TestAScaleThatNeverSaysStableStillPrintsInAdvisory is failure test 3.
//
// A corpus that is 100 % US on a station in the SHIPPED mode: the labels come out,
// and the journal records what the scale actually said. Stability is displayed and
// recorded; it does not block (A3, ADR-005).
func TestAScaleThatNeverSaysStableStillPrintsInAdvisory(t *testing.T) {
	b := newBench(t)
	if got := b.hub.Config().Stability.Mode; got != domain.ModeAdvisory {
		t.Fatalf("mode de stabilité livré %q, attendu %q", got, domain.ModeAdvisory)
	}

	for i := 0; i < 5; i++ {
		b.push(1236, domain.Unstable)
		b.advance(nominalCadence)
	}
	if s := b.hub.State(); s.Weight.Latched {
		t.Fatal("le poids est figé alors qu'aucune trame n'a jamais annoncé ST")
	}

	ack := b.tap("never-stable", 1236)
	if !ack.Accepted {
		t.Fatalf("pesée refusée en mode informatif sur un corpus 100 %% US : %s (%s)",
			ack.Message, ack.Code)
	}

	row := b.awaitJournal()
	if row.Stability != domain.Unstable {
		t.Fatalf("stabilité journalisée %q, attendu %q", row.Stability, domain.Unstable)
	}
	if row.Result != domain.ResultSent {
		t.Fatalf("résultat %q, attendu %q", row.Result, domain.ResultSent)
	}
	if n := len(b.printer.Jobs()); n != 1 {
		t.Fatalf("%d étiquette(s), attendu 1", n)
	}
}

// --- 3 bis: a scale that is too slow --------------------------------------

// TestASlowScaleIsCappedAtTheCeilingAndKeepsWeighing is failure test 3 bis.
//
// At a 2,4 s cadence the derived expiry — three times the observed median — would be
// 7,2 s, so the ceiling of 5 s takes over. That is exactly the condition the amber
// light watches: past the ceiling, a weight is declared expired BEFORE the next frame
// arrives, and the station falls silent for a reason nobody can name (§6.5).
//
// Weighing stays possible, and that is the other half: the light says « go and look
// at the cable », it never refuses a customer.
func TestASlowScaleIsCappedAtTheCeilingAndKeepsWeighing(t *testing.T) {
	const slowCadence = 2400 * time.Millisecond

	b := newBench(t)
	for i := 0; i < 9; i++ {
		b.push(1236, domain.Stable)
		b.advance(slowCadence)
	}

	s := b.hub.State()
	if s.Scale.Median != slowCadence {
		t.Fatalf("cadence médiane observée %s, attendu %s", s.Scale.Median, slowCadence)
	}
	if s.Scale.Provisional {
		t.Fatal("la cadence est encore annoncée provisoire après neuf trames")
	}
	if !s.Scale.TooSlow {
		t.Fatal("aucun feu orange : à 2,4 s de cadence, la mesure périme avant la trame suivante")
	}
	if got, want := s.Weight.Expiry, 5*time.Second; got != want {
		t.Fatalf("péremption dérivée %s, attendu le plafond de %s", got, want)
	}

	// A fresh frame, and the weighing goes through.
	b.push(1236, domain.Stable)
	b.tick()
	if ack := b.tap("slow", 1236); !ack.Accepted {
		t.Fatalf("pesée refusée sur une balance lente : %s (%s)", ack.Message, ack.Code)
	}
	b.awaitJournal()
	if n := len(b.printer.Jobs()); n != 1 {
		t.Fatalf("%d étiquette(s), attendu 1", n)
	}
}

// --- 4: an unreachable printer --------------------------------------------

// TestAnUnreachablePrinterPrintsOnceAndFaultsTheCycle is failure test 4, seen from
// the station.
//
// The two retries at 300 ms then 1 s belong to printing.Service, which IS the
// ports.Printer a real station is wired with, and they are pinned there by
// TestTwoRetriesAt300MsThen1S. What is asserted here is what the station owes the
// customer once the retries are spent: ONE handover, NO second label, a journal row
// that says « failed », and the fault code a volunteer reads on the screen.
func TestAnUnreachablePrinterPrintsOnceAndFaultsTheCycle(t *testing.T) {
	b := newBench(t)
	b.printer.Fail(errors.New("imprimante injoignable"))

	b.feed(1236, 2)
	if ack := b.tap("unreachable", 1236); !ack.Accepted {
		t.Fatalf("le cycle n'a pas démarré : %s (%s)", ack.Message, ack.Code)
	}

	row := b.awaitJournal()
	if row.Result != domain.ResultFailed {
		t.Fatalf("résultat journalisé %q, attendu %q", row.Result, domain.ResultFailed)
	}

	b.tick()
	s := b.hub.State()
	if s.FaultCode != "ERR-PRN-01" {
		t.Fatalf("code de panne %q, attendu ERR-PRN-01", s.FaultCode)
	}
	if s.State != domain.Faulted {
		t.Fatalf("état %s après une impression impossible, attendu faulted", s.State)
	}

	counters := b.station.Counters()
	if got := counters.PrintJobs.Load(); got != 1 {
		t.Fatalf("%d remise(s) à l'imprimante, attendu 1 : le poste rejoue une politique "+
			"de réessai qui appartient à printing.Service", got)
	}
	if got := counters.PrintFailures.Load(); got != 1 {
		t.Fatalf("%d échec(s) d'impression comptés, attendu 1", got)
	}
	if n := len(b.printer.Jobs()); n != 0 {
		t.Fatalf("%d étiquette(s) sorties d'une imprimante injoignable", n)
	}

	// The same key again replays the answer and hands nothing over a second time.
	b.tap("unreachable", 1236)
	if got := counters.PrintJobs.Load(); got != 1 {
		t.Fatalf("%d remises après le rejeu de la même clé, attendu 1", got)
	}
}

// --- 5: no paper AFTER a successful send ----------------------------------

// TestAnEmptyRollAfterASuccessfulSendStaysASuccess is failure test 5, and it is
// important-9 written as an assertion.
//
// The label came out. Turning the status that follows it into an error sent a
// customer away with a valid label and a red screen telling them to fetch a
// volunteer, so they stuck two labels on their bag or weighed again — double-counted
// at the till. The roll is an AMBER light on a screen a volunteer reads, and the
// weighing stays « sent ».
func TestAnEmptyRollAfterASuccessfulSendStaysASuccess(t *testing.T) {
	b := newBench(t)

	b.feed(1236, 2)
	if ack := b.tap("last-label", 1236); !ack.Accepted {
		t.Fatalf("pesée refusée : %s (%s)", ack.Message, ack.Code)
	}
	b.awaitPrint()

	// The device answers « media empty » only once the bytes are out.
	b.printer.SetStatus(ports.PrinterStatus{
		Health: ports.PrinterConsumable, Detail: "Fin de rouleau.",
	})

	row := b.awaitJournal()
	if row.Result != domain.ResultSent {
		t.Fatalf("résultat %q : un succès a été transformé en erreur par la fin de rouleau", row.Result)
	}

	awaitCondition(t, func() bool {
		b.clock.Advance(supervisorInterval)
		b.tick()
		return b.hub.State().Printer.Health == ports.PrinterConsumable
	}, "le superviseur n'a jamais relevé la fin de rouleau")

	s := b.hub.State()
	if s.State == domain.Faulted {
		t.Fatal("le poste est en panne pleine page alors que l'étiquette est sortie")
	}
	if s.Message != nil && s.Message.Level == domain.LevelError {
		t.Fatalf("message client de niveau erreur (%q) pour une fin de rouleau", s.Message.Text)
	}
	if s.Printer.Detail != "Fin de rouleau." {
		t.Fatalf("détail imprimante %q, attendu « Fin de rouleau. »", s.Printer.Detail)
	}

	// And the next customer is served. The bag of the first one leaves the plate,
	// which is the physical signal that ends a cycle (§14.3).
	b.push(0, domain.Stable)
	b.tick()
	if ack := b.weigh("after-the-roll", 1236); !ack.Accepted {
		t.Fatalf("pesée refusée après une fin de rouleau : %s (%s)", ack.Message, ack.Code)
	}
}

// --- 6: a printer that hangs for sixty seconds ----------------------------

// TestAPrinterHangingIsCutAtEightSecondsOfInjectedClock is failure test 6, end to
// end.
//
// The device never answers. The Hub keeps turning and keeps answering throughout —
// it is the worker that waits, never the loop — and the budget of eight seconds is
// spent on the INJECTED clock, so sixty seconds of a hanging device cost this test
// nothing at all.
func TestAPrinterHangingIsCutAtEightSecondsOfInjectedClock(t *testing.T) {
	skipUnderShort(t)
	started := time.Now()

	b := newBench(t)
	b.printer.Hang()
	defer b.printer.Release()

	b.feed(1236, 2)
	if ack := b.tap("hanging", 1236); !ack.Accepted {
		t.Fatalf("pesée refusée : %s (%s)", ack.Message, ack.Code)
	}

	// The loop answers, and keeps publishing, while the device holds the worker.
	// One second of injected time, well inside the eight-second budget: what is
	// being proved is that nothing about the loop depends on the printer.
	for i := 0; i < 10; i++ {
		b.tick()
	}
	if got := b.hub.State().State; got != domain.Printing {
		t.Fatalf("état %s : le cycle s'est terminé sans réponse de l'imprimante", got)
	}

	// The device must have been REACHED, and counting the clock's waiters does not say
	// that. Every ports.WithBudget of the station registers one — the supervisor posts
	// its own on every turn to probe the printer's status — and a cancelled budget stays
	// registered until its deadline, which is the fake's bookkeeping and not a leak. So
	// `waiters > 0` was true long before the print budget existed: the Advance below
	// fired somebody else's, the print budget was posted a moment later against a clock
	// that had already moved, and nothing ever cut the job. It broke the publication of
	// v0.4 — `make test` gates the archives, and the archives never came.
	//
	// A held job is exact: printWorker.print posts its budget and THEN calls Print.
	awaitCondition(t, func() bool { return b.printer.Held() > 0 },
		"l'imprimante n'a jamais reçu le travail : le budget d'impression n'est pas encore posé")
	b.clock.Advance(printBudget)

	row := b.awaitJournal()
	if row.Result != domain.ResultFailed {
		t.Fatalf("résultat %q, attendu %q : le budget n'a pas coupé l'impression",
			row.Result, domain.ResultFailed)
	}
	b.tick()
	if got := b.hub.State().FaultCode; got != "ERR-PRN-01" {
		t.Fatalf("code de panne %q, attendu ERR-PRN-01", got)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("durée murale %s : le budget est resté sur l'horloge réelle", elapsed)
	}
}

// --- 7: a full disk --------------------------------------------------------

// TestAFullDiskPrintsAnywayAndKeepsTheRowInRAM is failure test 7.
//
// « On dégrade le JOURNAL, jamais le SERVICE » (ADR-013). The store refuses every
// write; the label still comes out, the row lands in the RAM ring and the counter
// the dashboard shows in red goes up. The red banner itself is asserted in
// internal/web, which is where the dashboard is served.
//
// §16.2 injects a 10 MB tmpfs, which is a Linux-only setup. What reaches the station
// from a full disk is an error on every write, and that is what is injected here —
// on the three targets, in microseconds.
func TestAFullDiskPrintsAnywayAndKeepsTheRowInRAM(t *testing.T) {
	journal := newRecordingJournal()
	journal.err = errors.New("write balance.db: no space left on device")
	b := newBench(t, func(o *benchOptions) { o.journal = journal })

	b.feed(1236, 2)
	if ack := b.tap("disk-full", 1236); !ack.Accepted {
		t.Fatalf("pesée refusée alors que seul le disque est plein : %s (%s)", ack.Message, ack.Code)
	}
	b.awaitPrint()
	if n := len(b.printer.Jobs()); n != 1 {
		t.Fatalf("%d étiquette(s) : la pesée doit sortir quoi qu'il arrive au journal", n)
	}

	counters := b.station.Counters()
	awaitCondition(t, func() bool {
		b.tick()
		return counters.UnloggedWeighings.Load() == 1
	}, "la pesée perdue n'a jamais été comptée")

	entries := b.hub.Entries()
	if len(entries) != 1 {
		t.Fatalf("%d pesée(s) dans l'anneau RAM, attendu 1 : le compteur dit COMBIEN, "+
			"seul l'anneau dit LESQUELLES", len(entries))
	}
	if string(entries[0].Barcode) != garlicBarcode {
		t.Fatalf("code-barres conservé %q, attendu %q", entries[0].Barcode, garlicBarcode)
	}
	if got := counters.JournalFailures.Load(); got != 1 {
		t.Fatalf("%d échec(s) de journal comptés, attendu 1", got)
	}

	// And the station keeps serving: a full disk is not a reason to stop weighing.
	b.push(0, domain.Stable)
	b.tick()
	if ack := b.weigh("still-serving", 1236); !ack.Accepted {
		t.Fatalf("seconde pesée refusée : %s (%s)", ack.Message, ack.Code)
	}
}

// --- 14: a locked database -------------------------------------------------

// TestALockedDatabaseNeverReachesTheCustomer is failure test 14, against the REAL
// database and a REAL lock.
//
// A rival connection holds the write lock, so the journal worker is stuck on
// busy_timeout — five seconds of the production DSN, which no injected clock drives
// because it lives inside SQLite (§12.2). The test never waits for it: what it
// asserts is that the label came out and the loop kept answering WHILE the write was
// blocked, and then that the row lands once the lock is released. That the lock is
// taken at BEGIN and arbitrated by busy_timeout is pinned by
// TestTxLockImmediateTakesTheWriteLockAtBegin, in internal/store.
func TestALockedDatabaseNeverReachesTheCustomer(t *testing.T) {
	started := time.Now()
	ctx := context.Background()
	db := store.OpenTest(t)

	// weighings.product_id is a real foreign key since §10.9: the catalog has to be
	// in the database before a weighing can reference it.
	if _, err := db.ReplaceCatalog(ctx, store.Batch{
		Import: domain.Import{
			OccurredAt: epoch, Source: domain.CatalogSourceLocalDrop,
			FileName: "flv_2.csv", SHA256: "sha-verrou", Result: domain.ImportApplied,
		},
		Categories: []domain.Category{{Code: "vegetables", Label: "Légumes", Rank: 1, Visible: true}},
		Products:   garlicCatalog().Products(),
	}); err != nil {
		t.Fatalf("ReplaceCatalog : %v", err)
	}

	b := newBench(t, func(o *benchOptions) {
		o.journal = db
		// The technical journal goes nowhere on purpose: it shares the single
		// writer connection, so routing it to the same locked database would make
		// this test assert the queueing of its own log lines.
		o.technical = nopSink{}
	})

	release := lockTheWriter(t, db.Path())

	b.feed(1236, 2)
	if ack := b.tap("locked", 1236); !ack.Accepted {
		t.Fatalf("pesée refusée alors que seule la base est verrouillée : %s (%s)",
			ack.Message, ack.Code)
	}
	b.awaitPrint()

	// The loop keeps answering while the writer waits on the lock.
	for i := 0; i < 10; i++ {
		b.flush()
	}
	if got := b.hub.State().State; got == domain.Faulted {
		t.Fatal("le poste est en panne pleine page parce que la base est verrouillée")
	}
	if n, err := db.CountWeighings(ctx); err != nil || n != 0 {
		t.Fatalf("journal = %d ligne(s) (err %v) : l'écriture n'a pas attendu le verrou", n, err)
	}

	release()
	awaitCondition(t, func() bool {
		n, err := db.CountWeighings(ctx)
		return err == nil && n == 1
	}, "la pesée n'a jamais été écrite une fois le verrou relâché")

	if got := b.station.Counters().UnloggedWeighings.Load(); got != 0 {
		t.Fatalf("%d pesée(s) déclarées perdues alors que le verrou n'a fait qu'attendre", got)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("durée murale %s : le test attend un busy_timeout au lieu de l'observer", elapsed)
	}
}

// --- Shared doubles --------------------------------------------------------

// nopSink is a technical journal that swallows everything, for the tests that put
// the real database under a lock and must not have their assertions polluted by the
// technical lines the lock itself produces.
type nopSink struct{}

// RecordTechnical does nothing.
func (nopSink) RecordTechnical(context.Context, TechnicalEntry) error { return nil }

var _ TechnicalSink = nopSink{}

// lockTheWriter takes the SQLite write lock on the database at path and returns the
// function that releases it.
//
// The rival opens with _txlock=immediate, so the lock is taken at BEGIN and not at
// the first statement — that is the property §12.2 rests on and internal/store
// pins. Its own busy_timeout is 50 ms so that a mistake in this helper fails fast
// instead of costing the five seconds the production DSN grants.
func lockTheWriter(t *testing.T, path string) func() {
	t.Helper()
	rival, err := sql.Open("sqlite", "file:"+strings.ReplaceAll(path, `\`, `/`)+
		"?_pragma=busy_timeout(50)&_pragma=journal_mode(WAL)&_txlock=immediate")
	if err != nil {
		t.Fatalf("ouverture du handle concurrent : %v", err)
	}
	rival.SetMaxOpenConns(1)
	tx, err := rival.BeginTx(context.Background(), nil)
	if err != nil {
		_ = rival.Close()
		t.Fatalf("prise du verrou d'écriture : %v", err)
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			_ = tx.Rollback()
			_ = rival.Close()
		})
	}
	t.Cleanup(release)
	return release
}
