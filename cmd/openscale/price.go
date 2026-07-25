package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"openscale/internal/domain"
)

// runPrice is the demonstration command of the pricing rule. It exercises the
// SAME function the printing path calls -- domain.Price -- so that what a
// terminal shows and what a label carries cannot drift apart.
func runPrice(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("price", flag.ContinueOnError)
	fs.SetOutput(out)
	var (
		unitPrice = fs.String("unit-price", "", "prix unitaire du catalogue, en euros : 5,32 ou 5.32")
		weight    = fs.String("weight", "", "poids net en grammes entiers")
		units     = fs.String("units", "", "nombre d'unités, pour un produit vendu à l'unité")
		tiers     = fs.String("tiers", "cagette", "grille de tarifs : cagette (double) ou single (mono)")
		rounding  = fs.String("rounding", "half_up", "arrondi : half_up, truncate ou half_even")
		suffix    = fs.String("suffix", " €/kg", "suffixe de prix du produit")
	)
	fs.Usage = func() {
		fmt.Fprint(out, `Usage : openscale price --unit-price <prix> --weight <grammes>

Calcule les prix d'une pesée par le chemin unique de l'application. L'ordre des
opérations n'est pas négociable : le coefficient s'applique au PRIX UNITAIRE,
puis le montant en découle. Autrement, le prix au kilo imprimé, multiplié par le
poids imprimé, ne redonnerait pas le montant imprimé.

Options :
  --unit-price <prix>   prix unitaire du catalogue, en euros
  --weight <grammes>    poids net, en grammes entiers
  --units <nombre>      nombre d'unités, à la place de --weight
  --tiers <grille>      cagette (adhérent 9/10 + solidaire) ou single
  --rounding <mode>     half_up (défaut), truncate ou half_even
  --suffix <texte>      suffixe de prix : " €/kg", " € le litre", " € l'unité"
`)
	}
	positional, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 0 {
		fs.Usage()
		return fmt.Errorf("argument inattendu %q", positional[0])
	}
	if *unitPrice == "" {
		fs.Usage()
		return errors.New("--unit-price est obligatoire")
	}
	if (*weight == "") == (*units == "") {
		return errors.New("indiquez soit --weight, soit --units")
	}

	base, err := domain.ParseCents(*unitPrice)
	if err != nil {
		return err
	}
	rules, err := rulesNamed(*tiers)
	if err != nil {
		return err
	}
	policy, err := roundingNamed(*rounding)
	if err != nil {
		return err
	}
	rules.AmountRounding, rules.UnitPriceRounding = policy, policy

	product := domain.Product{
		Name: "produit de démonstration", UnitPrice: base,
		PriceSuffix: *suffix, Qualification: domain.Weighable,
	}
	var measurement domain.Measurement
	if *weight != "" {
		grams, err := strconv.ParseInt(*weight, 10, 64)
		if err != nil {
			return fmt.Errorf("poids %q : indiquez des grammes entiers", *weight)
		}
		product.Mode, measurement.Gross = domain.ByWeight, domain.Grams(grams)
	} else {
		count, err := strconv.Atoi(*units)
		if err != nil {
			return fmt.Errorf("quantité %q : indiquez un nombre entier d'unités", *units)
		}
		product.Mode, measurement.Quantity = domain.ByUnit, count
		product.PriceSuffix = " € l'unité"
	}

	label, err := domain.Price(product, measurement, rules)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, formatLabelLine(label, rules))
	return nil
}

// formatLabelLine renders what the label carries, in the order the label carries
// it: the primary unit price, the primary amount, then the secondary amounts.
//
// The suffix comes from the PRODUCT and never from a constant here: the real
// catalog carries " €/kg", " € le litre" and " € l'unité", and a hard-coded
// "€/kg" would contradict the `unite` column of nine real products.
func formatLabelLine(label domain.Label, rules domain.PricingRules) string {
	parts := []string{
		fmt.Sprintf("%s%s%s", abbrevPrefix(label.PrimaryLine.Tier),
			label.PrimaryLine.UnitPrice.Euro(), label.Product.PriceSuffix),
		fmt.Sprintf("%s%s €", abbrevPrefix(label.PrimaryLine.Tier),
			label.PrimaryLine.Amount.Euro()),
	}
	for _, code := range rules.SecondaryCodes {
		line := label.Find(code)
		if line == nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s%s €", abbrevPrefix(line.Tier), line.Amount.Euro()))
	}
	return strings.Join(parts, " · ")
}

// abbrevPrefix is the tier abbreviation followed by a space, or nothing at all in
// mono-tarif -- where Abbrev is empty and there is no second price to tell apart.
func abbrevPrefix(tier domain.PriceTier) string {
	if tier.Abbrev == "" {
		return ""
	}
	return tier.Abbrev + " "
}

func rulesNamed(name string) (domain.PricingRules, error) {
	switch name {
	case "cagette":
		return domain.LaCagetteRules(), nil
	case "single":
		return domain.SingleTierRules(), nil
	}
	return domain.PricingRules{}, fmt.Errorf("grille %q inconnue : cagette ou single", name)
}

func roundingNamed(name string) (domain.RoundingPolicy, error) {
	switch name {
	case "half_up":
		return domain.RoundHalfUp, nil
	case "truncate":
		return domain.RoundTowardZero, nil
	case "half_even":
		return domain.RoundHalfToEven, nil
	}
	return 0, fmt.Errorf("arrondi %q inconnu : half_up, truncate ou half_even", name)
}
