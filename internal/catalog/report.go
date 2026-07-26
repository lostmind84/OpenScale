package catalog

import (
	"fmt"
	"sort"
	"strings"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// Light is what the administration screen shows about the last import.
//
// There are three and they are not degrees of the same thing: green means there is
// nothing to do, amber means somebody has work to do IN ODOO, and red means the
// catalog in service was NOT replaced. Red therefore never comes from a Report — a
// batch that was refused produced an error, not an inventory (§10.4, §10.5).
type Light uint8

const (
	// LightGreen is a catalog that entered service with nothing to report.
	LightGreen Light = iota
	// LightAmber is a catalog that entered service and named work to do in Odoo.
	LightAmber
)

// String reports the light the way the journal and the screen spell it.
func (l Light) String() string {
	if l == LightAmber {
		return "orange"
	}
	return "vert"
}

// odooCorrectable are the motives that raise the amber light.
//
// THE DOCUMENT DOES NOT STATE THIS RULE, and the two figures it does state cannot
// both come from a count or from a ratio: flv.csv is amber with 16 anomalies out of
// 355 (4,5 %) and flv_1.csv is green with 7 out of 153 (4,6 %). No threshold
// separates 4,5 % from 4,6 %. What separates them is the NATURE of the anomaly, and
// §10.3 draws that line itself: INVALID_BARCODE is « le seul cas où l'on écrit au
// producteur » — nobody in the shop can act on it — while a reserved zone that is
// occupied and a price at zero are records somebody opens in Odoo and fixes this
// afternoon.
//
// So the light asks « is there work to do here? » rather than « how many lines are
// wrong? », which is also what makes it safe to look at: a light that goes amber for
// something nobody can act on is a light the team stops looking at (§10.4).
var odooCorrectable = map[string]bool{
	domain.FindingReservedZoneNotEmpty: true,
	domain.FindingZeroPrice:            true,
	domain.FindingPriceUnreadable:      true,
}

// Report is the inventory of one import, and it is the count the administration
// screen displays.
//
// The three outcomes are counted SEPARATELY because they are worded differently on
// screen and call for different things. There is deliberately no "hidden products"
// total: summing a prepackaged article and a wrong check digit produces a number that
// means nothing, and « 46 produits en erreur » is exactly the alarming falsehood
// ADR-021 exists to remove.
type Report struct {
	// RowsRead counts the product lines of the file, header excluded.
	RowsRead int
	// UnreadableRows feeds the absolute guard, and only it (§10.4a).
	UnreadableRows int

	Weighable    int
	NotWeighable int
	Anomalies    int

	// ByWeight and ByUnit split the weighable products the way the prefix does, and
	// never the way the `unite` column does.
	ByWeight int
	ByUnit   int

	// UnitMismatches counts products that stay weighable with a unit to fix. They are
	// INSIDE Weighable and are never added to Anomalies.
	UnitMismatches int

	// ImagesDecoded counts the PRODUCTS that came out with a photo, and ImagesStored
	// the distinct files behind them. The two differ, and the gap is not noise: 181
	// rows of flv.csv carry a photo but only 165 of them are distinct — 16 products
	// share a picture with another. Addressing an image by its sha is what turns that
	// into 165 files written instead of 181 (§10.7).
	ImagesDecoded int
	ImagesStored  int
	// ImagesRejected counts the photos refused for their format or their size. "No
	// image decoded" on a file that carried some is a symptom; a catalog with no
	// photos at all is a normal case, and flv_1.csv is exactly that.
	ImagesRejected int

	// byReason counts each motive, so that the relative guard can name the three
	// majority ones with an example line (§10.4b).
	byReason map[string]int
	// lines keeps one example CSV line per motive.
	lines map[string]int
}

// Summarize counts a batch. It reads the products and the findings and invents
// nothing: every figure below is derivable from the file that produced them.
func Summarize(b *ports.Batch) Report {
	r := Report{
		RowsRead:       b.RowsRead,
		UnreadableRows: b.UnreadableRows,
		ImagesStored:   len(b.Images),
		byReason:       make(map[string]int),
		lines:          make(map[string]int),
	}
	for _, p := range b.Products {
		if p.ImageSHA != "" {
			r.ImagesDecoded++
		}
		switch p.Qualification {
		case domain.Weighable:
			r.Weighable++
			if p.Mode == domain.ByUnit {
				r.ByUnit++
			} else {
				r.ByWeight++
			}
		case domain.NotWeighable:
			r.NotWeighable++
		case domain.Anomaly:
			r.Anomalies++
		}
	}
	for _, f := range b.Findings {
		if f.Code == domain.FindingUnitMismatch {
			r.UnitMismatches++
		}
		if f.Code == domain.FindingImageInvalid || f.Code == domain.FindingImageTooLarge {
			r.ImagesRejected++
		}
		r.byReason[f.Code]++
		if _, seen := r.lines[f.Code]; !seen {
			r.lines[f.Code] = f.CSVLine
		}
	}
	return r
}

// Light reports what the administration screen shows about this import.
func (r Report) Light() Light {
	for code, count := range r.byReason {
		if count > 0 && odooCorrectable[code] {
			return LightAmber
		}
	}
	return LightGreen
}

// Count reports how many findings carried a motive, and Line an example row for it.
func (r Report) Count(code string) int { return r.byReason[code] }

// Line reports the first CSV line that carried a motive, or zero.
func (r Report) Line(code string) int { return r.lines[code] }

// Motives lists the motives of this import, the most frequent first, with the count
// and an example line each.
//
// It is what the relative guard needs when it refuses a batch: « le lot n'est pas
// appliqué » is a wall, « 214 produits préemballés de plus qu'hier, par exemple ligne
// 87 » is a diagnosis (§10.4b).
func (r Report) Motives() []Motive {
	out := make([]Motive, 0, len(r.byReason))
	for code, count := range r.byReason {
		out = append(out, Motive{Code: code, Count: count, CSVLine: r.lines[code]})
	}
	// Count first, then code: two motives that tie must not swap between two runs, or
	// a golden report would differ from itself.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// Motive is one reason, how often it was raised, and a row that shows it.
type Motive struct {
	Code    string
	Count   int
	CSVLine int
}

// Readable reports whether enough lines of the file were products at all.
//
// The comparison is done in per mille and on integers: ratio is the only float a
// configuration carries, and it has no business deciding a count by way of a rounding
// error (§10.4a).
func (r Report) Readable(minRatio float64) bool {
	if r.RowsRead == 0 {
		return false
	}
	readable := r.RowsRead - r.UnreadableRows
	return readable*1000 >= int(minRatio*1000+0.5)*r.RowsRead
}

// String writes the block §10.3 shows on the administration screen, in French.
//
// It is the wording the specification freezes — « 355 produits reçus », then the
// three outcomes, then the divergent units counted INSIDE the weighable ones — and it
// is written here rather than in a template because the sentence is the deliverable:
// « jamais 46 produits en erreur », which is false, alarming, and would light a
// permanent red light on a perfectly normal catalog.
func (r Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d produit%s reçu%s\n", r.RowsRead, plural(r.RowsRead), plural(r.RowsRead))
	fmt.Fprintf(&b, "  %d pesable%s            %d au poids + %d à l'unité\n",
		r.Weighable, plural(r.Weighable), r.ByWeight, r.ByUnit)
	fmt.Fprintf(&b, "  %d non pesable%s        c'est normal\n",
		r.NotWeighable, plural(r.NotWeighable))
	fmt.Fprintf(&b, "  %d anomalie%s           à corriger dans Odoo\n",
		r.Anomalies, plural(r.Anomalies))
	if r.UnitMismatches > 0 {
		fmt.Fprintf(&b, "  + %d unité%s divergente%s  pesable, unité à corriger (comptée dans les %d)\n",
			r.UnitMismatches, plural(r.UnitMismatches), plural(r.UnitMismatches), r.Weighable)
	}
	return b.String()
}

// plural is the French mark of the plural, which starts at two.
//
// It is here rather than inline because this block is read by volunteers, and « 1
// anomalies » is the kind of detail that makes a screen look like a machine talking
// to itself.
func plural(n int) string {
	if n > 1 {
		return "s"
	}
	return ""
}
