package catalog

import (
	"fmt"
	"strings"

	"openscale/internal/domain"
)

// This file holds every sentence an import says about a row, and there is exactly one
// per motive.
//
// The structure is imposed by the type and not by the goodwill of whoever writes the
// message (§10.3 bis). Each one answers three questions in this order:
//
//	WHERE   — the Odoo id and the line, carried by the Finding itself, so that
//	          somebody can open the record without searching;
//	WHAT    — the action, in the imperative, with the value that is expected;
//	WHY     — the concrete consequence, in the French of a shop.
//
// A report that says « 16 anomalies » is a filter. One that says what to fix, where
// and why is a work plan, and this is the only measure of the quality of the Odoo
// configuration anybody will ever look at.

// unreadableRow reports a line that is not a product at all.
//
// It is the only motive that feeds the absolute guard: below min_readable_ratio the
// WHOLE batch is refused, because a CSV cut off in mid-flight does not replace a
// healthy catalog (§10.4a).
func unreadableRow(r Row) domain.Finding {
	return domain.Finding{
		CSVLine:   r.Line,
		ProductID: r.ID,
		Code:      domain.FindingUnreadableRow,
		Issue:     domain.IssueAnomaly,
		Value:     r.ID + " / " + r.Name,
		Message: "Corriger la ligne : elle doit porter un identifiant et un nom. " +
			"Sans les deux, ce n'est pas un produit mais du texte, et un fichier " +
			"dont trop de lignes sont dans ce cas est refusé en entier.",
	}
}

// priceUnreadable reports a price that is not a number this application can use.
func priceUnreadable(r Row) domain.Finding {
	return domain.Finding{
		CSVLine:   r.Line,
		ProductID: r.ID,
		Code:      domain.FindingPriceUnreadable,
		Issue:     domain.IssueAnomaly,
		Value:     r.Price,
		Message: fmt.Sprintf("Corriger le prix : « %s » n'est pas un nombre exploitable "+
			"(au plus deux décimales, au plus %s €, ni signe ni espace). "+
			"On ne met pas un prix inventé sur une étiquette : sans prix lisible, "+
			"le produit n'a pas de tuile.", r.Price, domain.MaxUnitPrice.Euro()),
	}
}

// zeroPrice reports a product at 0,00 €.
func zeroPrice(r Row) domain.Finding {
	return domain.Finding{
		CSVLine:   r.Line,
		ProductID: r.ID,
		Code:      domain.FindingZeroPrice,
		Issue:     domain.IssueAnomaly,
		Value:     r.Price,
		Message: "Renseigner le prix : il vaut zéro. Une étiquette à 0,00 € passe en " +
			"caisse sans rien facturer, et le client repart avec la marchandise.",
	}
}

// noBarcode reports an article the till could not read. It is NOT a defect.
func noBarcode(r Row) domain.Finding {
	return domain.Finding{
		CSVLine:   r.Line,
		ProductID: r.ID,
		Code:      domain.FindingNoBarcode,
		Issue:     domain.IssueInfo,
		Message: "Rien à corriger si ce produit ne se pèse pas : sans code-barres, il " +
			"n'est pas référencé en caisse et n'a donc pas de tuile. " +
			"Lui donner un code-barres de pesée dans Odoo suffirait à le proposer.",
	}
}

// invalidBarcode reports thirteen characters that are not a readable EAN-13.
//
// It NAMES the check digit that was expected, because that is the difference between
// « code-barres invalide » and a correction somebody can type: six of the seven
// offending codes of flv_1.csv are one digit away from being right.
func invalidBarcode(r Row) domain.Finding {
	return domain.Finding{
		CSVLine:   r.Line,
		ProductID: r.ID,
		Code:      domain.FindingInvalidBarcode,
		Issue:     domain.IssueAnomaly,
		Value:     r.Barcode,
		Message: fmt.Sprintf("Corriger le code-barres « %s » : %s. "+
			"Un scanner de caisse le refusera.", r.Barcode, whyNotEAN13(r.Barcode)),
	}
}

// whyNotEAN13 says, in French, which of the two faults a code carries.
//
// The two lead to different work: a wrong check digit is a typing mistake to fix at
// the producer, and anything else is not a barcode at all.
func whyNotEAN13(code string) string {
	if len(code) != 13 {
		return fmt.Sprintf("il compte %d caractères au lieu de 13", len([]rune(code)))
	}
	check, err := domain.CheckDigit(code[:12])
	if err != nil {
		return "il n'est pas composé de 13 chiffres"
	}
	return fmt.Sprintf("il se termine par %q alors que la clé de contrôle de %q est %q",
		code[12:], code[:12], string(check))
}

// prepackaged reports a supplier code. Nobody in the shop can act on it, and nothing
// is wrong.
func prepackaged(r Row) domain.Finding {
	return domain.Finding{
		CSVLine:   r.Line,
		ProductID: r.ID,
		Code:      domain.FindingPrepackagedProduct,
		Issue:     domain.IssueInfo,
		Value:     r.Barcode,
		Message: fmt.Sprintf("Rien à corriger : « %s » est un code fournisseur. "+
			"Le produit porte déjà son propre code-barres et n'a aucune raison d'être "+
			"pesé, donc pas de tuile.", r.Barcode),
	}
}

// internalCode reports a code the shop attributed itself and the scale cannot encode.
//
// Reported APART from a prepackaged product on purpose: somebody here chose this
// number, so it is fixable in Odoo (§10.3).
func internalCode(r Row) domain.Finding {
	return domain.Finding{
		CSVLine:   r.Line,
		ProductID: r.ID,
		Code:      domain.FindingInternalCodeNotWeighable,
		Issue:     domain.IssueInfo,
		Value:     r.Barcode,
		Message: fmt.Sprintf("Corriger le code-barres dans Odoo si ce produit doit être "+
			"pesé : « %s » est un code interne du magasin que la balance ne sait pas "+
			"encoder. Les codes pesables commencent par 0493 à 0499.", r.Barcode),
	}
}

// reservedZone reports the critical one: a reference that spills over the field the
// weight is written into.
//
// The message names the exact digits and the value expected, because it is copied
// into Odoo as is. Without this check, 1,236 kg of `0493100100006` would print a
// label the till reads as another article weighing 11,236 kg (§6.2, T32).
func reservedZone(r Row, plan domain.PrefixPlan, reserved string) domain.Finding {
	first := 13 - plan.PayloadWidth
	field := "poids"
	if plan.Mode == domain.ByUnit {
		field = "nombre d'unités"
	}
	return domain.Finding{
		CSVLine:   r.Line,
		ProductID: r.ID,
		Code:      domain.FindingReservedZoneNotEmpty,
		Issue:     domain.IssueAnomaly,
		Value:     r.Barcode,
		Message: fmt.Sprintf("Corriger le code-barres : les chiffres %d à 12 valent « %s » "+
			"au lieu de « %s ». La référence déborde sur le champ %s : à l'impression, "+
			"l'étiquette désignerait un autre article, avec son prix.",
			first, reserved, strings.Repeat("0", plan.PayloadWidth), field),
	}
}

// unitMismatch reports a unit column that fights the barcode by nature.
//
// The product STAYS weighable and the mode comes from the prefix: the barcode is the
// only one of the two the till reads. Only the printed suffix is wrong (§10.2).
func unitMismatch(r Row, plan domain.PrefixPlan) domain.Finding {
	sold := "au poids"
	if plan.Mode == domain.ByUnit {
		sold = "à l'unité"
	}
	return domain.Finding{
		CSVLine:   r.Line,
		ProductID: r.ID,
		Code:      domain.FindingUnitMismatch,
		Issue:     domain.IssueInfo,
		Value:     r.Barcode,
		Message: fmt.Sprintf("Corriger l'unité dans Odoo : elle annonce une grandeur %s "+
			"alors que le code-barres « %s » vend %s. Le code-barres fait foi — c'est la "+
			"seule des deux informations que la caisse lit —, le produit reste proposé et "+
			"seul le libellé du prix est faux.", r.Magnitude, r.Barcode, sold),
	}
}

// unknownUnit reports a wording that is none of the three the exchange format uses.
func unknownUnit(r Row, plan domain.PrefixPlan) domain.Finding {
	return domain.Finding{
		CSVLine:   r.Line,
		ProductID: r.ID,
		Code:      domain.FindingUnknownUnit,
		Issue:     domain.IssueInfo,
		Value:     r.PriceSuffix,
		Message: fmt.Sprintf("Corriger l'unité dans Odoo : elle ne vaut ni « kg », ni "+
			"« Litre(s) », ni « Unité(s) ». Le produit reste proposé, avec le libellé de "+
			"prix par défaut de son préfixe («%s »).", plan.PriceLabel),
	}
}

// UnexpectedHeader reports a first line that is not the one the exchange format
// declares.
//
// It bears on NO product and blocks nothing: the columns are read in the order the
// format fixes, and a producer who renamed a heading has not changed the file (§10.2).
// The legacy application built its header string and never compared it.
func UnexpectedHeader(got, want []string) domain.Finding {
	return domain.Finding{
		CSVLine: 1,
		Code:    domain.FindingUnexpectedHeader,
		Issue:   domain.IssueInfo,
		Value:   strings.Join(got, ";"),
		Message: fmt.Sprintf("Vérifier l'export Odoo : la première ligne annonce « %s » "+
			"au lieu de « %s ». Le fichier est lu quand même, colonne par colonne dans "+
			"l'ordre attendu.", strings.Join(got, ";"), strings.Join(want, ";")),
	}
}

// UnknownCategory reports a letter outside F, L, V and A.
//
// The product is filed under catalog.fallback_category and SHOWN all the same: there
// is no scenario where the grid is empty because of an unexpected category (§10.2 bis).
func UnknownCategory(r Row, letter, fallback string) domain.Finding {
	return domain.Finding{
		CSVLine:   r.Line,
		ProductID: r.ID,
		Code:      domain.FindingUnknownCategory,
		Issue:     domain.IssueInfo,
		Value:     letter,
		Message: fmt.Sprintf("Corriger la catégorie dans Odoo : « %s » n'est ni F, ni L, "+
			"ni V, ni A. Le produit est rangé dans « %s » et reste proposé.",
			letter, fallback),
	}
}

// ImageInvalid reports a photo that is none of the four formats.
//
// Non-blocking, and that is the decision: the product keeps its tile, it loses its
// photo. Half the real catalog has no photo anyway (§10.7).
func ImageInvalid(r Row, why string) domain.Finding {
	return domain.Finding{
		CSVLine:   r.Line,
		ProductID: r.ID,
		Code:      domain.FindingImageInvalid,
		Issue:     domain.IssueInfo,
		Value:     why,
		Message: fmt.Sprintf("Remplacer la photo dans Odoo : %s. Elle n'est ni JPEG, ni "+
			"PNG, ni GIF, ni BMP, donc elle n'est pas enregistrée. Le produit garde sa "+
			"tuile, sans photo.", why),
	}
}

// ImageTooLarge reports a photo past a bound: max_image_size_kb once decoded, or the
// dimensions that close the decompression bomb.
//
// Non-blocking, exactly like ImageInvalid: the product keeps its tile, it loses its
// photo (§10.7b).
func ImageTooLarge(r Row, why string) domain.Finding {
	return domain.Finding{
		CSVLine:   r.Line,
		ProductID: r.ID,
		Code:      domain.FindingImageTooLarge,
		Issue:     domain.IssueInfo,
		Value:     why,
		Message: fmt.Sprintf("Réduire la photo dans Odoo : %s. Elle n'est pas "+
			"enregistrée ; le produit garde sa tuile, sans photo.", why),
	}
}
