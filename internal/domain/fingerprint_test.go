// This file holds the eight characters a volunteer compares by eye: what they
// ignore, what they must catch, and the fact that they FOLLOW the export rather
// than deciding for themselves what two stations share.

package domain

import (
	"encoding/json"
	"testing"
	"time"
)

// TestFingerprintIsStableWhateverTheKeyOrder is the property §11.5 rests on: four
// stations compare eight characters, and a reformatted file must not change them.
func TestFingerprintIsStableWhateverTheKeyOrder(t *testing.T) {
	first := loadDelivered(t)

	// Re-serialise and re-read: encoding/json emits the keys in the order of the Go
	// fields, which is not the order of the delivered file.
	reserialised, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("encodage : %v", err)
	}
	var second Config
	if err := json.Unmarshal(reserialised, &second); err != nil {
		t.Fatalf("décodage : %v", err)
	}
	if first.Fingerprint() != second.Fingerprint() {
		t.Fatalf("empreinte %q après réécriture, %q avant : l'ordre des clés ne doit rien changer",
			second.Fingerprint(), first.Fingerprint())
	}
	if got := len(first.Fingerprint()); got != fingerprintLength {
		t.Fatalf("empreinte de %d caractères, attendu %d", got, fingerprintLength)
	}
}

// TestFingerprintIgnoresWhatDiffersFromStationToStation is what makes a homogeneous
// fleet show ONE string: four stations differ by their number, their name, their COM
// port and their print queue, and each file was written at a different instant.
func TestFingerprintIgnoresWhatDiffersFromStationToStation(t *testing.T) {
	station2 := loadDelivered(t)
	station3 := loadDelivered(t)
	station3.Station.Number = 3
	station3.Station.Name = "Poste 3 — légumes"
	station3.ModifiedAt = station2.ModifiedAt.Add(48 * time.Hour)
	setOption(t, station3.Scale.Options, "port", "COM3")
	setOption(t, station3.Printer.Options, "queue", "SATO WS408_3")
	station3.Network.Listen = "127.0.0.1:8086"
	station3.Admin.RecoveryCodeHash = ""

	if station2.Fingerprint() != station3.Fingerprint() {
		t.Fatalf("empreintes %q et %q : deux postes du même parc doivent afficher la même chaîne",
			station2.Fingerprint(), station3.Fingerprint())
	}
}

// TestFingerprintChangesWhenASharedValueChanges is the other half: a station that
// diverges on something that MUST be identical has to show it.
func TestFingerprintChangesWhenASharedValueChanges(t *testing.T) {
	reference := loadDelivered(t)
	for name, mutate := range map[string]func(*Config){
		"une remise de tarif":     func(c *Config) { c.Pricing.Tiers[0].Discount = 200 },
		"un seuil de panier":      func(c *Config) { c.Limits.BasketMin = -300 },
		"le gabarit":              func(c *Config) { c.Printer.Template = "weighing_neutral_single" },
		"une catégorie":           func(c *Config) { c.Catalog.Categories[0].Visible = false },
		// The WORDING and not only the visibility: internal/web keys its serialized
		// catalog on this digest, and the labels of the grid are read from the
		// configuration (§10.2 bis). A renamed shelf that left the eight characters
		// alone would be served from the cache with its old name until the next import.
		"le libellé d'une catégorie": func(c *Config) { c.Catalog.Categories[0].Label = "Fruits d'ici" },
		"la rétention du journal": func(c *Config) { c.Journal.MaxDays = 30 },
		// Two stations that disagree here do not show the same grid: one offers fifteen
		// tiles the other does not have, and the eight characters have to say so.
		"les produits à l'unité montrés": func(c *Config) { c.UI.ShowByUnitProducts = true },
		// Same reason, read from the other side: one station shows seven columns where
		// its neighbour follows the screen. Neither is wrong, and a fleet that diverges
		// by accident must be able to see it by eye.
		"le nombre de colonnes de la grille": func(c *Config) { c.UI.GridColumns = 7 },
		// internal/web caches the served catalog bytes on this digest (catalogBytes):
		// a raised threshold that left the eight characters alone would keep serving
		// a category bar built for the old one until an unrelated change flushed it.
		"le seuil de puce des catégories": func(c *Config) { c.UI.MinProductsForChip = 7 },
	} {
		t.Run(name, func(t *testing.T) {
			diverging := loadDelivered(t)
			mutate(&diverging)
			if diverging.Fingerprint() == reference.Fingerprint() {
				t.Fatalf("empreinte inchangée (%q) alors que %s a changé", reference.Fingerprint(), name)
			}
		})
	}
}

// TestBlockFingerprintIsWhatReloadCompares checks that a block reserialised with
// another key order does not cut the serial port in the middle of a service.
func TestBlockFingerprintIsWhatReloadCompares(t *testing.T) {
	config := loadDelivered(t)
	before := BlockFingerprint(config.Scale)

	reordered := config.Scale
	reordered.Options = DriverOptions{}
	for key, value := range config.Scale.Options {
		reordered.Options[key] = json.RawMessage("  " + string(value) + " ")
	}
	if after := BlockFingerprint(reordered); after != before {
		t.Fatalf("empreinte de bloc %q puis %q : une réécriture ne doit pas fermer le port série", before, after)
	}

	setOption(t, reordered.Options, "port", "COM3")
	if after := BlockFingerprint(reordered); after == before {
		t.Fatal("changer le port doit changer l'empreinte du bloc balance")
	}
}

// TestTheFollowedRepositoryEntersTheFingerprint: the four stations of one
// cooperative must follow the same repository, and a divergence has to be visible
// on the eight characters the dashboard shows and a volunteer compares by eye.
func TestTheFollowedRepositoryEntersTheFingerprint(t *testing.T) {
	reference := loadDelivered(t)
	diverged := loadDelivered(t)
	diverged.Update.Repository = "someone-else/OpenScale"

	if reference.Fingerprint() == diverged.Fingerprint() {
		t.Fatal("deux postes suivant deux dépôts différents portent la même empreinte")
	}
}
