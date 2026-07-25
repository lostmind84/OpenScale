package printing

import (
	"errors"
	"strings"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// entry builds a registry entry for a driver that does nothing, which is all the
// registry ever needs to know about one.
func entry(id, label string) Driver {
	return Driver{
		Descriptor: domain.PrinterDescriptor{ID: id, Label: label},
		New: func(DriverConfig) (ports.Printer, error) {
			return nil, errors.New("ce driver de test ne construit rien")
		},
	}
}

// TestTheThreeDriversOfPrinterTypeRegisterAndComeBackInOrder.
//
// The three IDs are the ones domain names for printer.type — one spelling, declared
// once, because Config.Validate uses them long before any driver exists (§8.1).
func TestTheThreeDriversOfPrinterTypeRegisterAndComeBackInOrder(t *testing.T) {
	r := NewRegistry()
	r.Register(entry(domain.PrinterRaster, "Imprimante d'étiquettes (rendu image)"))
	r.Register(entry(domain.PrinterSBPL, "Imprimante d'étiquettes (SBPL direct)"))
	r.Register(entry(domain.PrinterPreview, "Aperçu (PDF ou PNG)"))

	got := r.Descriptors()
	if len(got) != 3 {
		t.Fatalf("%d drivers, attendu 3", len(got))
	}
	for i, want := range []string{"raster", "sbpl", "preview"} {
		if got[i].ID != want {
			t.Errorf("driver %d = %q, attendu %q : l'ordre est celui de drivers.go, "+
				"donc celui que lit un bénévole", i, got[i].ID, want)
		}
		if got[i].Label == "" {
			t.Errorf("driver %q sans libellé", got[i].ID)
		}
	}
	if DefaultDriverID != domain.PrinterRaster {
		t.Errorf("driver par défaut = %q, attendu %q : raster est le chemin de production "+
			"(§8.1, ADR-002)", DefaultDriverID, domain.PrinterRaster)
	}
}

// TestAnEmptyRegistryOffersNothingRatherThanAnEmptyList.
func TestAnEmptyRegistryOffersNothingRatherThanAnEmptyList(t *testing.T) {
	r := NewRegistry()
	if got := r.Descriptors(); got != nil {
		t.Errorf("un registre vide rend %v", got)
	}
	_, err := r.New(domain.PrinterRaster, DriverConfig{})
	if !errors.Is(err, ErrUnknownDriver) {
		t.Fatalf("erreur = %v, attendu ErrUnknownDriver", err)
	}
	if !strings.Contains(err.Error(), "aucun driver d'impression n'est disponible") {
		t.Errorf("message « %s » : sur un binaire sans driver, c'est CELA la faute, et une "+
			"liste vide la cacherait", err)
	}
}

// TestAnUnknownDriverNamesTheOnesThatExist — never a bare « type inconnu » (§11.3).
func TestAnUnknownDriverNamesTheOnesThatExist(t *testing.T) {
	r := NewRegistry()
	r.Register(entry(domain.PrinterRaster, "Rendu image"))
	r.Register(entry(domain.PrinterPreview, "Aperçu"))

	_, err := r.New("zebra", DriverConfig{})
	if !errors.Is(err, ErrUnknownDriver) {
		t.Fatalf("erreur = %v, attendu ErrUnknownDriver", err)
	}
	for _, want := range []string{`"zebra"`, "preview", "raster"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message « %s » : il doit contenir « %s »", err, want)
		}
	}
}

// TestTheFactoryReceivesEverythingAndInventsNothing: a driver is handed its options,
// its transport, its template, the injected clock, the journal and the demonstration
// label — and it opens no device and reads no clock of its own (§5.3, §8.4).
func TestTheFactoryReceivesEverythingAndInventsNothing(t *testing.T) {
	var seen DriverConfig
	r := NewRegistry()
	r.Register(Driver{
		Descriptor: domain.PrinterDescriptor{ID: domain.PrinterRaster, Label: "Rendu image"},
		Options:    []domain.OptionSchema{{Key: "darkness", Kind: domain.OptionInt, Min: 1, Max: 5}},
		New:        func(c DriverConfig) (ports.Printer, error) { seen = c; return nil, nil },
	})

	want := DriverConfig{
		Template: domain.Template{Name: "weighing_identical"},
		Log:      ports.NopTechnicalLog{},
	}
	if _, err := r.New(domain.PrinterRaster, want); err != nil {
		t.Fatalf("New : %v", err)
	}
	if seen.Template.Name != "weighing_identical" || seen.Log == nil {
		t.Errorf("la fabrique a reçu %+v", seen)
	}
}

// TestTheOptionSchemaTravelsToTheValidation: control 7 of Config.Validate checks
// printer.options against the schema its driver declares, and the administration screen
// generates the form from the same one.
func TestTheOptionSchemaTravelsToTheValidation(t *testing.T) {
	schema := []domain.OptionSchema{{Key: "roll_capacity", Kind: domain.OptionInt, Min: 50}}
	r := NewRegistry()
	r.Register(Driver{
		Descriptor: domain.PrinterDescriptor{ID: domain.PrinterRaster, Label: "Rendu image"},
		Options:    schema,
		New:        func(DriverConfig) (ports.Printer, error) { return nil, nil },
	})

	got := r.Descriptors()
	if len(got[0].Options) != 1 || got[0].Options[0].Key != "roll_capacity" {
		t.Fatalf("schéma rendu : %+v", got[0].Options)
	}
	// A registry a caller can reach into has stopped describing the binary.
	got[0].Options[0].Key = "n'importe quoi"
	if again := r.Descriptors(); again[0].Options[0].Key != "roll_capacity" {
		t.Errorf("le schéma du registre a été modifié depuis l'extérieur : %q",
			again[0].Options[0].Key)
	}
}

// TestRegisterPanicsOnACompositionMistake.
//
// A panic and not an error, and that is deliberate: every refusal here is a mistake in
// drivers.go with no operator input anywhere in it, so it is settled before the first
// weighing — « stops the process at startup, never at print time » (§11.3). An error
// would have exactly one caller, and that caller could only panic on it anyway.
func TestRegisterPanicsOnACompositionMistake(t *testing.T) {
	valid := entry(domain.PrinterRaster, "Rendu image")
	for _, c := range []struct {
		name   string
		driver Driver
		says   string
		before []Driver
	}{
		{
			name:   "identifiant vide",
			driver: Driver{Descriptor: domain.PrinterDescriptor{Label: "Sans identifiant"}, New: valid.New},
			says:   "printer.type",
		},
		{
			name:   "libellé vide",
			driver: Driver{Descriptor: domain.PrinterDescriptor{ID: "muet"}, New: valid.New},
			says:   "label",
		},
		{
			name:   "fabrique nulle",
			driver: Driver{Descriptor: domain.PrinterDescriptor{ID: "creux", Label: "Creux"}},
			says:   "factory",
		},
		{
			name:   "doublon",
			driver: valid,
			says:   "registered twice",
			before: []Driver{valid},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := NewRegistry()
			for _, d := range c.before {
				r.Register(d)
			}
			defer func() {
				recovered := recover()
				if recovered == nil {
					t.Fatalf("Register a accepté %q : c'est une faute de composition, "+
						"elle doit arrêter le binaire au démarrage", c.name)
				}
				message, isString := recovered.(string)
				if !isString || !strings.Contains(message, c.says) {
					t.Errorf("panique « %v » : elle doit contenir « %s » et dire au développeur "+
						"quoi corriger", recovered, c.says)
				}
			}()
			r.Register(c.driver)
		})
	}
}

// TestADriverIsOnePackageAndOneLine — the promise of §5.2, checked on the registry
// itself: registering a second driver changes nothing about the first.
func TestADriverIsOnePackageAndOneLine(t *testing.T) {
	r := NewRegistry()
	r.Register(entry(domain.PrinterRaster, "Rendu image"))
	first := r.Descriptors()

	r.Register(entry(domain.PrinterSBPL, "SBPL direct"))
	again := r.Descriptors()
	if again[0].ID != first[0].ID || again[0].Label != first[0].Label {
		t.Errorf("l'entrée %q a changé quand une seconde a été ajoutée : %+v", first[0].ID, again[0])
	}
}
