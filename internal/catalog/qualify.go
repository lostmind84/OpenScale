package catalog

import (
	"strings"

	"openscale/internal/domain"
)

// Magnitude is the NATURE of the quantity a product is sold in, and it is all the
// `unite` column of the exchange file ever decides.
//
// The wordings themselves — "kg", "Litre(s)", "Unité(s)" — are Odoo vocabulary and
// belong to the adapter. What the decision tree needs is coarser and does not move
// with a producer's spelling: is the quantity MEASURED or COUNTED? That is the only
// thing that can contradict a barcode prefix. A litre and a kilogram are the same
// nature — one weighs the bottle, and only the printed suffix differs (§10.2).
type Magnitude uint8

const (
	// MagnitudeUnknown is a wording nobody declared. It raises UNKNOWN_UNIT and falls
	// back on the price label of the prefix: a fallback wording beats a missing
	// product.
	MagnitudeUnknown Magnitude = iota
	// Continuous is weighed: `kg`, and the `Litre(s)` of liquid bulk.
	Continuous
	// Discrete is counted: `Unité(s)`.
	Discrete
)

// String reports the magnitude the way a French finding names it.
func (m Magnitude) String() string {
	switch m {
	case Continuous:
		return "continue"
	case Discrete:
		return "discrète"
	}
	return "inconnue"
}

// Row is one catalog line as an adapter hands it over: text, trimmed, with the two
// pieces of Odoo vocabulary already translated — the category letter into the code of
// a shelf of THIS station, the unit wording into a magnitude and a price suffix.
//
// Nothing else is interpreted, and the price in particular stays TEXT: "is the price
// a usable number?" is one of the six questions of §10.3, and a row that arrived with
// an integer would have had that question answered somewhere nobody can see.
type Row struct {
	// Line is the line of the file, header included, because a report that says which
	// row to fix in Odoo is a work plan and one that says "16 anomalies" is a filter.
	Line int
	ID   string
	Name string
	// Barcode is the raw `code-barre` column, and it MAY be empty: 9 of the 153 rows
	// of flv_1.csv carry none, which is not a defect (§10.3).
	Barcode string
	// Price is the raw `prix` column. domain.ParseCents reads it, never a float.
	Price string
	// CategoryCode is the shelf of this station the adapter filed the row under.
	CategoryCode string
	// Magnitude and PriceSuffix both come from the `unite` column, which drives the
	// DISPLAY and never the sale mode.
	Magnitude   Magnitude
	PriceSuffix string
	// ImageSHA addresses the photo when the row carried a usable one, and is empty on
	// 174 of the 355 real rows — half a catalog, so not a degraded case (§10.7).
	//
	// A reader fills it only when the producer already gives an ADDRESS it can be held
	// to. Everywhere else the reader hands over the bytes in Image and lets Assemble
	// compute the address, which is the only way two sources that carry the same photo
	// end up writing one file.
	ImageSHA string
	// Image is the photo the row carried, in the bytes of the image ITSELF: the reader
	// has already unwrapped whatever the format wrapped them in — base64 for the Odoo
	// export, and raw bytes for a producer that hands them over. Nil is the ORDINARY
	// case and raises nothing.
	//
	// The bytes and not a sha, because who owns the ADDRESS is the question §10.7
	// answers, and the answer is « not the format ». Recognising the header against the
	// four accepted ones, refusing a photo too big or too wide, hashing it, noticing it
	// is the same photo as another product's and handing it to the sink is one rule
	// applied to every source, and it lives in Assemble.
	//
	// A reader is expected to stop unwrapping at the ceiling plus one byte rather than to
	// hand over everything it found: that is what refuses a field claiming three
	// megabytes after 256 kB have been read instead of after three megabytes have been
	// allocated.
	Image []byte
}

// internalPrefixSpace is the leading three digits every code the shop attributed
// itself shares, weighable or not.
//
// It is what tells the two "not weighable" reasons apart, and the distinction is
// operational and not cosmetic: a 0490 code was given by somebody in the shop, so it
// is fixable in Odoo, whereas a 3700147… belongs to a supplier and nobody here can do
// anything about it (§10.3).
const internalPrefixSpace = "049"

// Qualify answers, once per row and once for all, the question of §10.3: can this
// product be weighed?
//
// ok is false for the one row that is not a product at all — UNREADABLE_ROW — which
// is the only motive that feeds the absolute guard (§10.4a). The two causes of that
// verdict a single row cannot show, a record that does not carry seven fields and an
// id an earlier row already used, are facts about the FILE: the adapter detects them
// and never calls this function on such a row.
//
// The order of the questions is the order of the flowchart, and it is load bearing: a
// row whose price cannot be read is an anomaly whatever its barcode says, and a
// prepackaged product is set aside before anybody looks for a reserved zone it does
// not have.
func Qualify(r Row) (domain.Product, []domain.Finding, bool) {
	p := domain.Product{
		ID:           r.ID,
		Name:         r.Name,
		CategoryCode: r.CategoryCode,
		PriceSuffix:  r.PriceSuffix,
		CSVLine:      r.Line,
		ImageSHA:     r.ImageSHA,
	}

	// 1. Is the row readable? An id without a name is not a product, and neither is a
	// name without an id.
	if r.ID == "" || r.Name == "" {
		return p, []domain.Finding{unreadableRow(r)}, false
	}

	// 2. Is the price a usable number? We never put an invented price on a label.
	price, err := domain.ParseCents(r.Price)
	if err != nil {
		return set(p, domain.Anomaly, domain.FindingPriceUnreadable), one(priceUnreadable(r)), true
	}
	if price == 0 {
		return set(p, domain.Anomaly, domain.FindingZeroPrice), one(zeroPrice(r)), true
	}
	p.UnitPrice = price

	// 3. Does the product have a barcode? Without one the till could not read a label,
	// which is a fact about the article and not a fault of the file.
	if r.Barcode == "" {
		return set(p, domain.NotWeighable, domain.FindingNoBarcode), one(noBarcode(r)), true
	}

	// 4. Is it a valid EAN-13? The only case where somebody writes to the producer.
	reference, err := domain.ParseEAN13(r.Barcode)
	if err != nil {
		return set(p, domain.Anomaly, domain.FindingInvalidBarcode), one(invalidBarcode(r)), true
	}

	// 5. Does the prefix belong to the weighing plan?
	plan, err := domain.PlanFor(reference)
	if err != nil {
		if strings.HasPrefix(r.Barcode, internalPrefixSpace) {
			return set(p, domain.NotWeighable, domain.FindingInternalCodeNotWeighable),
				one(internalCode(r)), true
		}
		return set(p, domain.NotWeighable, domain.FindingPrepackagedProduct), one(prepackaged(r)), true
	}

	// 6. Is the reserved zone empty? Without this question the printed label would
	// carry the reference of ANOTHER product, at ANOTHER price (§6.2, T32).
	reserved := string(reference)[12-plan.PayloadWidth : 12]
	if strings.Trim(reserved, "0") != "" {
		return set(p, domain.Anomaly, domain.FindingReservedZoneNotEmpty),
			one(reservedZone(r, plan, reserved)), true
	}

	// Weighable. The mode comes from the PREFIX, the only one of the two pieces of
	// information the till reads (§10.2).
	p.Reference = reference
	p.Mode = plan.Mode
	p.Qualification = domain.Weighable

	switch {
	case r.Magnitude == MagnitudeUnknown:
		// A fallback wording beats a missing product: the label of the plan applies,
		// and the file gets told what to fix.
		p.PriceSuffix = plan.PriceLabel
		return p, one(unknownUnit(r, plan)), true
	case contradicts(plan.Mode, r.Magnitude):
		return p, one(unitMismatch(r, plan)), true
	}
	return p, nil, true
}

// contradicts reports a unit wording that fights the barcode BY NATURE: something
// counted on a code that carries a mass, something measured on a code that carries a
// count.
//
// `Litre(s)` on a by-weight code is NOT one of them, and that is the whole reason
// this takes a magnitude rather than a wording: it is liquid bulk, it gets weighed,
// and only the suffix changes. The legacy application sold those two products by
// unit, in contradiction with their own barcode (§10.2).
func contradicts(mode domain.SaleMode, m Magnitude) bool {
	return (mode == domain.ByWeight && m == Discrete) || (mode == domain.ByUnit && m == Continuous)
}

// set stamps the outcome and its motive on a product.
//
// The motive is stored ON THE PRODUCT and not only in the report, because "why has
// this article no tile?" is asked in front of the grid, months after the import that
// answered it (§12.3).
func set(p domain.Product, q domain.Qualification, reason string) domain.Product {
	p.Qualification = q
	p.Reason = reason
	return p
}

// one wraps a single finding, which is what every branch of the tree produces.
func one(f domain.Finding) []domain.Finding { return []domain.Finding{f} }
