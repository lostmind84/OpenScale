package catalog_test

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"openscale/internal/catalog"
	"openscale/internal/catalog/localdrop"
	"openscale/internal/catalog/webdav"
	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/station/ports"
	"time"
)

// shipped returns the registry a real binary is built with: the two sources of §10.1,
// the local drop first because it is the default.
func shipped() *catalog.Registry {
	registry := catalog.NewRegistry()
	registry.Register(localdrop.Descriptor())
	registry.Register(webdav.Descriptor())
	return registry
}

// TestTheRegistryOffersTheTwoSourcesOfSection10_1.
func TestTheRegistryOffersTheTwoSourcesOfSection10_1(t *testing.T) {
	descriptors := shipped().Descriptors()
	if len(descriptors) != 2 {
		t.Fatalf("%d source(s) déclarée(s), attendu 2", len(descriptors))
	}
	if descriptors[0].ID != domain.CatalogSourceLocalDrop {
		t.Errorf("la première source proposée est %q : le dépôt local est le défaut",
			descriptors[0].ID)
	}
	if catalog.DefaultSourceID != domain.CatalogSourceLocalDrop {
		t.Errorf("la source par défaut est %q", catalog.DefaultSourceID)
	}
	for _, descriptor := range descriptors {
		if descriptor.Label == "" {
			t.Errorf("%s sans libellé : un bénévole doit lire un nom", descriptor.ID)
		}
	}
}

// TestTheDescriptorsAreCopies: a registry a caller can reach into has stopped
// describing the binary.
func TestTheDescriptorsAreCopies(t *testing.T) {
	registry := shipped()
	first := registry.Descriptors()
	first[0].ID = "modifié"
	first[0].Options[0].Key = "modifiée"

	second := registry.Descriptors()
	if second[0].ID != domain.CatalogSourceLocalDrop || second[0].Options[0].Key == "modifiée" {
		t.Error("le registre a été modifié par son appelant")
	}
}

// TestAnUnknownSourceNamesTheOnesThatExist (§11.3).
func TestAnUnknownSourceNamesTheOnesThatExist(t *testing.T) {
	_, err := shipped().New("depot_local", catalog.SourceConfig{})
	if !errors.Is(err, catalog.ErrUnknownSource) {
		t.Fatalf("erreur %v, attendu ErrUnknownSource", err)
	}
	for _, expected := range []string{"local_drop", "webdav"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("le message n'offre pas %q : %v", expected, err)
		}
	}

	empty := catalog.NewRegistry()
	if _, err := empty.New("local_drop", catalog.SourceConfig{}); err == nil ||
		!strings.Contains(err.Error(), "aucune source") {
		t.Errorf("registre vide : %v", err)
	}
}

// TestTheRegistryBuildsTheSourceItNames.
func TestTheRegistryBuildsTheSourceItNames(t *testing.T) {
	source, err := shipped().New(domain.CatalogSourceLocalDrop, catalog.SourceConfig{
		StationNumber: 3, DataDir: t.TempDir(),
		Clock: fake.NewClock(time.Date(2026, 7, 24, 15, 38, 12, 0, time.UTC)),
	})
	if err != nil {
		t.Fatalf("construction : %v", err)
	}
	defer source.Close()
	if source.Name() != domain.CatalogSourceLocalDrop {
		t.Errorf("nom %q", source.Name())
	}
	var _ ports.CatalogSource = source
}

// TestRegisteringRefusesEveryCompositionMistake, and it does so by panicking: none of
// these has any operator input in it, so it is settled before the first weighing.
func TestRegisteringRefusesEveryCompositionMistake(t *testing.T) {
	valid := localdrop.Descriptor()
	for _, c := range []struct {
		what   string
		source catalog.Source
	}{
		{"sans identifiant", catalog.Source{Label: "x", New: valid.New}},
		{"sans libellé", catalog.Source{ID: "x", New: valid.New}},
		{"sans fabrique", catalog.Source{ID: "x", Label: "x"}},
		{"« manual », qui n'est pas une source", catalog.Source{
			ID: domain.CatalogSourceManual, Label: "x", New: valid.New}},
	} {
		t.Run(c.what, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("enregistrement accepté")
				}
			}()
			catalog.NewRegistry().Register(c.source)
		})
	}

	t.Run("deux fois la même source", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Error("enregistrement accepté deux fois")
			}
		}()
		registry := catalog.NewRegistry()
		registry.Register(valid)
		registry.Register(valid)
	})
}

// TestTheWatchedFileNameDerivesFromTheStationNumber (§11.2).
func TestTheWatchedFileNameDerivesFromTheStationNumber(t *testing.T) {
	for number, want := range map[int]string{1: "flv_1.csv", 2: "flv_2.csv", 12: "flv_12.csv"} {
		if got := catalog.FileName(number); got != want {
			t.Errorf("poste %d surveille %q, attendu %q", number, got, want)
		}
	}
}

// TestTheShippedConfigurationValidatesAgainstTheseSchemas is the test that keeps the
// declared options and the file the cooperative runs on in step.
//
// The shipped configuration selects `webdav` with thirteen options; if a schema
// declared a key it does not carry, or refused one it does, control 9 would say so
// here rather than on the station (§11.3).
func TestTheShippedConfigurationValidatesAgainstTheseSchemas(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/config-lacagette.json")
	if err != nil {
		t.Fatalf("lecture de la configuration livrée : %v", err)
	}
	var config domain.Config
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("configuration livrée illisible : %v", err)
	}

	faults := config.Validate(domain.Registries{CatalogSources: shipped().Descriptors()})
	for _, fault := range faults {
		if strings.HasPrefix(fault.Field, "catalog") {
			t.Errorf("%s : %s", fault.Field, fault.Message)
		}
	}

	// And the other way round: a station declaring the local drop with a URL in its
	// options is refused, with the message that sends it to the real authenticated
	// source (§10.1, control 41).
	config.Catalog.Type = domain.CatalogSourceLocalDrop
	faults = config.Validate(domain.Registries{CatalogSources: shipped().Descriptors()})
	named := false
	for _, fault := range faults {
		if fault.Field == "catalog.options.url" && strings.Contains(fault.Message, "webdav") {
			named = true
		}
	}
	if !named {
		t.Errorf("une URL dans un dépôt local n'a pas été renvoyée sur webdav : %+v", faults)
	}
}
