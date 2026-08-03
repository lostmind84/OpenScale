package webdav

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openscale/internal/catalog"
	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// The ports.CatalogSource contract over HTTP: a remote file read only once it has
// stopped moving — size AND date — an acknowledgement that archives locally and THEN
// deletes remotely, and a share that does not answer, which is logged and counted and
// never sends the watch round the loop with no delay.

// TestTheFileIsReadOnceItHasStoppedMoving: the same stability rule as the local drop,
// on the size and the date the PROPFIND reports (§10.1).
func TestTheFileIsReadOnceItHasStoppedMoving(t *testing.T) {
	remote := &share{content: aCatalog, present: true, modified: t0}
	source, _, _ := station(t, remote, nil)
	ctx := context.Background()

	if batch, err := source.poll(ctx); batch != nil || err != nil {
		t.Fatalf("lu dès la première scrutation : %v / %v", batch, err)
	}
	batch, err := source.poll(ctx)
	if err != nil || batch == nil {
		t.Fatalf("seconde scrutation : %v / %v", batch, err)
	}
	if remote.gets != 1 {
		t.Errorf("%d GET, attendu 1 : rien n'est téléchargé avant la stabilité", remote.gets)
	}
	if remote.deletes != 0 {
		t.Error("le fichier a été supprimé avant que le lot soit appliqué")
	}
	report := catalog.Summarize(batch)
	if report.RowsRead != 2 || report.Weighable != 2 {
		t.Errorf("inventaire %s", report)
	}
	if batch.Source != domain.CatalogSourceWebDAV {
		t.Errorf("provenance %q", batch.Source)
	}
}

// TestAGrowingRemoteFileIsNotRead: the size announced changes between two PROPFINDs.
func TestAGrowingRemoteFileIsNotRead(t *testing.T) {
	remote := &share{content: aCatalog[:60], present: true, modified: t0}
	source, _, _ := station(t, remote, nil)
	ctx := context.Background()

	source.poll(ctx)
	remote.content = aCatalog[:120]
	if batch, _ := source.poll(ctx); batch != nil {
		t.Fatal("un fichier dont la taille a changé a été téléchargé")
	}
	remote.content = aCatalog
	if batch, _ := source.poll(ctx); batch != nil {
		t.Fatal("un fichier dont la taille a encore changé a été téléchargé")
	}
	batch, err := source.poll(ctx)
	if err != nil || batch == nil {
		t.Fatalf("le fichier immobile n'a pas été lu : %v / %v", batch, err)
	}
	if len(batch.Products) != 2 {
		t.Errorf("%d produits : le fichier ENTIER devait être lu", len(batch.Products))
	}
}

// TestTheDateAloneMovingIsEnoughToWait: a producer that rewrites the same number of
// bytes is the case a size-only check would miss.
func TestTheDateAloneMovingIsEnoughToWait(t *testing.T) {
	remote := &share{content: aCatalog, present: true, modified: t0}
	source, _, _ := station(t, remote, nil)
	ctx := context.Background()

	source.poll(ctx)
	remote.modified = t0.Add(time.Minute)
	if batch, _ := source.poll(ctx); batch != nil {
		t.Fatal("un fichier réécrit à la même taille a été téléchargé")
	}
}

// TestAcknowledgingArchivesLocallyThenDeletesRemotely (ADR-004).
func TestAcknowledgingArchivesLocallyThenDeletesRemotely(t *testing.T) {
	remote := &share{content: aCatalog, present: true, modified: t0}
	source, _, _ := station(t, remote, map[string]any{"username": "balance", "password": "secret"})
	ctx := context.Background()

	source.poll(ctx)
	batch, err := source.poll(ctx)
	if err != nil || batch == nil {
		t.Fatalf("lecture : %v", err)
	}
	if remote.authorization == "" {
		t.Error("le compte déclaré n'a pas été présenté au partage")
	}

	if err := source.Acknowledge(ctx, batch, ports.BatchResult{Result: domain.ImportApplied}); err != nil {
		t.Fatalf("acquittement : %v", err)
	}
	if remote.deletes != 1 || remote.present {
		t.Errorf("%d DELETE, présent = %v : la suppression EST l'acquittement",
			remote.deletes, remote.present)
	}
	names := archives(t, source)
	if len(names) != 1 || names[0] != "flv_2-2026-07-24T15-38-12.csv" {
		t.Fatalf("archives %v", names)
	}
	kept, err := os.ReadFile(filepath.Join(source.archive.Directory(), names[0]))
	if err != nil || string(kept) != aCatalog {
		t.Errorf("l'archive locale ne porte pas les octets analysés (%v)", err)
	}
}

// TestAShareThatRefusesTheDeleteIsAmberAndNotQuarantined is failure test 11 over
// HTTP: the catalog is in service, only the acknowledgement failed.
func TestAShareThatRefusesTheDeleteIsAmberAndNotQuarantined(t *testing.T) {
	remote := &share{content: aCatalog, present: true, modified: t0}
	source, _, _ := station(t, remote, map[string]any{"username": "balance"})
	ctx := context.Background()

	source.poll(ctx)
	batch, _ := source.poll(ctx)
	remote.status = http.StatusForbidden

	err := source.Acknowledge(ctx, batch, ports.BatchResult{Result: domain.ImportApplied})
	if !errors.Is(err, catalog.ErrNotAcknowledged) {
		t.Fatalf("erreur %v, attendu ERR-CAT-05", err)
	}
	if errors.Is(err, catalog.ErrContent) {
		t.Error("un DELETE refusé est compté comme un échec de contenu")
	}
	if !strings.Contains(err.Error(), "balance") {
		t.Errorf("le message ne nomme pas le compte : %v", err)
	}
	if names := archives(t, source); len(names) != 1 {
		t.Errorf("archives %v : la copie locale doit exister quand même", names)
	}
}

// TestAnUnreachableShareIsLoggedAndNeverSpins: returning an error on every poll would
// send the watcher round the loop with no delay at all.
func TestAnUnreachableShareIsLoggedAndNeverSpins(t *testing.T) {
	remote := &share{status: http.StatusInternalServerError}
	source, _, _ := station(t, remote, nil)
	journal := &recorder{}
	source.log = journal
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		batch, err := source.poll(ctx)
		if batch != nil || err != nil {
			t.Fatalf("scrutation %d : %v / %v", i, batch, err)
		}
	}
	if len(journal.entries) != 3 {
		t.Fatalf("%d entrées journalisées, attendu 3", len(journal.entries))
	}
	if journal.entries[0].level != domain.LevelWarn || journal.entries[2].level != domain.LevelError {
		t.Errorf("niveaux %q puis %q : le troisième échec consécutif monte d'un cran",
			journal.entries[0].level, journal.entries[2].level)
	}
	for _, entry := range journal.entries {
		if entry.code != "ERR-CAT-03" {
			t.Errorf("code %q", entry.code)
		}
	}
}

// TestAnAbsentRemoteFileIsNothingToDo, which is the ordinary state of the share.
func TestAnAbsentRemoteFileIsNothingToDo(t *testing.T) {
	remote := &share{present: false}
	source, _, _ := station(t, remote, nil)
	for i := 0; i < 3; i++ {
		if batch, err := source.poll(context.Background()); batch != nil || err != nil {
			t.Fatalf("scrutation %d : %v / %v", i, batch, err)
		}
	}
	if remote.gets != 0 {
		t.Errorf("%d GET sur un partage vide", remote.gets)
	}
}

// TestAnUnusableRemoteFileIsSetAsideAndDeleted is failure test 9 over HTTP.
func TestAnUnusableRemoteFileIsSetAsideAndDeleted(t *testing.T) {
	remote := &share{content: "\"id\";\"nom\"\r\n\"20\";\"UNE SEULE COLONNE\"\r\n",
		present: true, modified: t0}
	source, _, _ := station(t, remote, nil)
	ctx := context.Background()

	source.poll(ctx)
	batch, err := source.poll(ctx)
	if batch != nil {
		t.Fatal("un contenu inexploitable a produit un lot")
	}
	if !errors.Is(err, catalog.ErrContent) {
		t.Fatalf("erreur %v, attendu ERR-CAT-03", err)
	}
	if remote.present || remote.deletes != 1 {
		t.Errorf("le fichier refusé n'a pas été retiré du partage (%d DELETE)", remote.deletes)
	}
	names := archives(t, source)
	if len(names) != 2 {
		t.Fatalf("archives %v, attendu la copie ET son motif", names)
	}
	reason, err := os.ReadFile(filepath.Join(source.archive.Directory(), "flv_2-2026-07-24T15-38-12.reason.txt"))
	if err != nil {
		t.Fatalf("lecture du motif : %v", err)
	}
	if !strings.Contains(string(reason), "ERR-CAT-03") {
		t.Errorf("motif : %s", reason)
	}
}

// TestNextStopsWithItsContext, on the injected clock and with no sleep (§16.4).
func TestNextStopsWithItsContext(t *testing.T) {
	remote := &share{present: false}
	source, clock, _ := station(t, remote, nil)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := source.Next(ctx)
		done <- err
	}()
	cancel()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Next a rendu %v", err)
			}
			return
		case <-deadline:
			t.Fatal("Next n'est pas sorti à l'annulation de son contexte")
		default:
			clock.Advance(5 * time.Second)
		}
	}
}

// TestAListingThatSaysNothingUsableIsNotAFileToRead.
//
// A size that is not a number is a share that answers something else; a file with no
// date at all still works, with a slightly weaker stability rule — refusing it would
// mean refusing a catalog because a server does not send `getlastmodified`.
func TestAListingThatSaysNothingUsableIsNotAFileToRead(t *testing.T) {
	remote := &share{content: aCatalog, present: true, modified: t0}
	source, _, _ := station(t, remote, nil)
	journal := &recorder{}
	source.log = journal
	ctx := context.Background()

	remote.brokenLength = true
	if batch, err := source.poll(ctx); batch != nil || err != nil {
		t.Fatalf("une taille illisible a produit %v / %v", batch, err)
	}
	if len(journal.entries) != 1 || !strings.Contains(journal.entries[0].detail, "taille annoncée") {
		t.Errorf("la taille illisible n'a pas été nommée : %+v", journal.entries)
	}

	remote.brokenLength, remote.noDate = false, true
	source.poll(ctx)
	batch, err := source.poll(ctx)
	if err != nil || batch == nil {
		t.Fatalf("un partage sans date a empêché la lecture : %v / %v", batch, err)
	}
}

// TestDeletingAFileSomebodyElseAlreadyRemovedIsASuccess: the acknowledgement has
// taken place, whoever performed it.
func TestDeletingAFileSomebodyElseAlreadyRemovedIsASuccess(t *testing.T) {
	remote := &share{content: aCatalog, present: true, modified: t0}
	source, _, _ := station(t, remote, nil)
	ctx := context.Background()

	source.poll(ctx)
	batch, _ := source.poll(ctx)
	remote.present = false // somebody else got there first

	if err := source.Acknowledge(ctx, batch, ports.BatchResult{Result: domain.ImportApplied}); err != nil {
		t.Fatalf("acquittement d'un fichier déjà parti : %v", err)
	}
}

// TestCloseIsIdempotentAndReleasesTheCopyInFlight.
func TestCloseIsIdempotentAndReleasesTheCopyInFlight(t *testing.T) {
	remote := &share{content: aCatalog, present: true, modified: t0}
	source, _, _ := station(t, remote, nil)
	ctx := context.Background()

	source.poll(ctx)
	if _, err := source.poll(ctx); err != nil {
		t.Fatalf("lecture : %v", err)
	}
	if err := source.Close(); err != nil {
		t.Fatalf("fermeture : %v", err)
	}
	if names := archives(t, source); len(names) != 0 {
		t.Errorf("archives %v, attendu aucune : la copie en vol est jetée", names)
	}
	if err := source.Close(); err != nil {
		t.Errorf("seconde fermeture : %v", err)
	}
}
