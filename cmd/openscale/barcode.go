package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"

	"openscale/internal/domain"
)

// runBarcode is the demonstration command of the numbering plan: it turns a
// catalog reference plus a measured quantity into the thirteen digits the till
// will read.
//
// It writes to out rather than to os.Stdout so that a test can read what a
// volunteer would see.
func runBarcode(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("barcode", flag.ContinueOnError)
	fs.SetOutput(out)
	var (
		weight = fs.String("weight", "", "poids net en grammes entiers, par exemple 1236")
		units  = fs.String("units", "", "nombre d'unités, pour un produit vendu à l'unité")
		price  = fs.String("price", "", "montant à encoder, pour une étiquette qui porte un prix")
		plain  = fs.Bool("quiet", false, "n'afficher que les treize chiffres")
	)
	fs.Usage = func() {
		fmt.Fprint(out, `Usage : openscale barcode <référence> --weight <grammes>

La référence est le code-barres du catalogue : treize chiffres dont la zone
réservée à la valeur pesée est à zéro. Le préfixe décide de tout le reste —
largeur des champs, mode de vente, décimales — et il n'est pas réglable.

Options :
  --weight <grammes>   poids net, en grammes entiers (préfixes 0493 à 0498)
  --units <nombre>     nombre d'unités (préfixe 0499)
  --price <montant>    montant à encoder, en euros : 6,58 ou 6.58
  --quiet              n'afficher que les treize chiffres
`)
	}
	positional, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		fs.Usage()
		return errors.New("il faut exactement une référence")
	}

	pattern, err := domain.ParseEAN13(positional[0])
	if err != nil {
		return err
	}
	plan, err := domain.PlanFor(pattern)
	if err != nil {
		return err
	}

	payload, kind, err := readPayload(*weight, *units, *price, plan)
	if err != nil {
		return err
	}

	barcode, err := domain.Generate(pattern, payload, plan.PayloadWidth)
	if err != nil {
		return err
	}

	if *plain {
		fmt.Fprintln(out, barcode)
		return nil
	}
	fmt.Fprintln(out, barcode)
	fmt.Fprintf(out, "  référence %s · %s %s · plan %s : %d chiffres de référence, %d de charge utile\n",
		string(pattern)[4:4+plan.RefWidth], kind, payloadLabel(payload, plan, kind),
		plan.Prefix, plan.RefWidth, plan.PayloadWidth)
	return nil
}

// readPayload picks the single quantity flag that applies and returns it in the
// unit the plan expects. Exactly one of the three must be given: guessing between
// a weight and a unit count is how a label ends up on the wrong product.
func readPayload(weight, units, price string, plan domain.PrefixPlan) (int64, string, error) {
	given := 0
	for _, s := range []string{weight, units, price} {
		if s != "" {
			given++
		}
	}
	if given != 1 {
		return 0, "", errors.New("indiquez exactement une valeur : --weight, --units ou --price")
	}

	switch {
	case weight != "":
		if plan.Mode != domain.ByWeight {
			return 0, "", fmt.Errorf("le préfixe %s vend à l'unité : utilisez --units", plan.Prefix)
		}
		grams, err := strconv.ParseInt(weight, 10, 64)
		if err != nil {
			return 0, "", fmt.Errorf("poids %q : indiquez des grammes entiers, par exemple 1236", weight)
		}
		return grams, "poids", nil

	case units != "":
		if plan.Mode != domain.ByUnit {
			return 0, "", fmt.Errorf("le préfixe %s vend au poids : utilisez --weight", plan.Prefix)
		}
		count, err := strconv.ParseInt(units, 10, 64)
		if err != nil {
			return 0, "", fmt.Errorf("quantité %q : indiquez un nombre entier d'unités", units)
		}
		return count, "quantité", nil

	default:
		amount, err := domain.ParseCents(price)
		if err != nil {
			return 0, "", err
		}
		return int64(amount), "montant", nil
	}
}

// payloadLabel renders the encoded value the way the label would show it, so that
// the operator can compare the two at a glance.
func payloadLabel(payload int64, plan domain.PrefixPlan, kind string) string {
	switch kind {
	case "poids":
		return domain.Grams(payload).Kilos() + " kg"
	case "montant":
		return domain.Cents(payload).Euro() + " €"
	default:
		return strconv.FormatInt(payload, 10)
	}
}
