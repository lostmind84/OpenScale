package catalog_test

import (
	"strings"
	"testing"

	"openscale/internal/catalog"
	"openscale/internal/domain"
)

// The six questions of §10.3, one line of the table each, plus every answer that
// leaves a product weighable.
//
// The order of the questions is checked too, and it matters: a row with an unreadable
// price is an anomaly WHATEVER its barcode says, so a case that fails two questions
// at once must come out with the motive of the first one.

// row builds a well-formed row, which each case then breaks in exactly one place.
func row(change func(*catalog.Row)) catalog.Row {
	r := catalog.Row{
		Line: 42, ID: "4412", Name: "AIL VIOLET SAF",
		Barcode: "0493021000003", Price: "5.32",
		CategoryCode: "vegetables", Magnitude: catalog.Continuous, PriceSuffix: " €/kg",
	}
	change(&r)
	return r
}

func TestTheSixQuestionsOfTheQualification(t *testing.T) {
	for _, c := range []struct {
		question string
		row      catalog.Row
		product  bool
		want     domain.Qualification
		reason   string
		mode     domain.SaleMode
	}{{
		question: "1. la ligne est-elle lisible ? — identifiant vide",
		row:      row(func(r *catalog.Row) { r.ID = "" }),
		product:  false,
		reason:   domain.FindingUnreadableRow,
	}, {
		question: "1. la ligne est-elle lisible ? — nom vide",
		row:      row(func(r *catalog.Row) { r.Name = "" }),
		product:  false,
		reason:   domain.FindingUnreadableRow,
	}, {
		question: "2. le prix est-il exploitable ? — texte",
		row:      row(func(r *catalog.Row) { r.Price = "gratuit" }),
		product:  true,
		want:     domain.Anomaly,
		reason:   domain.FindingPriceUnreadable,
	}, {
		question: "2. le prix est-il exploitable ? — au-delà de MaxUnitPrice",
		row:      row(func(r *catalog.Row) { r.Price = "10000.00" }),
		product:  true,
		want:     domain.Anomaly,
		reason:   domain.FindingPriceUnreadable,
	}, {
		question: "2. le prix est-il exploitable ? — zéro",
		row:      row(func(r *catalog.Row) { r.Price = "0.00" }),
		product:  true,
		want:     domain.Anomaly,
		reason:   domain.FindingZeroPrice,
	}, {
		question: "3. le produit a-t-il un code-barres ? — non",
		row:      row(func(r *catalog.Row) { r.Barcode = "" }),
		product:  true,
		want:     domain.NotWeighable,
		reason:   domain.FindingNoBarcode,
	}, {
		question: "4. est-ce un EAN-13 valide ? — clé fausse",
		row:      row(func(r *catalog.Row) { r.Barcode = "9999990005422" }),
		product:  true,
		want:     domain.Anomaly,
		reason:   domain.FindingInvalidBarcode,
	}, {
		question: "4. est-ce un EAN-13 valide ? — douze chiffres",
		row:      row(func(r *catalog.Row) { r.Barcode = "049302100000" }),
		product:  true,
		want:     domain.Anomaly,
		reason:   domain.FindingInvalidBarcode,
	}, {
		question: "5. le préfixe est-il au plan ? — code fournisseur",
		row:      row(func(r *catalog.Row) { r.Barcode = "3700147202196" }),
		product:  true,
		want:     domain.NotWeighable,
		reason:   domain.FindingPrepackagedProduct,
	}, {
		question: "5. le préfixe est-il au plan ? — code interne 0490",
		row:      row(func(r *catalog.Row) { r.Barcode = "0490000402001" }),
		product:  true,
		want:     domain.NotWeighable,
		reason:   domain.FindingInternalCodeNotWeighable,
	}, {
		question: "5. le préfixe est-il au plan ? — prix variable 0491",
		row:      row(func(r *catalog.Row) { r.Barcode = "0491000000006" }),
		product:  true,
		want:     domain.NotWeighable,
		reason:   domain.FindingInternalCodeNotWeighable,
	}, {
		question: "5. le préfixe est-il au plan ? — prix variable 0492",
		row:      row(func(r *catalog.Row) { r.Barcode = "0492000000003" }),
		product:  true,
		want:     domain.NotWeighable,
		reason:   domain.FindingInternalCodeNotWeighable,
	}, {
		question: "6. la zone de réservation est-elle à zéro ? — non",
		row:      row(func(r *catalog.Row) { r.Barcode = "0493100100006" }),
		product:  true,
		want:     domain.Anomaly,
		reason:   domain.FindingReservedZoneNotEmpty,
	}, {
		question: "pesable au poids, 0493",
		row:      row(func(r *catalog.Row) {}),
		product:  true,
		want:     domain.Weighable,
		mode:     domain.ByWeight,
	}, {
		question: "pesable à l'unité, 0499",
		row: row(func(r *catalog.Row) {
			r.Barcode, r.Magnitude, r.PriceSuffix = "0499000007001", catalog.Discrete, " € l'unité"
		}),
		product: true,
		want:    domain.Weighable,
		mode:    domain.ByUnit,
	}} {
		t.Run(c.question, func(t *testing.T) {
			product, findings, ok := catalog.Qualify(c.row)
			if ok != c.product {
				t.Fatalf("produit = %v, attendu %v", ok, c.product)
			}
			if !ok {
				if len(findings) != 1 || findings[0].Code != c.reason {
					t.Fatalf("signalements %v, attendu un seul %s", codes(findings), c.reason)
				}
				return
			}
			if product.Qualification != c.want {
				t.Errorf("qualification %s, attendu %s", product.Qualification, c.want)
			}
			if product.Reason != c.reason {
				t.Errorf("motif %q, attendu %q", product.Reason, c.reason)
			}
			if c.want == domain.Weighable && product.Mode != c.mode {
				t.Errorf("mode de vente %s, attendu %s", product.Mode, c.mode)
			}
			if product.CSVLine != c.row.Line {
				t.Errorf("ligne CSV %d, attendu %d", product.CSVLine, c.row.Line)
			}
		})
	}
}

// TestAValidSupplierEAN13IsNeverAnAnomaly is the test that keeps the specification
// honest (§16.1).
//
// The legacy application counted 46 « produits en erreur » on flv_1.csv, of which 29
// were prepackaged goods carrying an irreproachable supplier code. They are not
// defective; they are not the scale's business (ADR-021).
func TestAValidSupplierEAN13IsNeverAnAnomaly(t *testing.T) {
	for _, barcode := range []string{
		"3329482011050", // BOULGOUR GROS 5 KG, flv_1.csv
		"3760031080095",
		"7061255343345",
		"3700147202196", // GEL DOUCHE HYPOALLERGENIQUE (10Kg), flv.csv
		"3580281238790", // CERNEAUX DE NOIX BIO, flv.csv
	} {
		if _, err := domain.ParseEAN13(barcode); err != nil {
			t.Fatalf("%s doit être un EAN-13 valide pour que ce test veuille dire quelque chose : %v",
				barcode, err)
		}
		product, findings, ok := catalog.Qualify(row(func(r *catalog.Row) { r.Barcode = barcode }))
		if !ok || product.Qualification != domain.NotWeighable {
			t.Errorf("%s : qualification %s, attendu non pesable", barcode, product.Qualification)
		}
		if product.Reason != domain.FindingPrepackagedProduct {
			t.Errorf("%s : motif %q, attendu PREPACKAGED_PRODUCT", barcode, product.Reason)
		}
		if len(findings) != 1 || findings[0].Issue != domain.IssueInfo {
			t.Errorf("%s : un préemballé n'allume rien, gravité %v", barcode, findings)
		}
	}
}

// TestThePrefixDecidesAndTheUnitOnlyLabels covers the three combinations of §10.2:
// the legitimate liquid bulk, the two real contradictions, and the unknown wording.
func TestThePrefixDecidesAndTheUnitOnlyLabels(t *testing.T) {
	for _, c := range []struct {
		what      string
		barcode   string
		magnitude catalog.Magnitude
		suffix    string
		mode      domain.SaleMode
		wantMode  domain.SaleMode
		finding   string
		wantLabel string
	}{{
		what:      "kg sur un code au poids : rien à signaler",
		barcode:   "0493021000003",
		magnitude: catalog.Continuous,
		suffix:    " €/kg",
		wantMode:  domain.ByWeight,
		wantLabel: " €/kg",
	}, {
		what:      "Litre(s) sur un code au poids : du vrac liquide, rien à signaler",
		barcode:   "0493469000009",
		magnitude: catalog.Continuous,
		suffix:    " € le litre",
		wantMode:  domain.ByWeight,
		wantLabel: " € le litre",
	}, {
		what:      "Unité(s) sur un code au poids : UNIT_MISMATCH, le préfixe gagne",
		barcode:   "0493585000006",
		magnitude: catalog.Discrete,
		suffix:    " € l'unité",
		wantMode:  domain.ByWeight,
		finding:   domain.FindingUnitMismatch,
		wantLabel: " € l'unité",
	}, {
		what:      "kg sur un code à l'unité : UNIT_MISMATCH, le préfixe gagne",
		barcode:   "0499000007001",
		magnitude: catalog.Continuous,
		suffix:    " €/kg",
		wantMode:  domain.ByUnit,
		finding:   domain.FindingUnitMismatch,
		wantLabel: " €/kg",
	}, {
		what:      "unité inconnue : libellé de repli du préfixe et UNKNOWN_UNIT",
		barcode:   "0493021000003",
		magnitude: catalog.MagnitudeUnknown,
		suffix:    "",
		wantMode:  domain.ByWeight,
		finding:   domain.FindingUnknownUnit,
		wantLabel: " €/kg",
	}} {
		t.Run(c.what, func(t *testing.T) {
			product, findings, _ := catalog.Qualify(row(func(r *catalog.Row) {
				r.Barcode, r.Magnitude, r.PriceSuffix = c.barcode, c.magnitude, c.suffix
			}))
			if product.Qualification != domain.Weighable {
				t.Fatalf("qualification %s : le produit reste pesable dans les cinq cas",
					product.Qualification)
			}
			if product.Mode != c.wantMode {
				t.Errorf("mode %s, attendu %s : le préfixe fait foi", product.Mode, c.wantMode)
			}
			if product.PriceSuffix != c.wantLabel {
				t.Errorf("libellé %q, attendu %q", product.PriceSuffix, c.wantLabel)
			}
			if c.finding == "" {
				if len(findings) != 0 {
					t.Errorf("signalements %v, attendu aucun", codes(findings))
				}
				return
			}
			if len(findings) != 1 || findings[0].Code != c.finding {
				t.Errorf("signalements %v, attendu %s", codes(findings), c.finding)
			}
		})
	}
}

// TestAnUnreadablePriceWinsOverAnUnreadableBarcode: the order of the questions is
// part of the rule, not an implementation detail.
func TestAnUnreadablePriceWinsOverAnUnreadableBarcode(t *testing.T) {
	product, findings, ok := catalog.Qualify(row(func(r *catalog.Row) {
		r.Price, r.Barcode = "", "pas un code"
	}))
	if !ok || product.Reason != domain.FindingPriceUnreadable {
		t.Fatalf("motif %q, attendu PRICE_UNREADABLE : le prix est demandé avant le code",
			product.Reason)
	}
	if len(findings) != 1 {
		t.Errorf("%d signalements : une ligne ne se fait reprocher qu'une chose à la fois",
			len(findings))
	}
}

// TestAnAnomalyCarriesNoPriceAndNoReference, which is what the schema of §12.3
// requires: unit_price_cents BETWEEN 0 AND 999999, and length(reference) IN (0, 13).
func TestAnAnomalyCarriesNoPriceAndNoReference(t *testing.T) {
	product, _, _ := catalog.Qualify(row(func(r *catalog.Row) { r.Price = "999999999" }))
	if product.UnitPrice != 0 {
		t.Errorf("prix %d retenu sur une anomalie de prix", product.UnitPrice)
	}
	product, _, _ = catalog.Qualify(row(func(r *catalog.Row) { r.Barcode = "9999990005422" }))
	if product.Reference != "" {
		t.Errorf("référence %q retenue sur un code-barres invalide", product.Reference)
	}
}

// TestEveryMessageIsFrenchImperativeAndNamesTheConsequence walks the whole tree and
// checks the shape §10.3 bis imposes.
func TestEveryMessageIsFrenchImperativeAndNamesTheConsequence(t *testing.T) {
	for _, broken := range []func(*catalog.Row){
		func(r *catalog.Row) { r.Name = "" },
		func(r *catalog.Row) { r.Price = "gratuit" },
		func(r *catalog.Row) { r.Price = "0" },
		func(r *catalog.Row) { r.Barcode = "" },
		func(r *catalog.Row) { r.Barcode = "9999990005422" },
		func(r *catalog.Row) { r.Barcode = "3700147202196" },
		func(r *catalog.Row) { r.Barcode = "0490000402001" },
		func(r *catalog.Row) { r.Barcode = "0493100100006" },
		func(r *catalog.Row) { r.Magnitude = catalog.Discrete },
		func(r *catalog.Row) { r.Magnitude = catalog.MagnitudeUnknown },
	} {
		faulty := row(broken)
		_, findings, _ := catalog.Qualify(faulty)
		if len(findings) != 1 {
			t.Fatalf("%d signalements", len(findings))
		}
		f := findings[0]
		switch {
		case f.CSVLine != 42:
			t.Errorf("%s : ligne %d, attendu 42 — le OÙ", f.Code, f.CSVLine)
		case f.ProductID == "" && f.Code != domain.FindingUnexpectedHeader:
			t.Errorf("%s : sans identifiant Odoo — le OÙ", f.Code)
		// The name is part of the WHERE, and it is the row's own: « 4412 » is a number
		// somebody looks up before starting, « AIL VIOLET SAF » is the product they know.
		// Comparing against the row also covers the one case where it is legitimately
		// empty — a line so damaged it carries no name, which is UNREADABLE_ROW itself.
		case f.ProductName != faulty.Name:
			t.Errorf("%s : nom %q, attendu %q — le OÙ nomme aussi le produit",
				f.Code, f.ProductName, faulty.Name)
		case !strings.HasSuffix(f.Message, "."):
			t.Errorf("%s : « %s » n'est pas une phrase", f.Code, f.Message)
		case len(strings.Fields(f.Message)) < 12:
			t.Errorf("%s : « %s » ne dit pas à la fois quoi faire et pourquoi", f.Code, f.Message)
		}
	}
}

// TestTheReservedZoneMessageNamesTheDigitsAndTheValue reproduces the example of
// §10.3 bis, word for word on the figures.
func TestTheReservedZoneMessageNamesTheDigitsAndTheValue(t *testing.T) {
	_, findings, _ := catalog.Qualify(row(func(r *catalog.Row) { r.Barcode = "0493100100006" }))
	message := findings[0].Message
	for _, expected := range []string{"8 à 12", "« 10000 »", "« 00000 »", "champ poids", "autre article"} {
		if !strings.Contains(message, expected) {
			t.Errorf("le message ne contient pas %q : %s", expected, message)
		}
	}

	// The same motive on a by-unit code names the OTHER field and the OTHER digits:
	// the plan of the prefix carries the widths, nothing else does (ADR-028).
	_, findings, _ = catalog.Qualify(row(func(r *catalog.Row) {
		r.Barcode, r.Magnitude = "0499000000101", catalog.Discrete
	}))
	if len(findings) == 0 || findings[0].Code != domain.FindingReservedZoneNotEmpty {
		t.Fatalf("signalements %v, attendu RESERVED_ZONE_NOT_EMPTY", codes(findings))
	}
	if !strings.Contains(findings[0].Message, "11 à 12") ||
		!strings.Contains(findings[0].Message, "nombre d'unités") {
		t.Errorf("le message d'un code à l'unité : %s", findings[0].Message)
	}
}

// TestASpreadsheetNumberIsNamedForWhatItIs covers the one invalid barcode nobody can
// fix in Odoo, because Odoo is not where it broke.
//
// « 3,70015E+12 » is what a spreadsheet leaves of 3700147202196 after opening the
// export and saving it back: thirteen digits read as a number, kept to six significant
// figures. Counting its eleven characters is TRUE and worth nothing — the digits are
// gone, they cannot be typed back from the report, and every other code of the file is
// in the same state. The only usable instruction is to export again.
func TestASpreadsheetNumberIsNamedForWhatItIs(t *testing.T) {
	for _, mangled := range []string{"3,70015E+12", "3.70015E+12", "3,70015e+12", "4,93E+11"} {
		_, findings, _ := catalog.Qualify(row(func(r *catalog.Row) { r.Barcode = mangled }))
		if len(findings) != 1 || findings[0].Code != domain.FindingInvalidBarcode {
			t.Fatalf("%s : signalements %v, attendu INVALID_BARCODE", mangled, codes(findings))
		}
		message := findings[0].Message
		if strings.Contains(message, "caractères au lieu de 13") {
			t.Errorf("%s : le message compte les caractères d'un nombre : %s", mangled, message)
		}
		for _, expected := range []string{mangled, "Réexporter", "tableur"} {
			if !strings.Contains(message, expected) {
				t.Errorf("le message ne contient pas %q : %s", expected, message)
			}
		}
	}

	// A code that really is a short string of digits keeps the count: there, the number
	// of characters IS the fault, and someone retypes it in Odoo.
	_, findings, _ := catalog.Qualify(row(func(r *catalog.Row) { r.Barcode = "049302100000" }))
	if !strings.Contains(findings[0].Message, "12 caractères au lieu de 13") {
		t.Errorf("un code court a perdu son décompte : %s", findings[0].Message)
	}
}

// codes lists the codes of a slice of findings, for a failure message.
func codes(findings []domain.Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Code)
	}
	return out
}
