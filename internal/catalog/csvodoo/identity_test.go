package csvodoo_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"openscale/internal/catalog"
	"openscale/internal/catalog/csvodoo"
	"openscale/internal/domain"
	"openscale/internal/station/ports"
	"openscale/internal/store"
)

// §10.9, end to end: an import is an UPSERT BY the Odoo id, and a product that leaves
// the file is MARKED WITHDRAWN at a date rather than deleted.
//
// The store already owns that write and its own tests exercise it on rows written by
// hand. What is checked here is the half this package answers for: the ids the parser
// produces are the PRODUCER's key — stable from one export to the next, never derived
// from a position in the file. The 355 ids of flv.csv run from 20 to 5209 with gaps;
// they identify, they do not count and they do not index.

// shelves are the four categories of the shipped configuration. Products reference
// them by a foreign key, so they are written with every import.
var shelves = []domain.Category{
	{Code: "fruits", Label: "Fruits", Rank: 1, Visible: true},
	{Code: "vegetables", Label: "Légumes", Rank: 2, Visible: true},
	{Code: "bulk", Label: "Vrac", Rank: 3, Visible: true},
	{Code: "other", Label: "Autres", Rank: 4, Visible: true},
}

// TestTheOdooIdIsTheKeyAndAProductThatLeavesIsWithdrawn is failure test 12 ter, read
// from an authentic export rather than from rows written by hand.
func TestTheOdooIdIsTheKeyAndAProductThatLeavesIsWithdrawn(t *testing.T) {
	db := store.OpenTest(t)
	ctx := context.Background()

	whole := parseFixture(t, flv1)
	outcome, err := db.ReplaceCatalog(ctx, storeBatch(whole))
	if err != nil {
		t.Fatalf("premier import : %v", err)
	}
	if outcome.Inserted != len(whole.Products) || outcome.Updated != 0 || outcome.Withdrawn != 0 {
		t.Fatalf("premier import : %d insérés, %d mis à jour, %d retirés",
			outcome.Inserted, outcome.Updated, outcome.Withdrawn)
	}

	// The very same file again: 153 UPDATES and not one insertion. The legacy
	// application destroyed 153 identities in order to recreate 153 identical ones.
	outcome, err = db.ReplaceCatalog(ctx, storeBatch(parseFixture(t, flv1)))
	if err != nil {
		t.Fatalf("second import : %v", err)
	}
	if outcome.Inserted != 0 || outcome.Updated != len(whole.Products) || outcome.Withdrawn != 0 {
		t.Fatalf("réimport du MÊME fichier : %d insérés, %d mis à jour, %d retirés",
			outcome.Inserted, outcome.Updated, outcome.Withdrawn)
	}

	// Four product lines leave the file. The four products are MARKED, not deleted.
	amputated, gone := withoutFourProducts(t)
	outcome, err = db.ReplaceCatalog(ctx, storeBatch(amputated))
	if err != nil {
		t.Fatalf("troisième import : %v", err)
	}
	if outcome.Withdrawn != 4 {
		t.Fatalf("%d produit(s) retiré(s), attendu 4 : « 4 produits retirés depuis "+
			"l'import du 12/03 » est la phrase que le tableau de bord doit pouvoir écrire",
			outcome.Withdrawn)
	}
	for _, id := range gone {
		row, err := db.Product(ctx, id)
		if err != nil {
			t.Fatalf("le produit %s a été effacé au lieu d'être retiré : %v", id, err)
		}
		if row.WithdrawnAt.IsZero() {
			t.Errorf("le produit %s est absent du fichier et n'est pas marqué retiré", id)
		}
		if row.Product.Name == "" {
			t.Errorf("le produit %s a perdu son libellé en étant retiré", id)
		}
	}

	// And the product that stayed is still there, with its identity untouched.
	kept, err := db.Product(ctx, whole.Products[0].ID)
	if err != nil {
		t.Fatalf("le premier produit du fichier a disparu : %v", err)
	}
	if kept.Product.Name != whole.Products[0].Name || !kept.WithdrawnAt.IsZero() {
		t.Errorf("le produit %s : %q, retiré le %v", kept.Product.ID,
			kept.Product.Name, kept.WithdrawnAt)
	}
}

// storeBatch turns a parsed batch into the single-transaction write of §12.3.
func storeBatch(batch *ports.Batch) store.Batch {
	report := catalog.Summarize(batch)
	return store.Batch{
		Import: domain.Import{
			OccurredAt: store.TestEpoch,
			Source:     domain.CatalogSourceLocalDrop,
			FileName:   batch.FileName,
			SHA256:     batch.ID,
			ByteCount:  batch.Bytes,

			RowsRead:       report.RowsRead,
			UnreadableRows: report.UnreadableRows,
			Weighable:      report.Weighable,
			NotWeighable:   report.NotWeighable,
			Anomalies:      report.Anomalies,
			UnitMismatches: report.UnitMismatches,
			ImagesDecoded:  report.ImagesDecoded,
			ImagesRejected: report.ImagesRejected,

			Result: domain.ImportApplied,
		},
		Categories: shelves,
		Images:     batch.Images,
		Products:   batch.Products,
		Findings:   batch.Findings,
	}
}

// withoutFourProducts re-reads flv_1.csv with four product lines removed, and reports
// the Odoo ids that left.
func withoutFourProducts(t *testing.T) (*ports.Batch, []string) {
	t.Helper()
	source, err := os.ReadFile(fixture(flv1))
	if err != nil {
		t.Fatalf("lecture de la fixture : %v", err)
	}
	lines := strings.Split(string(source), "\r\n")
	// Four consecutive product lines, well inside the file.
	const from = 40
	gone := make([]string, 0, 4)
	for _, line := range lines[from : from+4] {
		id, _, found := strings.Cut(line, ";")
		if !found {
			t.Fatalf("ligne %d inattendue : %q", from, line)
		}
		gone = append(gone, strings.Trim(id, `"`))
	}
	amputated := append(append([]string{}, lines[:from]...), lines[from+4:]...)

	batch, err := csvodoo.Parse(bytes.NewReader([]byte(strings.Join(amputated, "\r\n"))),
		csvodoo.Options{Source: domain.CatalogSourceLocalDrop, FileName: flv1,
			FallbackCategory: "other", Now: readAt})
	if err != nil {
		t.Fatalf("lecture du fichier amputé : %v", err)
	}
	if len(batch.Products) != 149 {
		t.Fatalf("%d produits dans le fichier amputé, attendu 149", len(batch.Products))
	}
	return batch, gone
}
