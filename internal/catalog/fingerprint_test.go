package catalog_test

import (
	"testing"

	"openscale/internal/catalog"
	"openscale/internal/domain"
)

// What the fingerprint of a catalog must and must not depend on.
//
// It exists because a source that received OBJECTS has no bytes to hash. The trap it
// avoids is spelled out in catalog/fingerprint.go: hashing a JSON body makes the digest
// depend on the key order a server chose, so the same catalog arrives NEW every night —
// « le même catalogue deux fois » stops being the nominal case of §10.5, every poll
// rewrites the grid under a customer's finger, and the quarantine never sees one content
// refused three times.

// stocked is the catalog these tests fingerprint, in the order a producer happened to
// publish it.
func stocked() []domain.Product {
	return []domain.Product{
		{ID: "4412", Name: "AIL VIOLET SAF", Reference: "0493021000003",
			Mode: domain.ByWeight, UnitPrice: 1290, PriceSuffix: " €/kg",
			CategoryCode: "vegetables", Qualification: domain.Weighable, ImageSHA: "abc"},
		{ID: "4413", Name: "CAROTTE BOTTE", Reference: "0493022000000",
			Mode: domain.ByWeight, UnitPrice: 230, PriceSuffix: " €/kg",
			CategoryCode: "vegetables", Qualification: domain.Weighable},
		{ID: "4414", Name: "POMME GALA", Reference: "",
			Qualification: domain.NotWeighable, Reason: domain.FindingNoBarcode},
	}
}

// TestTheFingerprintDoesNotMoveWithTheORDERTheProducerChose.
//
// A producer that paginates differently on Tuesday publishes the same catalog, and a
// station that believed otherwise would rewrite its whole grid for nothing.
func TestTheFingerprintDoesNotMoveWithTheORDERTheProducerChose(t *testing.T) {
	first := stocked()
	shuffled := []domain.Product{first[2], first[0], first[1]}

	if catalog.Fingerprint(first) != catalog.Fingerprint(shuffled) {
		t.Fatalf("deux ordres du même catalogue ont deux empreintes :\n%s\n%s",
			catalog.Fingerprint(first), catalog.Fingerprint(shuffled))
	}
}

// TestTheFingerprintIsStableFromOneRunToTheNext: sans quoi rien de ce qui précède ne
// veut dire quoi que ce soit.
func TestTheFingerprintIsStableFromOneRunToTheNext(t *testing.T) {
	if catalog.Fingerprint(stocked()) != catalog.Fingerprint(stocked()) {
		t.Fatal("deux calculs sur le même catalogue ne concordent pas")
	}
	if catalog.Fingerprint(stocked()) == "" {
		t.Fatal("l'empreinte est vide : la quarantaine de §10.5 compte par contenu")
	}
}

// TestAnythingACustomerOrATillWouldNoticeChangesTheFingerprint.
//
// The other half, and the one that matters more: an identity that never moves is an
// identity that hides an update. One field per case, changed on one product.
func TestAnythingACustomerOrATillWouldNoticeChangesTheFingerprint(t *testing.T) {
	before := catalog.Fingerprint(stocked())
	for _, c := range []struct {
		what   string
		change func(*domain.Product)
	}{
		{"l'identifiant", func(p *domain.Product) { p.ID = "9999" }},
		{"le nom affiché sur la tuile", func(p *domain.Product) { p.Name = "AIL BLANC" }},
		{"la référence imprimée", func(p *domain.Product) { p.Reference = "0493099000005" }},
		{"le mode de vente", func(p *domain.Product) { p.Mode = domain.ByUnit }},
		{"le prix", func(p *domain.Product) { p.UnitPrice = 1350 }},
		{"le suffixe de prix", func(p *domain.Product) { p.PriceSuffix = " € le litre" }},
		{"le rayon", func(p *domain.Product) { p.CategoryCode = "fruits" }},
		{"la qualification", func(p *domain.Product) { p.Qualification = domain.Anomaly }},
		{"le motif de mise à l'écart", func(p *domain.Product) { p.Reason = domain.FindingZeroPrice }},
		{"la photo", func(p *domain.Product) { p.ImageSHA = "def" }},
	} {
		t.Run(c.what, func(t *testing.T) {
			after := stocked()
			c.change(&after[0])
			if catalog.Fingerprint(after) == before {
				t.Fatalf("%s change et l'empreinte ne bouge pas : la mise à jour serait "+
					"prise pour le catalogue déjà en service", c.what)
			}
		})
	}
}

// TestAProductThatLeavesTheCatalogChangesTheFingerprint: §10.9 fait sortir de la grille
// un produit qui a quitté le fichier, et c'est un changement comme un autre.
func TestAProductThatLeavesTheCatalogChangesTheFingerprint(t *testing.T) {
	whole := stocked()
	if catalog.Fingerprint(whole[:2]) == catalog.Fingerprint(whole) {
		t.Fatal("un produit retiré ne change pas l'empreinte")
	}
}

// TestTwoProductsCannotBeConfusedByTheirJoinedFields.
//
// The separator is a control character no product field can contain, and this is what
// it buys: « AIL, VIOLET »/« SAF » and « AIL »/« VIOLET, SAF » would hash the same on a
// comma. It is the kind of collision nobody notices until two catalogs are declared
// identical and one of them never enters service.
func TestTwoProductsCannotBeConfusedByTheirJoinedFields(t *testing.T) {
	left := []domain.Product{{ID: "1", Name: "AIL, VIOLET", PriceSuffix: "SAF"}}
	right := []domain.Product{{ID: "1", Name: "AIL", PriceSuffix: "VIOLET, SAF"}}

	if catalog.Fingerprint(left) == catalog.Fingerprint(right) {
		t.Fatal("deux découpages différents des mêmes caractères donnent la même empreinte")
	}
}

// TestAnEmptyCatalogStillHasAnEmpreinte: un lot vide est refusé plus haut, mais la
// fonction ne doit pas paniquer pour autant — un refus a besoin d'une clé lui aussi.
func TestAnEmptyCatalogStillHasAnEmpreinte(t *testing.T) {
	if catalog.Fingerprint(nil) == "" {
		t.Fatal("un catalogue vide n'a pas d'empreinte du tout")
	}
	if catalog.Fingerprint(nil) == catalog.Fingerprint(stocked()) {
		t.Fatal("le vide et le plein ont la même empreinte")
	}
}
