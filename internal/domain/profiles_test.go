package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestNeutralProfileValidatesWithoutAFault is the contract of §11.3: the station
// ALWAYS starts.
//
// It used to carry exactly one fault — the missing administration password — and that
// fault was NOT free: serve.go:256 puts a station OUT OF SERVICE the moment its
// configuration carries one. A cooperative's file, complete down to its tariffs and its
// categories but with no password set, therefore refused to weigh at all. A missing
// ADMINISTRATION password is not a reason to stop serving customers; it is a reason to
// say so on the administration screen, which ADR-033 does — the settings pages open, and
// the first protected act offers the recovery code of the installation sheet.
func TestNeutralProfileValidatesWithoutAFault(t *testing.T) {
	profile := NeutralProfile()
	faults := profile.Validate(testRegistries())
	if len(faults) != 0 {
		t.Fatalf("le profil neutre doit passer sans faute ; obtenu :\n%s",
			strings.Join(fieldsOf(faults), "\n"))
	}
}

// TestNeutralProfileCarriesNoSecret — il n'a rien à décider pour une coopérative, et
// une empreinte inventée là serait la même que celle qui a enfermé un poste dehors.
func TestNeutralProfileCarriesNoSecret(t *testing.T) {
	profile := NeutralProfile()
	if profile.Admin.PasswordHash != "" || profile.Admin.RecoveryCodeHash != "" {
		t.Fatalf("le profil neutre porte un secret : %q / %q",
			profile.Admin.PasswordHash, profile.Admin.RecoveryCodeHash)
	}
}

// TestNeutralProfileValidatesWithNoDriverAtAll is the state of lot L2: no scale
// driver, no printer driver, no catalog source exists yet, and the process must
// still be able to start on this profile.
func TestNeutralProfileValidatesWithNoDriverAtAll(t *testing.T) {
	profile := NeutralProfile()
	if faults := profile.Validate(Registries{}); len(faults) != 0 {
		t.Fatalf("le profil neutre doit passer avec un registre vide ; obtenu :\n%s",
			strings.Join(fieldsOf(faults), "\n"))
	}
}

// TestNeutralProfileIsTheStrictMinimum checks each of the five properties §11.5
// names, one assertion per property.
func TestNeutralProfileIsTheStrictMinimum(t *testing.T) {
	profile := NeutralProfile()

	// Mono-tarif: one tier, and the secondary price field disappears through its own
	// condition.
	if got := len(profile.Pricing.Tiers); got != 1 {
		t.Errorf("grille à %d tarifs, le profil neutre est mono-tarif", got)
	}
	// Generic safeguards: the basket check is off, because -282 g is the tare of one
	// cooperative's basket and nothing else.
	if profile.Limits.BasketCheckEnabled {
		t.Error("le contrôle du panier doit être désactivé : son seuil est un relevé de site")
	}
	if profile.Limits.BasketMin != 0 || profile.Limits.BasketMax != 0 {
		t.Errorf("fenêtre du panier = [%d, %d], le profil neutre ne porte aucun seuil relevé chez un client",
			profile.Limits.BasketMin, profile.Limits.BasketMax)
	}
	if profile.Printer.Type != PrinterPreview {
		t.Errorf("printer.type = %q, attendu %q", profile.Printer.Type, PrinterPreview)
	}
	if profile.Scale.Present {
		t.Error("scale.present doit être faux")
	}
	// It names no hardware: a station that declares it has no scale has no protocol
	// to name (§9.3).
	if profile.Scale.Type != "" {
		t.Errorf("scale.type = %q, le profil neutre ne nomme aucun matériel", profile.Scale.Type)
	}
	// And manual entry is then NOMINAL, not a degraded mode.
	if !profile.Scale.ManualEntryAllowed {
		t.Error("sur un poste sans balance, la saisie à la main est nominale")
	}
}

// TestNeutralProfileCarriesNoSiteValue is the property ADR-026 exists for: no URL,
// no price coefficient, no threshold measured at a customer's site.
func TestNeutralProfileCarriesNoSiteValue(t *testing.T) {
	profile := NeutralProfile()

	if profile.Catalog.Type != CatalogSourceLocalDrop {
		t.Errorf("catalog.type = %q : la source neutre est le répertoire que le service possède",
			profile.Catalog.Type)
	}
	// Serialised and searched: an URL hidden in a driver option would be just as
	// wrong as one in a named field.
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("encodage : %v", err)
	}
	document := string(encoded)
	for _, forbidden := range []string{"http://", "https://", "dav.", "COM8", "SATO", "argon2"} {
		if strings.Contains(document, forbidden) {
			t.Errorf("le profil neutre porte %q :\n%s", forbidden, document)
		}
	}
	// The single tier is neutral: no discount established from any cooperative's
	// evidence.
	tier := profile.Pricing.Tiers[0]
	if tier.Discount != 0 {
		t.Errorf("remise = %s %%, attendu aucune", tier.Discount)
	}
	if profile.Station.Coop != "" || profile.Station.Name != "" {
		t.Errorf("station = %+v, le profil neutre ne nomme aucune coopérative", profile.Station)
	}
}

// TestTheNeutralProfileHidesTheByUnitProducts, and says so in so many words.
//
// A tile sold by unit prints a label WITHOUT EVER READING THE SCALE: it is a gesture of
// its own, and a station that fell back to the factory profile must not start offering
// it because nobody wrote the key down. The profile still validates clean, which is the
// property the whole file exists to hold.
func TestTheNeutralProfileHidesTheByUnitProducts(t *testing.T) {
	profile := NeutralProfile()
	if profile.UI.ShowByUnitProducts {
		t.Error("le profil d'usine montre les produits vendus à l'unité : " +
			"un poste en configuration d'usine offrirait un geste que personne n'a décidé")
	}
	if faults := profile.Validate(testRegistries()); len(faults) != 0 {
		t.Fatalf("le profil neutre doit passer sans faute ; obtenu :\n%s",
			strings.Join(fieldsOf(faults), "\n"))
	}
}

// TestNeutralProfileReadsNoClock: nothing in internal/domain reads a wall clock, so
// the profile leaves modified_at to whoever writes it.
func TestNeutralProfileReadsNoClock(t *testing.T) {
	if !NeutralProfile().ModifiedAt.IsZero() {
		t.Fatal("modified_at doit rester vide : l'horloge est injectée, elle n'est pas lue ici")
	}
}

// TestNeutralProfileDiffersFromTheDeliveredFile is the point of ADR-026 read from
// the other end: the values of a cooperative are a FILE, so the two must not be
// interchangeable.
func TestNeutralProfileDiffersFromTheDeliveredFile(t *testing.T) {
	neutral := NeutralProfile()
	delivered := loadDelivered(t)
	if neutral.Fingerprint() == delivered.Fingerprint() {
		t.Fatal("le profil compilé et le fichier livré doivent avoir des empreintes distinctes")
	}
}

// TestTheNeutralProfileFollowsARepository is not a detail of completeness.
//
// This profile is what an OUT-OF-SERVICE station runs, and an out-of-service
// station is exactly the one that may need a newer binary: Hub.UpdateGuard lets
// it update deliberately, as the escape hatch out of a broken version. A neutral
// profile failing control 48 would close the door the guard leaves open.
func TestTheNeutralProfileFollowsARepository(t *testing.T) {
	profile := NeutralProfile()
	if profile.Update.Repository != DefaultUpdateRepository {
		t.Fatalf("dépôt du profil neutre = %q, attendu %q",
			profile.Update.Repository, DefaultUpdateRepository)
	}
}
