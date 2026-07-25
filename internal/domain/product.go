package domain

import "fmt"

// SaleMode says whether a product is sold by weight or by unit.
//
// It is DERIVED from the barcode prefix, never from the `unite` column of the CSV
// (§10.2): the prefix is the only one of the two the till reads. Two real
// products -- SHAMPOING CHEVEUX NORMAUX and SAVON LIQUIDE LAVANDE 20KG -- are
// declared "Litre(s)" and carry a by-weight code: they are liquid bulk, they get
// weighed, and only the printed suffix changes. The legacy application sold them
// by unit, contradicting their own barcode.
type SaleMode uint8

const (
	// ByWeight is prefixes 0493 to 0498: the payload carries a mass in grams.
	ByWeight SaleMode = iota
	// ByUnit is prefix 0499: the payload carries a count of units.
	ByUnit
)

// String reports the value stored in products.mode, so that a log line and a
// database row spell the mode the same way.
func (m SaleMode) String() string {
	switch m {
	case ByWeight:
		return "by_weight"
	case ByUnit:
		return "by_unit"
	}
	return "unknown"
}

// Qualification answers, once per imported row: can this product be weighed?
//
// Three answers, not two. The legacy application asked "is this product valid?"
// and hid whatever failed, which mislabelled 30 % of one real catalog -- mostly
// perfectly valid supplier EAN-13 codes on prepackaged goods (ADR-021).
type Qualification uint8

const (
	// Weighable enters the grid.
	Weighable Qualification = iota
	// NotWeighable does not, and that is normal.
	NotWeighable
	// Anomaly does not, and someone must look into it.
	Anomaly
)

// String reports the value stored in products.qualification.
func (q Qualification) String() string {
	switch q {
	case Weighable:
		return "weighable"
	case NotWeighable:
		return "not_weighable"
	case Anomaly:
		return "anomaly"
	}
	return "unknown"
}

// Product is one row of the catalog, as the domain sees it.
//
// ID is the Odoo id and it is the PRODUCER's key: unique and stable across
// imports, which is why an import is an upsert and not a wholesale replacement
// (§10.9). It is never an index -- the real ids run from 20 to 5209 with gaps.
type Product struct {
	ID            string
	Name          string
	Reference     EAN13 // may be empty: 9 of the 153 rows of flv_1.csv have no barcode
	Mode          SaleMode
	PriceSuffix   string // " €/kg" | " € le litre" | " € l'unité" -- a display, never a rule
	UnitPrice     Cents
	CategoryCode  string
	Qualification Qualification
	Reason        string // NO_BARCODE, PREPACKAGED_PRODUCT, RESERVED_ZONE_NOT_EMPTY, ...
	CSVLine       int    // to name the row to fix in Odoo
	ImageSHA      string // empty on 174 of the 355 real products, which is not a defect
}

// Category is a shelf of the grid, as configured for this station.
//
// The letter comes from the producer (F/L/V/A); the label, the rank, the colour
// and "show this category on this station" are real shop decisions (§10.2 bis).
type Category struct {
	Code    string
	Label   string
	Rank    int
	Color   string
	Visible bool
}

// Catalog is an IMMUTABLE snapshot of the products and categories of a station.
//
// Immutable is what allows the Hub to publish it through an atomic.Pointer and
// the weighing path to touch no disk at all: a locked database, a full disk or an
// import in flight never stop a label from coming out (ADR-013). Nothing mutable
// is ever published -- a snapshot is built, then frozen, and only the pointer
// travels.
type Catalog struct {
	products   []Product
	categories []Category
	byID       map[string]int
}

// NewCatalog freezes a snapshot from the rows an import produced.
//
// It copies its inputs: a caller that keeps writing to its own slice afterwards
// must not be able to reorder tiles under a customer's finger (§10.8).
func NewCatalog(products []Product, categories []Category) *Catalog {
	c := &Catalog{
		products:   make([]Product, len(products)),
		categories: make([]Category, len(categories)),
		byID:       make(map[string]int, len(products)),
	}
	copy(c.products, products)
	copy(c.categories, categories)
	for i, p := range c.products {
		c.byID[p.ID] = i
	}
	return c
}

// Len reports how many products the snapshot holds, weighable or not.
func (c *Catalog) Len() int {
	if c == nil {
		return 0
	}
	return len(c.products)
}

// ByID returns a COPY of the product and whether it was found.
//
// A copy and not a pointer: a caller must not be able to reach into a published
// snapshot and change a price under the Hub.
func (c *Catalog) ByID(id string) (Product, bool) {
	if c == nil {
		return Product{}, false
	}
	i, ok := c.byID[id]
	if !ok {
		return Product{}, false
	}
	return c.products[i], true
}

// Products returns a copy of every product of the snapshot, in import order.
func (c *Catalog) Products() []Product {
	if c == nil {
		return nil
	}
	out := make([]Product, len(c.products))
	copy(out, c.products)
	return out
}

// Categories returns a copy of the configured categories.
func (c *Catalog) Categories() []Category {
	if c == nil {
		return nil
	}
	out := make([]Category, len(c.categories))
	copy(out, c.categories)
	return out
}

// WeighableCount reports how many products get a tile. It is the figure the
// admin dashboard shows, and never the number of rows read: that one counts
// prepackaged goods, which have no tile (§14.4).
func (c *Catalog) WeighableCount() int {
	if c == nil {
		return 0
	}
	n := 0
	for _, p := range c.products {
		if p.Qualification == Weighable {
			n++
		}
	}
	return n
}

// String makes a Product readable in a test failure without dumping base64.
func (p Product) String() string {
	return fmt.Sprintf("Product{ID:%s, Name:%q, Reference:%s, Mode:%s, UnitPrice:%d}",
		p.ID, p.Name, p.Reference, p.Mode, p.UnitPrice)
}
