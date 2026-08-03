package station

import (
	"context"
	"errors"
	"testing"
	"time"

	"openscale/internal/domain"
)

// What reaches a screen, and when: the throttle and the forced heartbeat of §13.3, a
// slow subscriber that never holds the loop back, and the two things a snapshot carries
// that expire on their own — the banner and the reprint bar.

// TestAChangeInsideTheThrottleGoesOutOnTheNextTick pins both halves of the
// publication rule at once: nothing is emitted twice inside 100 ms, and what was
// held back is emitted on the very next tick rather than waiting for the 500 ms
// heartbeat.
func TestAChangeInsideTheThrottleGoesOutOnTheNextTick(t *testing.T) {
	b := newBench(t)
	snapshots, unsubscribe := b.hub.Subscribe()
	defer unsubscribe()
	<-snapshots // the state a new subscriber gets at once

	b.tick() // a publication happens here, and fixes the throttle window
	drain(snapshots)

	// Same instant as the last publication: the change is held back.
	b.push(1236, domain.Stable)
	b.flush()
	if len(snapshots) != 0 {
		t.Fatal("un changement a été publié dans les 100 ms de la publication précédente")
	}

	b.tick()
	if len(snapshots) != 1 {
		t.Fatal("le snapshot retenu par le throttle n'est jamais parti")
	}
	if got := (<-snapshots).Weight.Gross; got != 1236 {
		t.Fatalf("poids publié %d g, attendu 1236 g", got)
	}
}

// drain empties a subscriber channel without blocking.
func drain(snapshots <-chan Snapshot) {
	for {
		select {
		case <-snapshots:
		default:
			return
		}
	}
}

// TestPublicationIsThrottledAndStillBeats checks both halves of §13.3 at once:
// nothing is published twice in 100 ms, and something is published at least every
// 500 ms even when nothing changes.
func TestPublicationIsThrottledAndStillBeats(t *testing.T) {
	b := newBench(t)
	snapshots, unsubscribe := b.hub.Subscribe()
	defer unsubscribe()
	<-snapshots // the state a new subscriber gets at once

	// Nothing changes for two seconds: the forced heartbeat must still fire, and
	// it must not fire ten times a second.
	beats := 0
	for i := 0; i < 20; i++ {
		b.tick()
		select {
		case <-snapshots:
			beats++
		default:
		}
	}
	if beats == 0 {
		t.Fatal("aucun battement en 2 s : un navigateur reconnecté resterait sur un bandeau figé")
	}
	if beats > 8 {
		t.Fatalf("%d battements en 2 s alors que rien ne change : le throttle ne tient pas", beats)
	}
}

// TestASlowSubscriberNeverHoldsTheHub proves the drop-old rule: a subscriber that
// stops reading gets the stale snapshot dropped, never the loop blocked.
func TestASlowSubscriberNeverHoldsTheHub(t *testing.T) {
	b := newBench(t)
	snapshots, unsubscribe := b.hub.Subscribe()
	defer unsubscribe()

	// Never read from snapshots, and keep the station busy.
	for i := 0; i < 50; i++ {
		b.push(domain.Grams(1000+i), domain.Stable)
		b.tick()
	}
	if got := b.hub.State().Weight.Gross; got != 1049 {
		t.Fatalf("dernier poids %d g, attendu 1049 g : la boucle a été retenue par un abonné", got)
	}
	if n := len(snapshots); n > subscriberDepth {
		t.Fatalf("%d snapshots en attente, capacité %d", n, subscriberDepth)
	}
}

// TestABannerExpiresOnTheInjectedClock: what really ends a message is the physical
// signal, and these durations only bound a message nobody came back for.
func TestABannerExpiresOnTheInjectedClock(t *testing.T) {
	b := newBench(t)
	b.feed(8, 2)
	b.tap("too-light", 8)
	b.tick()

	message := b.hub.State().Message
	if message == nil {
		t.Fatal("aucun bandeau après un refus")
	}
	if message.Code != domain.CodeWeightTooLow {
		t.Fatalf("code du bandeau %q, attendu %q", message.Code, domain.CodeWeightTooLow)
	}

	b.advance(domain.RejectMessageDuration + time.Second)
	if got := b.hub.State().Message; got != nil {
		t.Fatalf("le bandeau %q survit à sa durée", got.Text)
	}
}

// TestTheScaleLossBannerHasNoExpiry: a station with no scale does not stop having
// no scale because five seconds went by.
func TestTheScaleLossBannerHasNoExpiry(t *testing.T) {
	b := newBench(t)
	b.disconnect(errors.New("câble débranché"))
	b.tick()

	message := b.hub.State().Message
	if message == nil {
		t.Fatal("aucun bandeau après la perte de la balance")
	}
	if !message.ExpiresAt.IsZero() {
		t.Fatalf("le bandeau de perte de balance expire à %s", message.ExpiresAt)
	}
	b.advance(time.Hour)
	if b.hub.State().Message == nil {
		t.Fatal("le bandeau de perte de balance a disparu tout seul")
	}
}

// TestTheReprintBarIsPermanentInsideItsWindow is §8.5: one reprint per label,
// marked, inside reprint_window_s.
func TestTheReprintBarIsPermanentInsideItsWindow(t *testing.T) {
	b := newBench(t)
	b.feed(1236, 2)
	first := b.tap("original", 1236)
	b.awaitJournal()
	b.tick()

	s := b.hub.State()
	if !s.ReprintAvailable {
		t.Fatal("la barre de réimpression n'est pas active juste après une étiquette")
	}

	ctx := context.Background()
	ack, err := b.hub.Submit(ctx, domain.ReprintRequested{JobID: first.JobID, Key: "reprint-1"}, "reprint-1")
	if err != nil {
		t.Fatalf("Submit(ReprintRequested) : %v", err)
	}
	if !ack.Accepted {
		t.Fatalf("réimpression refusée dans sa fenêtre : %s", ack.Message)
	}
	row := b.awaitJournal()
	if row.Result != domain.ResultReprint {
		t.Fatalf("résultat %q, attendu %q", row.Result, domain.ResultReprint)
	}

	// One reprint only.
	again, err := b.hub.Submit(ctx, domain.ReprintRequested{Key: "reprint-2"}, "reprint-2")
	if err != nil {
		t.Fatalf("Submit : %v", err)
	}
	if again.Accepted {
		t.Fatal("une seconde réimpression a été acceptée")
	}
	if n := len(b.printer.Jobs()); n != 2 {
		t.Fatalf("%d étiquettes, attendu 2 (l'originale et sa réimpression)", n)
	}
}
