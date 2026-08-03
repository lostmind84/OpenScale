package station

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"openscale/internal/domain"
)

// What the effects of effects.go do, and what they refuse to do to the service: a
// command always answers, a saturated print worker becomes a failure the machine knows,
// a hanging printer never holds the loop, and a journal that cannot write degrades the
// JOURNAL and never the service.

// TestNoLeakOnCommandWithoutAck is the test §13.2 names.
//
// Five hundred refused commands alternated with five hundred nominal ones, then
// the goroutine count compared with the baseline AT REST, WITH NO CLIENT
// CONNECTED. Without the end-of-cycle safety net, every refusal leaks the
// goroutine of its caller.
func TestNoLeakOnCommandWithoutAck(t *testing.T) {
	b := newBench(t)
	b.push(1236, domain.Stable)
	b.tick()

	baseline := stableCount()

	ctx := context.Background()
	for i := 0; i < 500; i++ {
		// Refused: a product the catalog does not offer, then an event the current
		// state has nothing to say about.
		if _, err := b.hub.Submit(ctx, domain.ProductTapped{ProductID: "inconnu"}, ""); err != nil {
			t.Fatalf("Submit(produit inconnu) : %v", err)
		}
		if _, err := b.hub.Submit(ctx, domain.Dismiss{}, ""); err != nil {
			t.Fatalf("Submit(Dismiss hors Faulted) : %v", err)
		}
		if _, err := b.hub.Submit(ctx, domain.ReprintRequested{JobID: "jamais imprimé"}, ""); err != nil {
			t.Fatalf("Submit(réimpression impossible) : %v", err)
		}
	}

	if got := stableCount(); got > baseline {
		t.Fatalf("%d goroutines après 1 500 commandes refusées, ligne de base %d : une commande "+
			"refusée laisse son appelant en attente", got, baseline)
	}
}

// TestARefusedCommandStillAnswers checks the CONTENT of the safety net, not only
// that it fires: the answer names the state reached and refuses in French.
func TestARefusedCommandStillAnswers(t *testing.T) {
	b := newBench(t)
	ctx := context.Background()

	ack, err := b.hub.Submit(ctx, domain.ReprintRequested{JobID: "jamais imprimé"}, "")
	if err != nil {
		t.Fatalf("Submit : %v", err)
	}
	if ack.Accepted {
		t.Fatal("une réimpression impossible a été acceptée")
	}
	if ack.Message == "" {
		t.Fatal("un refus sans message : l'écran n'a rien à afficher")
	}
	if !strings.Contains(ack.Message, "réimprim") {
		t.Fatalf("message %q : il doit parler de la réimpression", ack.Message)
	}
}

// TestSubmitAnswersWhenTheHubIsGone proves the symmetric half of the contract: a
// caller never waits on the channel alone.
func TestSubmitAnswersWhenTheHubIsGone(t *testing.T) {
	b := newBench(t)
	b.station.Stop()
	<-b.station.Stopped()

	if _, err := b.hub.Submit(context.Background(), domain.Cancel{}, ""); !errors.Is(err, ErrStopped) {
		t.Fatalf("erreur %v, attendu ErrStopped", err)
	}
}

// TestASaturatedPrintWorkerBecomesAPrintFailure covers the branch of execute that
// only an ABNORMAL station reaches: the machine forbids two prints inside one
// cycle, so a full channel means the worker is stuck on a device.
//
// It is driven directly rather than through the loop, and that is the honest way
// to test it: getting there through the machine would require a third cycle to
// start while the second is still printing, which the machine refuses — the test
// would prove the setup, not the branch.
func TestASaturatedPrintWorkerBecomesAPrintFailure(t *testing.T) {
	h := newHub(Options{
		Clock: fakeClockAt(epoch), Config: loadConfig(t), Catalog: garlicCatalog(),
		Counters: &Counters{},
	})
	h.printJobs <- job{} // the worker is stuck: the one slot is taken

	ev := h.execute(domain.PrintEffect{Label: domain.Label{JobID: "j-42"}}, epoch)
	finished, ok := ev.(domain.PrintFinished)
	if !ok {
		t.Fatalf("execute rend %T, attendu un domain.PrintFinished réinjecté", ev)
	}
	if finished.JobID != "j-42" {
		t.Fatalf("job %q, attendu j-42", finished.JobID)
	}
	if !errors.Is(finished.Err, ErrPrintWorkerBusy) {
		t.Fatalf("erreur %v, attendu ErrPrintWorkerBusy", finished.Err)
	}
	select {
	case entry := <-h.technical:
		if entry.Code != "ERR-PRN-09" {
			t.Fatalf("code technique %q, attendu ERR-PRN-09", entry.Code)
		}
	default:
		t.Fatal("aucune ligne technique : la saturation du worker n'est pas journalisée")
	}
}

// TestTheHubKeepsAnsweringWhileThePrinterHangs is failure test 6 seen from the
// Hub: the device is stuck, and neither the loop nor a caller waits on it.
func TestTheHubKeepsAnsweringWhileThePrinterHangs(t *testing.T) {
	b := newBench(t)
	b.printer.Hang()
	defer b.printer.Release()

	b.feed(1236, 2)
	if ack := b.tap("hung", 1236); !ack.Accepted {
		t.Fatalf("pesée refusée : %s", ack.Message)
	}
	// The loop keeps turning and keeps answering while the device holds the worker.
	for i := 0; i < 10; i++ {
		b.tick()
	}
	if got := b.hub.State().State; got != domain.Printing {
		t.Fatalf("état %s : le cycle ne devrait pas se terminer sans réponse de l'imprimante", got)
	}
}

// TestTheJournalDegradesAndTheServiceDoesNot is ADR-013 from the failing side: the
// store refuses, the label still came out, the weighing lands in the RAM ring and
// the counter goes up.
func TestTheJournalDegradesAndTheServiceDoesNot(t *testing.T) {
	b := newBench(t)
	b.journal.mu.Lock()
	b.journal.err = errors.New("disque plein")
	b.journal.mu.Unlock()

	b.feed(1236, 2)
	if ack := b.tap("disk-full", 1236); !ack.Accepted {
		t.Fatalf("pesée refusée alors que seul le journal est en panne : %s", ack.Message)
	}
	b.awaitPrint()
	if n := len(b.printer.Jobs()); n != 1 {
		t.Fatalf("%d étiquettes : la pesée doit sortir même quand le journal ne suit pas", n)
	}
	// The store refuses, so the counter is what says so. It is written by the
	// journal worker, which is why the assertion converges instead of snapshotting.
	counters := b.station.Counters()
	for i := 0; i < 20000 && counters.UnloggedWeighings.Load() == 0; i++ {
		b.tick()
		if counters.UnloggedWeighings.Load() != 0 {
			break
		}
	}
	if got := counters.UnloggedWeighings.Load(); got != 1 {
		t.Fatalf("compteur de pesées non journalisées = %d, attendu 1", got)
	}
	// The row goes to the RAM ring, exactly as it does when the channel is
	// saturated: a disk that is full and a channel nobody drains lose the same row
	// for the same customer, and failure test 7 asks for the ring on the first of
	// the two (§16.2, ADR-013). The counter says HOW MANY were lost; only the ring
	// says WHICH ONES.
	if entries := b.hub.Entries(); len(entries) != 1 {
		t.Fatalf("%d pesée(s) dans l'anneau RAM, attendu 1", len(entries))
	}
}

// TestARefusedWeighingAnswersWithItsSafeguardCode covers the end-of-cycle safety
// net where it matters most: a blocking safeguard produces no AckEffect of its own
// in some paths, and the answer must still name the code the screen shows.
func TestARefusedWeighingAnswersWithItsSafeguardCode(t *testing.T) {
	b := newBench(t)
	// Eight grams: under min_weight_g, which is 10 on the shipped file.
	b.feed(8, 2)
	ack := b.tap("too-light", 8)
	if ack.Accepted {
		t.Fatal("une pesée de 8 g a été acceptée alors que le plancher est à 10 g")
	}
	if ack.Code != domain.CodeWeightTooLow {
		t.Fatalf("code %q, attendu %q", ack.Code, domain.CodeWeightTooLow)
	}
	if ack.Message == "" {
		t.Fatal("un refus sans message : le client ne sait pas quoi corriger")
	}
	if n := len(b.printer.Jobs()); n != 0 {
		t.Fatalf("%d étiquettes pour une pesée refusée", n)
	}

	// And the refusal IS journalled, with what it would have cost.
	row := b.awaitJournal()
	if row.Result != domain.ResultRejected {
		t.Fatalf("résultat %q, attendu %q", row.Result, domain.ResultRejected)
	}
	if row.Detail == "" {
		t.Fatal("le journal ne dit pas pourquoi la pesée a été refusée")
	}
}

// TestDefaultAckNamesTheStateAndNeverAJobID is the unit of the safety net.
func TestDefaultAckNamesTheStateAndNeverAJobID(t *testing.T) {
	rejected := domain.Model{
		State: domain.Rejected,
		Diagnostics: []domain.Diagnostic{
			{Code: domain.CodeWeightTooLow, Severity: domain.Blocking,
				Message: domain.DefaultMessage(domain.CodeWeightTooLow)},
		},
	}
	ack := defaultAck(rejected, domain.ProductTapped{})
	if ack.Accepted {
		t.Fatal("le filet de fin de cycle prétend accepter : il n'est pas un accusé d'acceptation")
	}
	if ack.State != domain.Rejected || ack.Code != domain.CodeWeightTooLow {
		t.Fatalf("accusé %+v : il doit porter l'état atteint et le code bloquant", ack)
	}
	if ack.JobID != "" {
		t.Fatal("le filet de fin de cycle a inventé un JobID : aucune étiquette n'est partie")
	}

	silent := defaultAck(domain.Model{State: domain.Idle}, domain.Dismiss{})
	if silent.Message == "" {
		t.Fatal("un événement ignoré rend un accusé muet : l'appelant n'a rien à afficher")
	}
}

// TestTheNumberOfCopiesComesFromTheConfiguration, and a count left at zero is ONE:
// a station that prints nothing because a field is empty is a station nobody can
// debug.
func TestTheNumberOfCopiesComesFromTheConfiguration(t *testing.T) {
	b := newBench(t, func(o *benchOptions) {
		o.config = func(c *domain.Config) {
			c.Printer.Options["copies"] = json.RawMessage(`3`)
		}
	})
	b.feed(1236, 2)
	b.tap("three-copies", 1236)
	b.awaitPrint()
	if got := b.printer.Jobs()[0].Copies; got != 3 {
		t.Fatalf("%d exemplaires, attendu 3", got)
	}

	zero := newBench(t, func(o *benchOptions) {
		o.config = func(c *domain.Config) {
			c.Printer.Options["copies"] = json.RawMessage(`0`)
		}
	})
	zero.feed(1236, 2)
	zero.tap("zero-copies", 1236)
	zero.awaitPrint()
	if got := zero.printer.Jobs()[0].Copies; got != 1 {
		t.Fatalf("%d exemplaires pour un réglage à zéro, attendu 1", got)
	}
}
