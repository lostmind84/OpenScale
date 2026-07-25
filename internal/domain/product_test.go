package domain

import "testing"

func sampleProducts() []Product {
	return []Product{
		{ID: "20", Name: "LENTILLES VERTES ♥ *", Reference: "0493171000007", Mode: ByWeight,
			PriceSuffix: " €/kg", UnitPrice: 789, CategoryCode: "bulk", Qualification: Weighable, CSVLine: 2},
		{ID: "4412", Name: "AIL VIOLET BIO", Reference: garlicPattern, Mode: ByWeight,
			PriceSuffix: " €/kg", UnitPrice: 532, CategoryCode: "vegetables", Qualification: Weighable},
		// A prepackaged product: valid supplier EAN-13, no tile, and NOT an error.
		{ID: "5000", Name: "BOULGOUR GROS 5 KG", Reference: "3329482011050", Mode: ByWeight,
			UnitPrice: 1200, CategoryCode: "other", Qualification: NotWeighable, Reason: "PREPACKAGED_PRODUCT"},
		// One of the sixteen real broken codes.
		{ID: "5115", Name: "♥AA-TOMME DE SAVOIE -MV", Reference: "0493100100006", Mode: ByWeight,
			UnitPrice: 2990, CategoryCode: "other", Qualification: Anomaly,
			Reason: "RESERVED_ZONE_NOT_EMPTY", CSVLine: 312},
	}
}

func sampleCategories() []Category {
	return []Category{
		{Code: "fruits", Label: "Fruits", Rank: 1, Color: "#C0392B", Visible: true},
		{Code: "vegetables", Label: "Légumes", Rank: 2, Color: "#27AE60", Visible: true},
		{Code: "bulk", Label: "Vrac", Rank: 3, Color: "#B7950B", Visible: true},
		{Code: "other", Label: "Autres", Rank: 4, Color: "#5D6D7E", Visible: true},
	}
}

func TestCatalogLooksUpByProducerKey(t *testing.T) {
	c := NewCatalog(sampleProducts(), sampleCategories())
	if c.Len() != 4 {
		t.Errorf("Len = %d, want 4", c.Len())
	}
	got, ok := c.ByID("4412")
	if !ok {
		t.Fatal("product 4412 not found")
	}
	if got.Name != "AIL VIOLET BIO" {
		t.Errorf("name = %q, want AIL VIOLET BIO", got.Name)
	}
	if _, ok := c.ByID("does-not-exist"); ok {
		t.Error("an unknown id must not be found")
	}
	// The Odoo ids are NOT contiguous and never an index: the real ones run from
	// 20 to 5209 with gaps, and a lookup by position would silently return the
	// wrong product.
	if _, ok := c.ByID("0"); ok {
		t.Error("id 0 must not resolve to the first product")
	}
}

// TestCatalogIsImmutable is what makes publishing it through an atomic.Pointer
// safe, and what stops an import from reordering tiles under a customer's finger
// (§10.8, ADR-016).
func TestCatalogIsImmutable(t *testing.T) {
	products, categories := sampleProducts(), sampleCategories()
	c := NewCatalog(products, categories)

	// 1. Mutating the caller's slices afterwards must not reach the snapshot.
	products[1].UnitPrice = 1
	products[1].Name = "MUTATED"
	categories[1].Label = "MUTATED"
	if got, _ := c.ByID("4412"); got.UnitPrice != 532 || got.Name != "AIL VIOLET BIO" {
		t.Errorf("the snapshot followed a mutation of the caller's slice: %v", got)
	}
	if c.Categories()[1].Label != "Légumes" {
		t.Error("the snapshot followed a mutation of the caller's categories")
	}

	// 2. What the snapshot hands out must not alias what it holds.
	handed := c.Products()
	handed[0].UnitPrice = 1
	if got, _ := c.ByID("20"); got.UnitPrice != 789 {
		t.Error("Products() returned products that alias the snapshot")
	}
	fromID, _ := c.ByID("20")
	fromID.UnitPrice = 1
	if again, _ := c.ByID("20"); again.UnitPrice != 789 {
		t.Error("ByID returned a product that aliases the snapshot")
	}
	handedCategories := c.Categories()
	handedCategories[0].Label = "MUTATED"
	if c.Categories()[0].Label != "Fruits" {
		t.Error("Categories() returned categories that alias the snapshot")
	}
}

// TestWeighableCountIgnoresWhatHasNoTile: the dashboard shows 331 weighable out of
// 355 received, and never "46 products in error", which is false and alarming
// (§10.4, ADR-021).
func TestWeighableCountIgnoresWhatHasNoTile(t *testing.T) {
	c := NewCatalog(sampleProducts(), sampleCategories())
	if got := c.WeighableCount(); got != 2 {
		t.Errorf("WeighableCount = %d, want 2 of 4", got)
	}
	if c.Len() == c.WeighableCount() {
		t.Error("rows read and weighable products must not be the same figure")
	}
}

// TestNilCatalogIsUsable: the station starts before the first import, and a nil
// snapshot must yield an empty grid rather than a crash. The station always
// starts (guiding principle 7).
func TestNilCatalogIsUsable(t *testing.T) {
	var c *Catalog
	if c.Len() != 0 || c.WeighableCount() != 0 {
		t.Error("a nil catalog must be empty")
	}
	if _, ok := c.ByID("4412"); ok {
		t.Error("a nil catalog must find nothing")
	}
	if c.Products() != nil || c.Categories() != nil {
		t.Error("a nil catalog must hand out nothing")
	}
}

func TestSaleModeAndQualificationSpellTheDatabaseValues(t *testing.T) {
	// These strings are CHECK constraints in the DDL: a typo here is a runtime
	// insert failure in L2, not a compile error.
	if got := ByWeight.String(); got != "by_weight" {
		t.Errorf("ByWeight = %q, want by_weight", got)
	}
	if got := ByUnit.String(); got != "by_unit" {
		t.Errorf("ByUnit = %q, want by_unit", got)
	}
	// And not 'P'/'U': those two letters existed only because Access stored them.
	for _, m := range []SaleMode{ByWeight, ByUnit} {
		if s := m.String(); s == "P" || s == "U" {
			t.Errorf("mode %v must not spell itself as a legacy letter", m)
		}
	}
	for want, q := range map[string]Qualification{
		"weighable": Weighable, "not_weighable": NotWeighable, "anomaly": Anomaly,
	} {
		if got := q.String(); got != want {
			t.Errorf("qualification = %q, want %q", got, want)
		}
	}
}

func TestMeasurementNetIsGrossMinusTare(t *testing.T) {
	cases := []struct {
		gross, tare, want Grams
	}{
		{1236, 0, 1236},
		{1236, 236, 1000},
		{300, 295, 5}, // the case the legacy application got wrong: it evaluated
		// every threshold on the net weight and told a perfectly
		// tared scale it needed retaring
		{-282, 0, -282}, // basket missing
		{0, 100, -100},
	}
	for _, c := range cases {
		m := Measurement{Gross: c.gross, Tare: c.tare}
		if got := m.Net(); got != c.want {
			t.Errorf("Measurement{Gross:%d, Tare:%d}.Net() = %d, want %d", c.gross, c.tare, got, c.want)
		}
	}
}

func TestStabilitySpellsTheDatabaseValues(t *testing.T) {
	for want, s := range map[string]Stability{
		"stable": Stable, "unstable": Unstable,
		"unknown": StabilityUnknown, "not_applicable": StabilityNotApplicable,
	} {
		if got := s.String(); got != want {
			t.Errorf("stability = %q, want %q", got, want)
		}
	}
}
