package web

import (
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
)

// The HTTP half of the recette of §16.2.
//
// Three lines of the table cannot be asserted anywhere else: 3 ter, whose refusal is a
// POST; the dashboard of 7, which is a route; and 16, whose subject is the socket. The
// twenty others live in internal/station, and internal/station/failures_test.go carries
// the index of the whole table.

// --- 3 ter: an expired measurement ------------------------------------------

// TestExpiredMeasurementRejectsWeighing is failure test 3 ter, and it is the test
// bloquant-1 exists for.
//
// A stable reading of 1 236 g, a median cadence of 400 ms — so a DERIVED expiry of
// 1 200 ms, the floor — and then the scale goes silent. At 1 199 ms of age the weight
// is still good and the label comes out; at 1 600 ms it is expired, the published
// state says so, and POST /api/v1/weigh is refused with MEASUREMENT_EXPIRED.
//
// IN BOTH STABILITY MODES, advisory included. That is the whole point: stability is
// advisory by default (A3), expiry is NOT, and the two were confused. « On n'empêche
// jamais le client de regarder un poids que la balance vient d'émettre ; on refuse
// d'imprimer un poids dont on ne sait plus s'il est encore vrai » (§6.5).
//
// The station lives 1,6 s in a few milliseconds of wall time: the clock is injected,
// and the assertion below is what keeps it that way.
func TestExpiredMeasurementRejectsWeighing(t *testing.T) {
	const (
		derivedExpiry = 1200 * time.Millisecond
		cadence       = 400 * time.Millisecond
	)

	for _, mode := range []string{domain.ModeAdvisory, domain.ModeBlocking} {
		t.Run(mode, func(t *testing.T) {
			b := newBench(t, func(o *benchOptions) {
				o.config = func(c *domain.Config) { c.Stability.Mode = mode }
			})

			// Nine frames at the nominal cadence: the rate meter needs eight
			// intervals before it trusts a median, and three times 400 ms is 1 200 ms
			// — the floor, which is what the document names.
			for i := 0; i < 9; i++ {
				b.push(1236, domain.Stable)
				b.advance(cadence)
			}
			injected, started := b.clock.Now(), time.Now()

			// --- 1 199 ms of age: still good ---------------------------------
			//
			// 400 ms have already gone by since the last frame, so 799 more make
			// 1 199. The comparison is STRICT — at the expiry itself the weight is
			// still good — and this is the half that proves the refusal below is not
			// a station that refuses everything.
			b.advance(799 * time.Millisecond)
			fresh := b.state()
			if fresh.Weight.ExpiryMS != derivedExpiry.Milliseconds() {
				t.Fatalf("péremption dérivée %d ms, attendu %d ms",
					fresh.Weight.ExpiryMS, derivedExpiry.Milliseconds())
			}
			if fresh.Weight.Expired {
				t.Fatalf("à %d ms d'âge la mesure est déclarée périmée", fresh.Weight.AgeMS)
			}
			accepted := decodeStatus[ackDTO](t,
				b.post("/api/v1/weigh", `{"product_id":"4412","seen_weight_g":1236,"key":"frais"}`),
				http.StatusAccepted)
			if !accepted.Accepted {
				t.Fatalf("pesée refusée à 1 199 ms d'âge : %+v", accepted)
			}
			b.awaitPrint()

			// --- The bag leaves, another one settles, and the scale goes silent --
			b.push(0, domain.Stable)
			b.advance(cadence)
			for i := 0; i < 3; i++ {
				b.push(1236, domain.Stable)
				b.advance(cadence)
			}

			// --- 1 600 ms of silence -----------------------------------------
			b.clock.Advance(1600 * time.Millisecond)
			b.turn()
			b.turn()

			stale := b.state()
			if !stale.Weight.Expired {
				t.Fatalf("à %d ms d'âge (péremption %d ms) la mesure doit être périmée",
					stale.Weight.AgeMS, stale.Weight.ExpiryMS)
			}
			if stale.Weight.AgeMS <= stale.Weight.ExpiryMS {
				t.Fatalf("âge %d ms, péremption %d ms : l'état publié se contredit",
					stale.Weight.AgeMS, stale.Weight.ExpiryMS)
			}

			refused := decodeStatus[ackDTO](t,
				b.post("/api/v1/weigh", `{"product_id":"4412","seen_weight_g":1236,"key":"perime"}`),
				http.StatusOK)
			if refused.Accepted {
				t.Fatal("une pesée sur un poids périmé a été acceptée")
			}
			if refused.Code != domain.CodeMeasurementExpired {
				t.Fatalf("code de refus %q, attendu %q", refused.Code, domain.CodeMeasurementExpired)
			}
			// The wording IS the masked weight: the screen hides the figure and shows
			// this sentence (§6.4, §6.5).
			if want := "Poids indisponible. Patientez ou appelez un bénévole."; refused.Message != want {
				t.Fatalf("message de refus %q, attendu %q", refused.Message, want)
			}
			if n := len(b.printer.Jobs()); n != 1 {
				t.Fatalf("%d étiquette(s) : le poids périmé en a produit une", n)
			}

			// The clock is injected: the station lived seconds, the test did not.
			lived := b.clock.Now().Sub(injected)
			elapsed := time.Since(started)
			if lived < 3*time.Second {
				t.Fatalf("le poste n'a vécu que %s : le scénario n'a pas eu lieu", lived)
			}
			if elapsed > 500*time.Millisecond {
				t.Fatalf("durée murale %s pour %s de temps station : une attente est "+
					"restée sur l'horloge réelle", elapsed, lived)
			}
		})
	}
}

// --- 7: the dashboard of a full disk ----------------------------------------

// TestAFullDiskLightsTheDashboardRedAndRefusesNobody is the web half of failure
// test 7.
//
// The database refuses every write; the label still comes out and the dashboard says
// how many weighings were lost. It is a RED light and never a refusal: what a
// volunteer needs from that screen is the exact count, so that « il manque des pesées
// au journal » stops being a suspicion (ADR-013).
//
// /healthz must stay green through all of it. It is the route a watchdog watches, and
// a station that RESTARTS because a disk filled up loses the weighing in flight to
// solve nothing at all.
func TestAFullDiskLightsTheDashboardRedAndRefusesNobody(t *testing.T) {
	b := newBench(t)
	b.store.mu.Lock()
	b.store.writeErr = errors.New("write balance.db: no space left on device")
	b.store.mu.Unlock()

	b.feed(1236, 2)
	accepted := decodeStatus[ackDTO](t,
		b.post("/api/v1/weigh", weighRequestBody), http.StatusAccepted)
	if !accepted.Accepted {
		t.Fatalf("pesée refusée sur un disque plein : %+v", accepted)
	}
	b.awaitPrint()

	state := b.awaitState(func(s stateDTO) bool { return s.UnloggedCount == 1 })
	if state.UnloggedCount != 1 {
		t.Fatalf("%d pesée(s) non journalisées publiées, attendu 1", state.UnloggedCount)
	}

	health := decodeStatus[adminHealthDTO](t, b.get("/admin/api/health"), http.StatusOK)
	if health.Counters.Unlogged != 1 {
		t.Fatalf("tableau de bord : %d pesée(s) non journalisées, attendu 1",
			health.Counters.Unlogged)
	}
	if !health.Alive {
		t.Fatal("le tableau de bord déclare le poste mort parce que le disque est plein")
	}

	live := b.get("/healthz")
	defer live.Body.Close()
	if live.StatusCode != http.StatusOK {
		t.Fatalf("/healthz = %d : un disque plein a rendu la sonde de vivacité rouge",
			live.StatusCode)
	}
}

// --- 16: two instances ------------------------------------------------------

// TestASecondInstanceCannotTakeTheSocket is failure test 16.
//
// THE SOCKET IS THE SINGLE-INSTANCE LOCK: no lock file left behind by a crash, no
// Windows named mutex, nothing to clean up by hand. A second `serve` cannot bind, so
// it cannot serve, so two processes can never answer the same screen.
//
// It also exercises the discrimination §13.4 asks for, because the two failures need
// two different sentences: an address that REFUSES a bind AND answers a probe is
// another instance (ERR-SYS-01, « une autre instance est déjà lancée ») ; one that
// refuses and answers nothing is an address this station cannot have (ERR-SYS-02).
// Sending a volunteer hunting for a ghost process is the failure this tells apart.
//
// TO REPLAY IN L8: the mapping to ERR-SYS-01 / ERR-SYS-02 and the exit code 3 belong
// to `openscale serve`, which is not written yet — cmd/openscale has no serve
// subcommand, so there is nothing here that can exit with a code.
func TestASecondInstanceCannotTakeTheSocket(t *testing.T) {
	clock := fake.NewClock(epoch)

	first, err := Listen(clock, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("première écoute : %v", err)
	}
	defer first.Close()
	address := first.Addr().String()

	second, err := Listen(clock, address, nil)
	if err == nil {
		second.Close()
		t.Fatal("une seconde instance a pris la même adresse : le verrou d'instance unique " +
			"n'existe plus, et deux processus servent le même écran")
	}
	if !containsAll(err.Error(), address) {
		t.Fatalf("le refus (%v) ne nomme pas l'adresse %q qu'un bénévole doit lire", err, address)
	}

	// The address ANSWERS: that is the probe that tells « another instance is already
	// running » from « this address cannot be bound ».
	conn, dialErr := net.DialTimeout("tcp", address, time.Second)
	if dialErr != nil {
		t.Fatalf("l'adresse refusée ne répond pas (%v) : le diagnostic enverrait un "+
			"bénévole chercher un processus fantôme", dialErr)
	}
	_ = conn.Close()

	// And an address nobody can bind fails WITHOUT answering anything: the other
	// branch of the same decision.
	unreachable, err := Listen(clock, "203.0.113.1:9", nil)
	if err == nil {
		unreachable.Close()
		t.Skip("cette machine accepte de lier une adresse qui ne lui appartient pas")
	}
	if _, dialErr := net.DialTimeout("tcp", "203.0.113.1:9", 50*time.Millisecond); dialErr == nil {
		t.Fatal("l'adresse de documentation RFC 5737 répond : la sonde ne prouve plus rien")
	}
}

// containsAll reports whether the text carries every fragment.
func containsAll(text string, fragments ...string) bool {
	for _, fragment := range fragments {
		found := false
		for i := 0; i+len(fragment) <= len(text); i++ {
			if text[i:i+len(fragment)] == fragment {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
