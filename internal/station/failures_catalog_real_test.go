package station

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openscale/internal/catalog"
	"openscale/internal/domain"
	"openscale/internal/station/ports"
	"openscale/internal/store"
)

// The same seven lines, replayed against internal/catalog AS IT IS SHIPPED. A failure
// test written against a double proves the double; these are the ones that prove the
// station. The bench they run on is in failures_catalog_bench_test.go.

// TestACatalogFileStillGrowingIsNotReadAgainstTheRealDrop is failure test 8, replayed
// against the poll loop of internal/catalog/localdrop.
//
// The assertion that carries it is the ARCHIVE: the copy is written WHILE the file is
// read, so an empty archive directory is proof that nothing was read at all — and that
// is stronger than counting reads, because it is the artefact production leaves behind.
func TestACatalogFileStillGrowingIsNotReadAgainstTheRealDrop(t *testing.T) {
	initial := garlicCatalog()
	watch := &pollByPoll{ask: make(chan struct{}), done: make(chan struct{})}
	b := newRealBench(t, func(o *realOptions) {
		o.catalog = initial
		o.wrap = func(s ports.CatalogSource) ports.CatalogSource {
			watch.CatalogSource = s
			return watch
		}
	})

	lines := rows(t, fixtureBytes(t, "flv_1.csv"))
	for _, upto := range []int{20, 60, 100, 140} {
		b.dropContent(join(lines[:upto]))
		// ONE poll, and it has finished when this returns: whatever it saw, it saw this
		// size and no other.
		watch.once(t)
		b.flush()
		// A COMMITTED archive is the proof that a file was read to the end and
		// acknowledged. The « .part » of a copy in flight is not one, and counting it as
		// such is what turned this red on a loaded runner the first time.
		if names := b.archived(); len(names) != 0 {
			t.Fatalf("archives %v : la copie est écrite PENDANT la lecture, donc un fichier "+
				"dont la taille bouge encore a été lu", names)
		}
		if _, err := os.Stat(b.path); err != nil {
			t.Fatalf("le fichier a disparu sans avoir été acquitté : %v", err)
		}
		if b.hub.Catalog() != initial {
			t.Fatal("le catalogue a été remplacé par un fichier en cours d'écriture")
		}
	}

	// Immobile at last. What takes service is the WHOLE file — 107 tiles — and never
	// one of the four truncations above. Two identical polls are what the rule asks for,
	// and the loop below is what grants them.
	b.dropContent(join(lines))
	awaitCondition(t, func() bool {
		watch.once(t)
		b.flush()
		return b.hub.Catalog() != nil && b.hub.Catalog().WeighableCount() == firstTiles
	}, fmt.Sprintf("la grille n'a jamais compté %d tuiles", firstTiles))
	if grid := b.hub.Catalog(); grid.Len() != firstRows {
		t.Errorf("%d produits en base, attendu les %d lignes du fichier", grid.Len(), firstRows)
	}
	awaitCondition(t, func() bool {
		watch.once(t)
		_, err := os.Stat(b.path)
		return errors.Is(err, os.ErrNotExist)
	}, "le fichier déposé n'a jamais disparu : l'acquittement EST la suppression")
	if names := b.archived(); len(names) != 1 {
		t.Errorf("archives %v, attendu une seule copie : celle du fichier complet", names)
	}
}

// TestACorruptedCatalogIsQuarantinedAgainstTheRealChain is failure test 9, replayed
// against the real parser, the real archive and the real quarantine table.
//
// Three drops of the same unusable content. Each one is set aside with its reason and
// REMOVED — leaving it would re-read the same broken file every five seconds for ever —
// the catalog in service is never touched, and the third failure is the one that turns
// the light red.
func TestACorruptedCatalogIsQuarantinedAgainstTheRealChain(t *testing.T) {
	initial := garlicCatalog()
	b := newRealBench(t, func(o *realOptions) { o.catalog = initial })

	broken := []byte("\"id\";\"nom\"\r\n\"20\";\"UNE SEULE COLONNE UTILE\"\r\n")
	sha := digest(broken)

	for attempt := 1; attempt <= 3; attempt++ {
		b.dropContent(broken)
		b.awaitFileGone()
		entry, banned := b.quarantine(sha)
		if !banned {
			t.Fatalf("essai %d : rien en quarantaine sous le sha du fichier refusé", attempt)
		}
		if entry.FailureCount != attempt {
			t.Fatalf("essai %d : %d échec(s) comptés", attempt, entry.FailureCount)
		}
	}

	entry, _ := b.quarantine(sha)
	if entry.Code != "ERR-CAT-03" {
		t.Errorf("code %q en quarantaine, attendu ERR-CAT-03", entry.Code)
	}
	threshold, ok := b.hub.Config().Catalog.Options.Int("failures_before_reject")
	if !ok || int64(entry.FailureCount) < threshold {
		t.Errorf("%d échecs pour un seuil de %d", entry.FailureCount, threshold)
	}
	// The catalog N−1 served throughout.
	b.scan()
	if b.hub.Catalog() != initial {
		t.Fatal("un contenu refusé a remplacé le catalogue N−1")
	}
	// Three copies and three reasons: somebody has to be able to see it happened three
	// times, without a database.
	names := b.archived()
	if len(names) != 6 {
		t.Fatalf("archives %v, attendu trois copies et trois motifs", names)
	}
	reason, err := os.ReadFile(filepath.Join(b.archives, names[len(names)-1]))
	if err != nil || !strings.Contains(string(reason), "ERR-CAT-03") {
		t.Errorf("le motif archivé ne nomme pas le code : %s / %v", reason, err)
	}
	// The light only goes RED on the third refusal: a producer who corrects the file
	// after one bad export must not find a station that has already given up.
	if got := b.linesWithLevel("ERR-CAT-03", domain.LevelError); got != 1 {
		t.Errorf("%d ligne(s) ERR-CAT-03 en niveau erreur, attendu 1 — celle du "+
			"troisième refus", got)
	}
}

// TestTheSameCatalogTwiceAgainstTheRealFile is failure test 10 on the authentic file.
//
// A producer may drop a byte-identical export every night: two rows in `imports`,
// `applied` then `unchanged`, no red light, no quarantine — and not one photo rewritten,
// because the sha IS the address (§10.7).
func TestTheSameCatalogTwiceAgainstTheRealFile(t *testing.T) {
	b := newRealBench(t)

	b.drop("flv.csv")
	b.awaitTiles(realTiles)
	b.awaitFileGone()
	written := b.photoFiles()

	b.drop("flv.csv")
	b.awaitFileGone()
	history := b.awaitImports(2)

	if len(history) != 2 {
		t.Fatalf("%d ligne(s) dans imports, attendu 2 : l'historique est append-only", len(history))
	}
	if history[1].Result != domain.ImportApplied || history[0].Result != domain.ImportUnchanged {
		t.Fatalf("résultats %q puis %q, attendu %q puis %q",
			history[1].Result, history[0].Result, domain.ImportApplied, domain.ImportUnchanged)
	}
	if history[0].SHA256 != history[1].SHA256 {
		t.Error("les deux lignes ne portent pas le même sha")
	}
	if _, banned := b.quarantine(history[0].SHA256); banned {
		t.Fatal("un catalogue valide déposé deux fois a été mis en quarantaine")
	}
	if b.technical.has("ERR-CAT-03") || b.technical.has("ERR-CAT-05") {
		t.Fatal("déposer deux fois le même fichier a allumé un feu")
	}
	if b.hub.State().Message != nil {
		t.Fatalf("bandeau client « %s » pour un second dépôt", b.hub.State().Message.Text)
	}
	if got := b.photoFiles(); got != written {
		t.Errorf("%d photos sur le disque après le second dépôt, %d après le premier : "+
			"réimporter le même catalogue ne doit écrire aucun fichier", got, written)
	}
	if b.hub.Catalog().WeighableCount() != realTiles {
		t.Errorf("%d tuiles après le second dépôt", b.hub.Catalog().WeighableCount())
	}
}

// undeletable is a REAL local drop whose acknowledgement fails the way a read-only
// directory makes it fail.
//
// It is the one injection of this file, and it is at the syscall: no portable file
// system produces a file that can be read and not deleted — Windows clears a read-only
// attribute by itself, Unix decides by the directory. Everything else here is the real
// thing, and the source-side half of the line is exercised in
// internal/catalog/localdrop, where the seam lives.
type undeletable struct {
	ports.CatalogSource
	directory string
}

// Acknowledge refuses, and never touches the file — which is exactly the state §16.2
// line 11 describes: read, applied, still there.
func (u undeletable) Acknowledge(context.Context, *ports.Batch, ports.BatchResult) error {
	return fmt.Errorf("%w : droits en écriture manquants sur %s pour le compte balance",
		catalog.ErrNotAcknowledged, u.directory)
}

// TestACatalogFileThatCannotBeDeletedAgainstTheRealApplier is failure test 11, and the
// trap of §16.2 line 11.
//
// The file was read and APPLIED; only its removal failed. That is ERR-CAT-05, an amber
// light, and it must NEVER count against the quarantine: the file is not corrupted, the
// directory is. A red light that fires wrongly is the worst enemy of operations,
// because after three false alarms the team stops looking at the lights.
func TestACatalogFileThatCannotBeDeletedAgainstTheRealApplier(t *testing.T) {
	initial := garlicCatalog()
	b := newRealBench(t, func(o *realOptions) {
		o.catalog = initial
		o.wrap = func(s ports.CatalogSource) ports.CatalogSource {
			return undeletable{CatalogSource: s, directory: `\\serveur\balance\`}
		}
	})

	content := fixtureBytes(t, "flv_1.csv")
	b.dropContent(content)

	// The catalog it carried takes service all the same.
	b.awaitTiles(firstTiles)
	awaitCondition(t, func() bool {
		b.scan()
		return b.technical.has("ERR-CAT-05")
	}, "ERR-CAT-05 n'a pas été journalisé alors que le fichier n'a pas pu être supprimé")

	if b.technical.has("ERR-CAT-03") {
		t.Fatal("ERR-CAT-03 journalisé : une suppression impossible a été prise pour un " +
			"échec de contenu")
	}
	if _, banned := b.quarantine(digest(content)); banned {
		t.Fatal("le contenu a été mis en quarantaine alors que seule sa suppression a échoué")
	}
	if _, err := os.Stat(b.path); err != nil {
		t.Fatalf("le fichier a disparu : %v", err)
	}
	// It is read again and again, and every re-reading is `unchanged`: a station whose
	// share is read-only keeps serving, and nothing accumulates but history.
	//
	// The applied one is named BY ITS IDENTITY and not by its rank, because the number of
	// re-readings is bounded by nothing at all: `Next` polls the moment it is entered, so
	// a file nobody can delete is read again as fast as the loop turns. Twenty rows —
	// the window `imports()` reads, which is the window the screen shows — scroll past in
	// well under a second on a slow machine, and « le premier import » silently became
	// « le vingtième ». Measured here: two rows on this machine, more than twenty as soon
	// as anything delays the test.
	applied, err := b.db.LastAppliedImport(context.Background())
	if err != nil {
		t.Fatalf("aucun import appliqué : la première lecture n'a rien mis en service : %v", err)
	}
	for _, row := range b.awaitImports(2) {
		if row.ID == applied.ID {
			continue
		}
		if row.Result != domain.ImportUnchanged {
			t.Fatalf("import %d journalisé %q, attendu %q : une seule lecture applique, "+
				"toutes les autres sont des relectures", row.ID, row.Result, domain.ImportUnchanged)
		}
	}
}

// TestAnAmputatedCatalogIsRefusedAgainstTheRealGuard is failure test 12, replayed on a
// column shift of the AUTHENTIC file.
//
// Every one of the 355 rows is still perfectly readable, so the absolute guard of
// §10.4a sees nothing at all; not one product is weighable any more, which is exactly
// the grandeur the relative guard watches.
func TestAnAmputatedCatalogIsRefusedAgainstTheRealGuard(t *testing.T) {
	b := newRealBench(t)
	b.drop("flv.csv")
	b.awaitTiles(realTiles)
	b.awaitFileGone()

	shifted := join(shiftColumns(t, rows(t, fixtureBytes(t, "flv.csv"))))
	b.dropContent(shifted)
	b.awaitFileGone()
	history := b.awaitImports(2)

	refused := history[0]
	if refused.Result != domain.ImportRejected || refused.Code != "ERR-CAT-03" {
		t.Fatalf("ligne d'import %+v, attendu un refus ERR-CAT-03", refused)
	}
	if refused.RowsRead != realRows || refused.UnreadableRows != 0 {
		t.Errorf("%d lignes lues dont %d illisibles : le garde ABSOLU ne voit rien, "+
			"c'est le garde RELATIF qui doit refuser", refused.RowsRead, refused.UnreadableRows)
	}
	if refused.Weighable != 0 {
		t.Errorf("%d pesables après un décalage de colonne", refused.Weighable)
	}
	for _, expected := range []string{
		"0 produit pesable reçu contre 331", "90 %",
		domain.FindingPriceUnreadable, "par exemple ligne", "reste en service",
	} {
		if !strings.Contains(refused.Reason, expected) {
			t.Errorf("le motif du refus ne contient pas %q :\n%s", expected, refused.Reason)
		}
	}
	// The catalog N−1 kept serving, whole.
	b.scan()
	if got := b.hub.Catalog().WeighableCount(); got != realTiles {
		t.Fatalf("%d tuiles en service, attendu les %d du catalogue N−1", got, realTiles)
	}
	// A content failure, so it counts — and the reason is next to the archived copy.
	entry, banned := b.quarantine(digest(shifted))
	if !banned || entry.FailureCount != 1 {
		t.Errorf("quarantaine %+v", entry)
	}
	if !hasReason(b.archived()) {
		t.Errorf("aucun .reason.txt à côté de la copie refusée : %v", b.archived())
	}
	awaitTechnical(t, b.technical, "ERR-CAT-03", "aucune ligne technique : le feu rouge n'a rien à afficher")
}

// hasReason reports whether a refusal left its explanation next to a copy.
func hasReason(names []string) bool {
	for _, name := range names {
		if strings.HasSuffix(name, ".reason.txt") {
			return true
		}
	}
	return false
}

// TestAnOrdinaryCatalogLightsNothingRedAgainstTheRealFiles is failure test 12 bis and
// the acceptance criterion of §18, on the two AUTHENTIC files and nothing else.
//
// Every figure below was MEASURED on the files of this repository. Two of them correct
// the document, and the correction is arithmetic rather than opinion: §18 says « 331
// tuiles dont 181 avec photo et 174 sans » and §16.2 says « 331 tuiles dont 174 sans
// photo », but 181 + 174 = 355, which is the number of ROWS. Of the 331 TILES, 177
// carry a photo and 154 do not.
func TestAnOrdinaryCatalogLightsNothingRedAgainstTheRealFiles(t *testing.T) {
	for _, c := range []struct {
		file                                          string
		rowsRead, tiles, notWeighable, issues, units  int
		photoRows, photoFiles                         int
		tilesWithPhoto, tilesWithoutPhoto, otherTiles int
	}{
		{"flv.csv", realRows, realTiles, realNotWeighable, realIssues, realUnitMismatch,
			realPhotoRows, realPhotoFiles, realTilesWithPhoto, realTilesWithoutPhoto, realOtherTiles},
		{"flv_1.csv", firstRows, firstTiles, 39, 7, 5, 0, 0, 0, firstTiles, 1},
	} {
		t.Run(c.file, func(t *testing.T) {
			// The inventory has to recompose, or every assertion below means nothing.
			if c.tiles+c.notWeighable+c.issues != c.rowsRead {
				t.Fatalf("%d + %d + %d ≠ %d", c.tiles, c.notWeighable, c.issues, c.rowsRead)
			}
			b := newRealBench(t) // a virgin station: the grid says « Catalogue vide »
			b.drop(c.file)
			grid := b.awaitTiles(c.tiles)
			b.awaitFileGone()

			if grid.Len() != c.rowsRead {
				t.Errorf("%d produits en base, attendu les %d lignes reçues : un préemballé "+
					"est une ligne, il n'a simplement pas de tuile", grid.Len(), c.rowsRead)
			}
			if got := weighableIn(grid, "other"); got != c.otherTiles {
				t.Errorf("filtre « Autres » : %d tuiles, attendu %d", got, c.otherTiles)
			}

			// A tile without a photo is NORMAL and makes no hole: the two counts add up
			// to the number of tiles, so no product was dropped for want of a picture.
			with, without := 0, 0
			for _, p := range grid.Products() {
				if p.Qualification != domain.Weighable {
					continue
				}
				if p.ImageSHA != "" {
					with++
				} else {
					without++
				}
			}
			if with != c.tilesWithPhoto || without != c.tilesWithoutPhoto {
				t.Errorf("%d tuiles avec photo et %d sans, attendu %d et %d",
					with, without, c.tilesWithPhoto, c.tilesWithoutPhoto)
			}
			if with+without != c.tiles {
				t.Fatalf("%d + %d ≠ %d : une tuile a été perdue faute de photo",
					with, without, c.tiles)
			}
			if got := b.photoFiles(); got != c.photoFiles {
				t.Errorf("%d photos écrites sur le disque, attendu %d", got, c.photoFiles)
			}

			// The inventory the administration screen shows: 355 · 331 · 8 · 16.
			history := b.imports()
			if len(history) != 1 {
				t.Fatalf("%d ligne(s) d'import", len(history))
			}
			row := history[0]
			if row.RowsRead != c.rowsRead || row.Weighable != c.tiles ||
				row.NotWeighable != c.notWeighable || row.Anomalies != c.issues {
				t.Errorf("inventaire %d · %d · %d · %d, attendu %d · %d · %d · %d",
					row.RowsRead, row.Weighable, row.NotWeighable, row.Anomalies,
					c.rowsRead, c.tiles, c.notWeighable, c.issues)
			}
			if row.UnitMismatches != c.units || row.ImagesDecoded != c.photoRows {
				t.Errorf("%d unités divergentes et %d images décodées, attendu %d et %d",
					row.UnitMismatches, row.ImagesDecoded, c.units, c.photoRows)
			}
			if row.ImagesRejected != 0 {
				t.Errorf("%d photo(s) refusée(s) sur un fichier authentique", row.ImagesRejected)
			}
			// The anomalies are NAMED, one line each: a report that says « 16 anomalies »
			// is a filter, one that says which row to fix is a work plan (§10.3 bis).
			findings, err := b.db.Findings(context.Background(), row.ID)
			if err != nil {
				t.Fatalf("Findings : %v", err)
			}
			anomalies := 0
			for _, f := range findings {
				if f.Issue != domain.IssueAnomaly {
					continue
				}
				anomalies++
				if f.CSVLine == 0 || f.ProductID == "" || f.Message == "" {
					t.Fatalf("signalement sans où/quoi/pourquoi : %+v", f)
				}
			}
			if anomalies != c.issues {
				t.Errorf("%d signalements d'anomalie conservés, attendu %d", anomalies, c.issues)
			}

			// Nothing on the CLIENT screen, and no red light.
			if s := b.hub.State(); s.Message != nil {
				t.Fatalf("bandeau client « %s » pour un catalogue ordinaire", s.Message.Text)
			}
			if s := b.hub.State(); s.State != domain.Idle {
				t.Fatalf("état %s après un import nominal", s.State)
			}
			if b.technical.has("ERR-CAT-03") || b.technical.has("ERR-CAT-05") {
				t.Fatal("un feu rouge s'est allumé sur un catalogue ordinaire")
			}

			// The same file again changes nothing at all.
			b.drop(c.file)
			b.awaitFileGone()
			b.awaitImports(2)
			if b.hub.State().Message != nil {
				t.Fatal("le second dépôt a affiché un bandeau client")
			}
			if got := b.hub.Catalog().WeighableCount(); got != c.tiles {
				t.Errorf("%d tuiles après le second dépôt", got)
			}
		})
	}
}

// TestAProductThatLeavesTheFileAgainstTheRealChain is failure test 12 ter on the
// authentic file.
//
// A product absent from the new file is MARKED WITHDRAWN at a date, never deleted. It
// leaves the grid and keeps its weighing history, its local decision and its image —
// « 4 produits retirés » becomes a fact the dashboard can show instead of a silence.
func TestAProductThatLeavesTheFileAgainstTheRealChain(t *testing.T) {
	ctx := context.Background()
	b := newRealBench(t)
	b.drop("flv.csv")
	grid := b.awaitTiles(realTiles)
	b.awaitFileGone()

	// Four tiles about to disappear, and one of them carries a weighing and a decision.
	doomed := make([]domain.Product, 0, 4)
	for _, p := range grid.Products() {
		if p.Qualification == domain.Weighable && len(doomed) < 4 {
			doomed = append(doomed, p)
		}
	}
	ids := map[string]bool{}
	for _, p := range doomed {
		ids[p.ID] = true
	}
	weighing := domain.Weighing{
		OccurredAt: store.TestEpoch, Station: 2, JobID: "01J9F2ABC",
		IdempotencyKey: "01J9F2ABC", ProductID: doomed[0].ID, ProductName: doomed[0].Name,
		Reference: doomed[0].Reference, Mode: doomed[0].Mode,
		GrossWeight: 1236, NetWeight: 1236, Quantity: 1, Barcode: "0493021012365",
		Source: domain.SourceScale, Stability: domain.Stable, Result: domain.ResultSent,
	}
	if err := b.db.RecordWeighing(ctx, &weighing); err != nil {
		t.Fatalf("RecordWeighing : %v", err)
	}
	waiver := domain.Grams(8)
	decision := domain.LocalDecision{
		ProductID: doomed[0].ID, Offered: false, MinWeightG: &waiver,
		Reason: "prix faux chez Odoo", DecidedAt: store.TestEpoch, DecidedBy: "bénévole",
	}
	if err := b.db.SaveDecision(ctx, decision); err != nil {
		t.Fatalf("SaveDecision : %v", err)
	}

	// The next file is short of those four rows.
	b.dropContent(join(withoutProducts(t, rows(t, fixtureBytes(t, "flv.csv")), ids)))
	b.awaitFileGone()
	history := b.awaitImports(2)

	if history[0].ProductsWithdrawn != 4 {
		t.Fatalf("%d produit(s) retirés dans la ligne d'import, attendu 4 : « 4 produits "+
			"retirés » est la phrase que le tableau de bord doit pouvoir écrire",
			history[0].ProductsWithdrawn)
	}
	// The survivors first: a grid that withdrew everything would satisfy « the four
	// that left are gone » without serving anybody.
	if got := b.hub.Catalog().Len(); got != realRows-4 {
		t.Fatalf("%d produits en grille, attendu %d", got, realRows-4)
	}
	for _, p := range doomed {
		if _, offered := b.hub.Catalog().ByID(p.ID); offered {
			t.Fatalf("le produit %s est encore en grille alors qu'il a quitté le fichier", p.ID)
		}
		row, err := b.db.Product(ctx, p.ID)
		if err != nil {
			t.Fatalf("le produit %s a été EFFACÉ : %v", p.ID, err)
		}
		if row.WithdrawnAt.IsZero() {
			t.Fatalf("le produit %s n'a pas de date de retrait", p.ID)
		}
	}
	// Its weighing is still readable, and its decision still stands — both columns.
	journal, err := b.db.Weighings(ctx, store.JournalFilter{Limit: 10})
	if err != nil || len(journal) != 1 || journal[0].ProductID != doomed[0].ID {
		t.Fatalf("%d pesée(s) lisibles : l'historique d'un produit retiré a disparu avec lui : %v",
			len(journal), err)
	}
	kept, err := b.db.Decision(ctx, doomed[0].ID)
	if err != nil {
		t.Fatalf("la décision locale n'a pas survécu au retrait : %v", err)
	}
	if kept.Offered || kept.MinWeightG == nil || *kept.MinWeightG != waiver ||
		kept.Reason != decision.Reason {
		t.Errorf("décision relue %+v", kept)
	}
}

// TestALocalDecisionSurvivesTheNextImport is §10.6, and the sentence of §18 it settles:
// « sans que la table ait à survivre à quoi que ce soit ».
//
// « Ne plus proposer ce produit » and the light-product waiver are two COLUMNS of one
// decision, not two mechanisms, and an import is an upsert that does not touch them.
func TestALocalDecisionSurvivesTheNextImport(t *testing.T) {
	ctx := context.Background()
	b := newRealBench(t)
	b.drop("flv.csv")
	grid := b.awaitTiles(realTiles)
	b.awaitFileGone()

	var chosen domain.Product
	for _, p := range grid.Products() {
		if p.Qualification == domain.Weighable {
			chosen = p
			break
		}
	}
	waiver := domain.Grams(8)
	decision := domain.LocalDecision{
		ProductID: chosen.ID, Offered: false, MinWeightG: &waiver,
		Reason: "code appartenant à un autre article", DecidedAt: store.TestEpoch,
		DecidedBy: "bénévole",
	}
	if err := b.db.SaveDecision(ctx, decision); err != nil {
		t.Fatalf("SaveDecision : %v", err)
	}

	// The next export carries that product again, unchanged. The decision is a
	// JUDGEMENT, and no import overwrites one.
	b.dropContent(join(touchLastName(t, rows(t, fixtureBytes(t, "flv.csv")))))
	b.awaitFileGone()
	b.awaitImports(2)

	grid = b.awaitTiles(realTiles - 1)
	if _, offered := grid.ByID(chosen.ID); offered {
		t.Fatalf("le produit %s (%s) est revenu en grille avec l'import suivant",
			chosen.ID, chosen.Name)
	}
	kept, err := b.db.Decision(ctx, chosen.ID)
	if err != nil {
		t.Fatalf("la décision a disparu avec l'import : %v", err)
	}
	if kept.Offered {
		t.Fatal("l'import a remis le produit en vente")
	}
	if kept.MinWeightG == nil || *kept.MinWeightG != waiver {
		t.Errorf("la dérogation « produit léger » n'a pas survécu : %+v", kept.MinWeightG)
	}
	if kept.Reason != decision.Reason || !kept.DecidedAt.Equal(decision.DecidedAt) ||
		kept.DecidedBy != decision.DecidedBy {
		t.Errorf("décision relue %+v", kept)
	}
	// The product itself is still in the catalog, not withdrawn: it is in the file, a
	// human simply stopped offering it.
	row, err := b.db.Product(ctx, chosen.ID)
	if err != nil || !row.WithdrawnAt.IsZero() {
		t.Errorf("produit %s marqué retiré alors qu'il est dans le fichier : %v", chosen.ID, err)
	}
}
