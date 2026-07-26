package web

import (
	"net/http"
	"testing"
	"time"

	"openscale/internal/domain"
)

// weighRequestBody is the EXACT body the screen sends on a single product tap
// (§16.3). Replaying it verbatim is what makes the double-tap assertion mean
// something: a test that sent a different body twice would be testing its own helper.
const weighRequestBody = `{"product_id":"4412","seen_weight_g":1236,"key":"01J9F2ABC"}`

// TestWeighingEndToEnd is the test that is worth all the others (§16.3).
//
// It proves that one tap yields one label, that WHAT IS PRINTED IS WHAT WAS DISPLAYED,
// and that a repeated idempotency key prints nothing more. No scale, no printer, no
// network, no browser and no sleep.
func TestWeighingEndToEnd(t *testing.T) {
	b := newBench(t)
	b.feed(1236, 2)

	response := b.post("/api/v1/weigh", weighRequestBody)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /api/v1/weigh = %d, attendu 202 : %s",
			response.StatusCode, body(t, response))
	}
	ack := decode[ackDTO](t, response)
	if !ack.Accepted || ack.JobID == "" {
		t.Fatalf("accusé = %+v, attendu une acceptation avec un identifiant de travail", ack)
	}
	b.awaitPrint()

	printed := b.printer.Jobs()
	if len(printed) != 1 {
		t.Fatalf("%d travaux d'impression, attendu 1", len(printed))
	}
	if got := string(printed[0].Label.Barcode); got != garlicBarcode {
		t.Fatalf("code-barres imprimé = %q, attendu %q", got, garlicBarcode)
	}

	shown := b.awaitState(func(s stateDTO) bool { return s.LastLabel != nil })
	if shown.LastLabel.Barcode != garlicBarcode {
		t.Fatalf("AFFICHÉ (%q) diffère de IMPRIMÉ (%q)",
			shown.LastLabel.Barcode, printed[0].Label.Barcode)
	}
	// A6: half-up rounding, on the reference vector of §18.
	amounts := amountsOf(shown.LastLabel)
	if amounts["MEMBER"] != "5,92" || amounts["SOLIDARITY"] != "6,58" {
		t.Fatalf("montants = %v, attendu MEMBER 5,92 et SOLIDARITY 6,58", amounts)
	}

	// Double tap: the SAME key, no second label.
	second := b.post("/api/v1/weigh", weighRequestBody)
	if second.StatusCode != http.StatusAccepted {
		t.Fatalf("second POST = %d, attendu 202", second.StatusCode)
	}
	if replayed := decode[ackDTO](t, second); replayed != ack {
		t.Fatalf("le second accusé (%+v) diffère du premier (%+v) : ce n'est plus un rejeu",
			replayed, ack)
	}
	if n := len(b.printer.Jobs()); n != 1 {
		t.Fatalf("%d travaux d'impression, attendu 1", n)
	}
}

// TestTheSameKeyTwiceYieldsOneJobAndTwoIdenticalAnswers is failure test 15, stated as
// the mission states it: same key twice, one job, two identical 202.
//
// The two answers being IDENTICAL is the part that matters. A second answer that
// merely said « already done » would force the front end to have a second code path
// for a case it cannot tell apart from the first — a retry after a lost response looks
// exactly like a double tap from where the browser sits.
func TestTheSameKeyTwiceYieldsOneJobAndTwoIdenticalAnswers(t *testing.T) {
	b := newBench(t)
	b.feed(1236, 2)

	first := decodeStatus[ackDTO](t, b.post("/api/v1/weigh", weighRequestBody), http.StatusAccepted)
	b.awaitPrint()
	second := decodeStatus[ackDTO](t, b.post("/api/v1/weigh", weighRequestBody), http.StatusAccepted)

	if first != second {
		t.Fatalf("réponses différentes : %+v puis %+v", first, second)
	}
	if n := len(b.printer.Jobs()); n != 1 {
		t.Fatalf("%d étiquettes pour une clé rejouée, attendu 1", n)
	}
}

// TestAProductTheCatalogDoesNotOfferIsRefusedInFrench: the command was understood, so
// it is not a 4xx; it was refused, so it is not a 202.
func TestAProductTheCatalogDoesNotOfferIsRefusedInFrench(t *testing.T) {
	b := newBench(t)
	b.feed(1236, 2)

	response := b.post("/api/v1/weigh", `{"product_id":"9999","seen_weight_g":1236,"key":"k1"}`)
	ack := decodeStatus[ackDTO](t, response, http.StatusOK)
	if ack.Accepted {
		t.Fatal("un produit absent du catalogue a été accepté")
	}
	if ack.Code != domain.CodeProductWithdrawn || ack.Message == "" {
		t.Fatalf("refus = %+v, attendu le code %s et un message français",
			ack, domain.CodeProductWithdrawn)
	}
}

// TestAWeighWithoutAProductIsARequestError separates the two refusals: a body that
// names no product is malformed, and that is a 400 the front end must fix, not a
// business answer a customer reads.
func TestAWeighWithoutAProductIsARequestError(t *testing.T) {
	b := newBench(t)
	for _, sent := range []string{`{}`, `{"key":"k"}`, `pas du json`, `{"inconnu":1}`} {
		response := b.post("/api/v1/weigh", sent)
		if response.StatusCode != http.StatusBadRequest {
			t.Errorf("POST %s = %d, attendu 400", sent, response.StatusCode)
		}
		response.Body.Close()
	}
}

// TestAManualWeightPrintsOnceAndReplaysExactly covers the one route of §14.5 that
// carries two events.
//
// The screen sends ONE request with manual_weight_g; the handler taps the tile and
// confirms the mass. Replaying the request must print nothing more — which is what the
// derived key buys, and what a shared key would break.
func TestAManualWeightPrintsOnceAndReplaysExactly(t *testing.T) {
	b := newBench(t, func(o *benchOptions) {
		o.config = func(cfg *domain.Config) {
			cfg.Scale.Present = false
			cfg.Scale.ManualEntryAllowed = true
		}
	})
	// A station with no scale rests in ManualMode, which it reaches on a tick.
	b.advance(200 * time.Millisecond)

	const request = `{"product_id":"4412","manual_weight_g":1236,"key":"01J9MANUAL"}`
	first := decodeStatus[ackDTO](t, b.post("/api/v1/weigh", request), http.StatusAccepted)
	b.awaitPrint()
	if n := len(b.printer.Jobs()); n != 1 {
		t.Fatalf("%d étiquettes, attendu 1", n)
	}

	second := decodeStatus[ackDTO](t, b.post("/api/v1/weigh", request), http.StatusAccepted)
	if first != second {
		t.Fatalf("le rejeu répond %+v au lieu de %+v", second, first)
	}
	if n := len(b.printer.Jobs()); n != 1 {
		t.Fatalf("%d étiquettes après rejeu, attendu 1", n)
	}
}

// TestCancelAndDismissAnswerFromEveryState: two routes, one line each, and the reason
// they exist is that a screen must always have a way out.
func TestCancelAndDismissAnswerFromEveryState(t *testing.T) {
	b := newBench(t)
	b.feed(1236, 2)

	if response := b.post("/api/v1/cancel", `{}`); response.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /api/v1/cancel = %d, attendu 202", response.StatusCode)
	}
	// Dismiss outside a fault is a no-op the machine ignores, and the end-of-cycle
	// safety net is what still answers it: 200 and a French sentence, never a hang.
	response := b.post("/api/v1/dismiss", `{}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/v1/dismiss = %d, attendu 200", response.StatusCode)
	}
	if ack := decode[ackDTO](t, response); ack.Message == "" {
		t.Fatal("un acquittement sans objet ne dit rien à l'écran")
	}
}

// TestReprintIsRefusedWhenThereIsNothingToReprint.
func TestReprintIsRefusedWhenThereIsNothingToReprint(t *testing.T) {
	b := newBench(t)
	ack := decodeStatus[ackDTO](t, b.post("/api/v1/reprint", `{"job_id":"","key":"r1"}`),
		http.StatusOK)
	if ack.Accepted {
		t.Fatal("une réimpression sans étiquette a été acceptée")
	}
}

// TestABrowserErrorReachesTheTechnicalJournal: ERR-UI-01 is what turns « l'écran est
// figé » into a line somebody can read afterwards.
func TestABrowserErrorReachesTheTechnicalJournal(t *testing.T) {
	b := newBench(t)
	response := b.post("/api/v1/ui/error", `{"message":"TypeError","stack":"at f (app.js:1)"}`)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /api/v1/ui/error = %d, attendu 202", response.StatusCode)
	}
	response.Body.Close()
	if !b.technical.has("ERR-UI-01") {
		t.Fatal("l'erreur du navigateur n'a pas été journalisée")
	}
}

// TestCommandsAreRefusedOnceTheStationStops.
func TestCommandsAreRefusedOnceTheStationStops(t *testing.T) {
	b := newBench(t)
	b.station.Stop()

	response := b.post("/api/v1/weigh", weighRequestBody)
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("POST après l'arrêt = %d, attendu 503", response.StatusCode)
	}
}

// --- Helpers ----------------------------------------------------------------

// awaitState advances the clock until the published state satisfies the condition.
//
// Advancing is necessary and is not a wait: publish throttles at 10 Hz, so a change
// decided on a frozen clock is held until the next tick. On the fake clock those ticks
// cost microseconds.
func (b *bench) awaitState(holds func(stateDTO) bool) stateDTO {
	b.t.Helper()
	for i := 0; i < 50; i++ {
		if got := b.state(); holds(got) {
			return got
		}
		b.advance(200 * time.Millisecond)
	}
	b.t.Fatal("l'état publié n'a jamais satisfait la condition attendue")
	return stateDTO{}
}

// amountsOf indexes the price lines of a label by tier code, which is how §16.3 reads
// them.
func amountsOf(label *labelDTO) map[string]string {
	out := make(map[string]string, len(label.Prices))
	for _, price := range label.Prices {
		out[price.Code] = price.AmountText
	}
	return out
}

// decodeStatus reads one body after asserting its status.
func decodeStatus[T any](t *testing.T, response *http.Response, want int) T {
	t.Helper()
	if response.StatusCode != want {
		t.Fatalf("statut %d, attendu %d : %s", response.StatusCode, want, body(t, response))
	}
	return decode[T](t, response)
}
