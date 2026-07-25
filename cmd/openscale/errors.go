package main

import (
	"errors"
	"fmt"

	"openscale/internal/domain"
)

// frenchMessage turns a sentinel error of the domain into the sentence an operator
// reads.
//
// The split is deliberate and it is the convention of the whole project: the
// domain names its conditions in ENGLISH, because they are identifiers; the
// presentation layer says what it means in FRENCH, because a volunteer reads it.
// Putting the French inside domain would make the core depend on an audience.
//
// This is also the first draft of what §10.3 bis asks of the import report --
// where / what / why -- with the difference that the report also carries the Odoo
// id and the CSV line number, which a command line does not have.
func frenchMessage(err error) string {
	switch {
	case errors.Is(err, domain.ErrEAN13Format):
		return "ce n'est pas un code-barres : il faut treize chiffres, sans lettre ni espace."

	case errors.Is(err, domain.ErrEAN13CheckDigit):
		return "la clé de contrôle du code-barres est fausse. C'est une erreur de saisie, " +
			"à corriger dans Odoo — la caisse refuserait ce code."

	case errors.Is(err, domain.ErrPrefixNotInPlan):
		return "ce préfixe n'est pas un code de pesée. Les préfixes 0493 à 0498 pèsent, " +
			"0499 vend à l'unité ; tout autre code appartient à un produit préemballé, " +
			"qui porte déjà son propre code-barres et n'a pas à passer sur la balance."

	case errors.Is(err, domain.ErrPatternNotZeroed):
		return "la référence déborde sur le champ réservé au poids. À l'impression, " +
			"l'étiquette désignerait un AUTRE article, avec son prix. " +
			"À corriger dans Odoo : les chiffres réservés doivent être à zéro."

	case errors.Is(err, domain.ErrWidthNotInPlan):
		return "la largeur du champ ne vient pas du plan de numérotation. " +
			"Elle est une propriété du préfixe, jamais un réglage : c'est ce qui garantit " +
			"que la caisse lit l'étiquette comme nous l'avons écrite."

	case errors.Is(err, domain.ErrPayloadOutOfRange):
		return "la valeur ne tient pas dans le champ du code-barres."

	case errors.Is(err, domain.ErrZeroQuantity):
		return "une étiquette ne porte jamais une quantité nulle."

	case errors.Is(err, domain.ErrPrefixModeMismatch):
		return "le mode de vente contredit le code-barres. Le préfixe fait foi : " +
			"c'est la seule des deux informations que la caisse lit."

	case errors.Is(err, domain.ErrPriceFormat):
		return "ce n'est pas un prix exploitable : indiquez des euros avec au plus " +
			"deux décimales, par exemple 5,32 ou 5.32."

	case errors.Is(err, domain.ErrInconsistentTiers):
		return "la grille de tarifs est incohérente."
	}
	return ""
}

// explain prints the French sentence when there is one, followed by the technical
// detail. Both, and in that order: the volunteer needs the first, whoever answers
// the telephone needs the second.
func explain(err error) string {
	if message := frenchMessage(err); message != "" {
		return fmt.Sprintf("%s\n  détail technique : %v", message, err)
	}
	return err.Error()
}
