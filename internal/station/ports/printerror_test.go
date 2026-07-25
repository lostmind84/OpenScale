// The tests of the printing taxonomy of §8.5.
//
// It lives in ports because the taxonomy does: it is the contract between a printer
// driver and the station that calls it, and it used to exist TWICE — once in
// internal/printing/raster and once in internal/printing/sbpl, with the same six
// kinds, the same three methods and two different zero values. These tests are the
// two suites merged, and they are what keeps the single definition honest.
package ports_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"openscale/internal/station/ports"
)

// TestEveryKindHasItsOwnSpelling: one spelling per value, shared by the journal, the
// database column and the screen (§16.1 naming rule).
func TestEveryKindHasItsOwnSpelling(t *testing.T) {
	seen := map[string]ports.Kind{}
	for _, c := range []struct {
		kind ports.Kind
		want string
	}{
		{ports.KindData, "data"},
		{ports.KindTemplate, "template"},
		{ports.KindTransient, "transient"},
		{ports.KindConsumable, "consumable"},
		{ports.KindConfig, "config"},
		{ports.KindInternal, "internal"},
	} {
		got := c.kind.String()
		if got != c.want {
			t.Errorf("Kind(%d).String() = %q, attendu %q", c.kind, got, c.want)
		}
		if other, twice := seen[got]; twice {
			t.Errorf("les Kind %d et %d s'écrivent tous les deux « %s »", other, c.kind, got)
		}
		seen[got] = c.kind
	}
	if ports.Kind(200).String() != "unknown" {
		t.Error("un Kind hors vocabulaire doit s'écrire « unknown » plutôt que de mentir")
	}
}

// TestTheZeroKindBlamesThisBinary is the one property the two merged copies disagreed
// on, and the reason this one was kept.
//
// §8.5 lists the six kinds in the order of its table, KindData first. That table is a
// presentation of the taxonomy and not a numbering of it, and the number that matters
// is the ZERO: it is what a *PrintError built without naming a Kind carries. Blaming
// the catalog for a driver's omission would flag a healthy product and tell a customer
// « Ce produit n'est pas disponible » about a product that is; announcing our own bug
// is the honest default, and it retries nothing.
func TestTheZeroKindBlamesThisBinary(t *testing.T) {
	var unclassified ports.Kind
	if unclassified != ports.KindInternal {
		t.Fatalf("le Kind zéro s'écrit %q : une erreur que personne n'a classée doit être interne",
			unclassified)
	}
	if (&ports.PrintError{Op: "essai", Message: "message"}).Retryable() {
		t.Error("une erreur non classée est déclarée réessayable : une faute de programmation " +
			"serait tentée trois fois")
	}
}

// TestOnlyATransientFailureIsRetried freezes the policy column of §8.5.
//
// KindConsumable is the one that costs if it moves: the label CAME OUT, the weighing
// succeeded, and the printer only then said media-empty. Retrying it — or reporting it
// as a failure — is what sent a customer away with a valid label and a red screen, so
// they stuck two labels on or weighed again, and the till counted twice (important-9).
func TestOnlyATransientFailureIsRetried(t *testing.T) {
	for _, c := range []struct {
		kind      ports.Kind
		retryable bool
	}{
		{ports.KindData, false},
		{ports.KindTemplate, false},
		{ports.KindTransient, true},
		{ports.KindConsumable, false},
		{ports.KindConfig, false},
		{ports.KindInternal, false},
	} {
		refusal := &ports.PrintError{Kind: c.kind, Op: "essai", Message: "message"}
		if got := refusal.Retryable(); got != c.retryable {
			t.Errorf("Retryable() sur %s = %v, attendu %v", c.kind, got, c.retryable)
		}
	}
}

// TestTheMessageCarriesTheOperationAndTheCause is what a support request is read from:
// an English identifier that situates the fault, and a French sentence a volunteer can
// act on.
func TestTheMessageCarriesTheOperationAndTheCause(t *testing.T) {
	bare := &ports.PrintError{Kind: ports.KindConfig, Op: "sbpl.darkness",
		Message: "noircissement 7 hors bornes SBPL (1..5)"}
	if got := bare.Error(); got != "sbpl.darkness : noircissement 7 hors bornes SBPL (1..5)" {
		t.Errorf("Error() = %q", got)
	}
	if bare.Unwrap() != nil {
		t.Errorf("une erreur de bornes n'enveloppe rien : %v", bare.Unwrap())
	}

	cause := errors.New("le périphérique a refusé l'écriture")
	wrapped := &ports.PrintError{Kind: ports.KindTransient, Op: "sbpl.encode",
		Message: "l'imprimante n'a pas accepté la trame", Err: cause}
	if !errors.Is(wrapped, cause) {
		t.Error("la cause n'est pas atteignable par errors.Is : le journal perdrait l'erreur système")
	}
	if got := wrapped.Error(); !strings.Contains(got, "sbpl.encode") ||
		!strings.Contains(got, cause.Error()) {
		t.Errorf("Error() = %q : il doit porter l'opération, le message et la cause", got)
	}

	// errors.As is how the print service reaches the Kind through whatever a driver
	// wrapped it in, and it is the only reason this type is a pointer receiver.
	var found *ports.PrintError
	if !errors.As(fmt.Errorf("raster.Print : %w", wrapped), &found) {
		t.Fatal("errors.As ne retrouve pas la *PrintError sous un fmt.Errorf")
	}
	if found.Kind != ports.KindTransient {
		t.Errorf("Kind = %s après enveloppement, attendu transient", found.Kind)
	}
}
