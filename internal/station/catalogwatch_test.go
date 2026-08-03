package station

import (
	"context"
	"errors"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// The catalog watch across a reload: a source that is replaced while the watch is
// parked inside it, one that could not be built, and one that arrives on a station
// that started without any. The watch must never stay reading the source it no longer
// has.

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
