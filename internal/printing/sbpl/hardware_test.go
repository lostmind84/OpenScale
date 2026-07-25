//go:build hardware

// The bench run of L5, and the only thing in this package that a machine cannot
// decide.
//
// Everything else here is arithmetic and byte layout, testable on any laptop. Four
// questions are not, because they are about what a FIRMWARE does with bytes it is
// given, and no amount of reasoning settles them:
//
//  1. does the WS408 accept this frame at all, <A> to <Z>, and print;
//  2. is the polarity of <G> the one shipped — InkIsOne — or is invert_bits due;
//  3. does <A1> take its two fields as HEIGHT then width, as §8.3 states;
//  4. are the A to F of the H payload accepted in UPPER case (this package's choice,
//     §8.3 fixes the format letter and says nothing about the alphabet).
//
// A printer answers all four in about five minutes, and the failure signature of
// question 4 is the cruel one: the printer says nothing and prints nothing.
//
// # RUNNING IT
//
//	go test -tags hardware ./internal/printing/sbpl/ -v \
//	    -device '\\.\SATO WS408_1'          # Windows: a share, or a queue in RAW
//	    -device /dev/usb/lp0                # Linux
//
// It writes the golden frame STRAIGHT to the node, on purpose: this is not the
// production path — internal/printing/transport is — and mixing the two here would
// mean a failed bench run could be blamed on either.
package sbpl_test

import (
	"flag"
	"os"
	"testing"

	"openscale/internal/printing/sbpl"
)

var device = flag.String("device", "",
	"nœud d'impression sur lequel écrire la trame — obligatoire avec -tags hardware")

// TestTheRealPrinterAcceptsTheFrame prints two labels and asks a human to compare
// them.
//
// TWO, and the second one is the point: it is the same label under the opposite
// polarity. Whichever of the two comes out readable IS the answer to invert_bits,
// and it is read off the paper rather than deduced — which is the whole method of
// the alignment self-test of §8.6, applied to the one field this package owns.
func TestTheRealPrinterAcceptsTheFrame(t *testing.T) {
	if *device == "" {
		t.Skip("aucun -device : ce test exige la vraie SATO WS408 du banc")
	}
	bitmap := productionBitmap(t)
	setup := mustSetup(t, productionHeightDots, productionWidthDots)

	for _, c := range []struct {
		name   string
		ink    sbpl.InkPolarity
		expect string
	}{
		{"polarité livrée", sbpl.InkIsOne, "texte noir sur étiquette blanche"},
		{"invert_bits", sbpl.InkIsZero, "étiquette noire, texte en réserve"},
	} {
		t.Run(c.name, func(t *testing.T) {
			job := mustJob(t, setup, mustGraphic(t, 0, 0, bitmap, c.ink), 1)
			node, err := os.OpenFile(*device, os.O_RDWR|os.O_SYNC, 0)
			if err != nil {
				t.Fatalf("ouverture de %s : %v", *device, err)
			}
			defer node.Close()
			if err := sbpl.Encode(node, job); err != nil {
				t.Fatalf("Encode vers %s : %v", *device, err)
			}
			t.Logf("étiquette envoyée — attendu : %s", c.expect)
		})
	}

	t.Log("À VÉRIFIER À LA MAIN, sur les deux étiquettes sorties :")
	t.Log("  1. laquelle des deux est lisible : c'est la valeur de invert_bits")
	t.Log("  2. superposée à une étiquette de production sur une table lumineuse, " +
		"la lisible coïncide (§18, L5)")
	t.Log("  3. l'étiquette n'est ni tournée ni tronquée : <A1> prend bien la hauteur " +
		"AVANT la largeur")
	t.Log("  4. si RIEN ne sort et que l'imprimante ne dit rien, le premier suspect est " +
		"la casse des chiffres hexadécimaux — hexDigits, une constante de sbpl.go")
}
