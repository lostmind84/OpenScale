package localdrop

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"openscale/internal/catalog"
	"openscale/internal/domain"
	"openscale/internal/fake"
)

// What a configuration BUILDS, and what it refuses to build: the directory this source
// owns and creates itself, the one it is pointed at and must not own, the descriptor
// the administration screen generates its form from, and the factory the registry
// reaches it through.

// TestTheDescriptorMatchesTheShippedConfiguration: the schema this source declares is
// what Config.Validate checks catalog.options against (control 9).
func TestTheDescriptorMatchesTheShippedConfiguration(t *testing.T) {
	descriptor := Descriptor()
	if descriptor.ID != domain.CatalogSourceLocalDrop || descriptor.Label == "" || descriptor.New == nil {
		t.Fatalf("descripteur incomplet : %+v", descriptor)
	}
	for _, forbidden := range []string{"url", "username", "password"} {
		for _, option := range descriptor.Options {
			if option.Key == forbidden {
				t.Errorf("le dépôt local déclare %q : un répertoire qu'on possède n'a "+
					"aucun secret à porter (§10.1)", forbidden)
			}
		}
	}
}

// TestASourceRefusesToBeBuiltWithoutWhatItNeeds.
//
// Both refusals are composition mistakes with no operator input in them, and both are
// worth a sentence rather than a nil pointer three seconds later.
func TestASourceRefusesToBeBuiltWithoutWhatItNeeds(t *testing.T) {
	for _, c := range []struct {
		what   string
		config catalog.SourceConfig
		says   string
	}{
		{"sans horloge", catalog.SourceConfig{DataDir: t.TempDir()}, "horloge"},
		{"sans répertoire de données",
			catalog.SourceConfig{Clock: fake.NewClock(t0)}, "répertoire de données"},
	} {
		if _, err := New(c.config); err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s : %v", c.what, err)
		}
	}
}

// TestADropDirectoryThatCannotBeCreatedIsNamed: an installation whose data directory
// is a file is a mistake somebody has to be told about, in the terms of the path.
func TestADropDirectoryThatCannotBeCreatedIsNamed(t *testing.T) {
	root := t.TempDir()
	inTheWay := filepath.Join(root, "catalog")
	if err := os.WriteFile(inTheWay, []byte("un fichier là où un répertoire est attendu"), 0o644); err != nil {
		t.Fatalf("préparation : %v", err)
	}
	_, err := New(catalog.SourceConfig{
		StationNumber: 2, DataDir: root, Clock: fake.NewClock(t0),
	})
	if err == nil {
		t.Fatal("la source a été construite sur un répertoire impossible")
	}
	if !strings.Contains(err.Error(), inTheWay) {
		t.Errorf("le message ne nomme pas le chemin fautif : %v", err)
	}
}

// TestAnUnreadableDropDirectoryIsSaidAndTheWatchGoesOn: a share that blinks is not a
// reason to stop watching (§10.1).
func TestAnUnreadableDropDirectoryIsSaidAndTheWatchGoesOn(t *testing.T) {
	source, _ := station(t, "")
	journal := &recorder{}
	source.log = journal
	// A DIRECTORY where the file is expected: os.Stat succeeds, os.Open succeeds and
	// the read fails — the same shape as a share that answers half way.
	if err := os.Mkdir(source.Path(), 0o755); err != nil {
		t.Fatalf("préparation : %v", err)
	}
	source.poll(context.Background())
	batch, err := source.poll(context.Background())
	if batch != nil {
		t.Fatalf("un répertoire a été lu comme un catalogue : %v", batch)
	}
	if err == nil {
		t.Fatal("la lecture d'un répertoire n'a rien signalé")
	}
	// It was set aside like any unusable content, with its reason, and the watch is
	// usable afterwards: the real file lands later and is read.
	if names := archives(t, source); len(names) == 0 {
		t.Error("rien n'a été mis de côté")
	}
	drop(t, source, aCatalog)
	source.poll(context.Background())
	if batch, err := source.poll(context.Background()); batch == nil {
		t.Fatalf("la scrutation ne s'est pas remise : %v", err)
	}
}

// TestTheTechnicalLogIsNeverNil: no driver checks for one (ADR-013).
func TestTheTechnicalLogIsNeverNil(t *testing.T) {
	source, _ := station(t, "")
	if source.log == nil {
		t.Fatal("la source a gardé un journal nil")
	}
	source.log.Technical("info", "catalog", "", "message", "détail")
}

// TestTheFactoryOfTheRegistryBuildsAWatchingSource: the descriptor is what
// cmd/openscale/drivers.go registers, so the one line of §5.2 is exercised here.
func TestTheFactoryOfTheRegistryBuildsAWatchingSource(t *testing.T) {
	journal := &recorder{}
	built, err := Descriptor().New(catalog.SourceConfig{
		Catalog:       domain.CatalogConfig{FallbackCategory: "other"},
		StationNumber: 4,
		DataDir:       t.TempDir(),
		Clock:         fake.NewClock(t0),
		Log:           journal,
	})
	if err != nil {
		t.Fatalf("construction par la fabrique : %v", err)
	}
	defer built.Close()

	source, ok := built.(*Source)
	if !ok {
		t.Fatalf("la fabrique a rendu un %T", built)
	}
	if filepath.Base(source.Path()) != "flv_4.csv" {
		t.Errorf("le poste 4 surveille %q", filepath.Base(source.Path()))
	}
	if source.log != journal {
		t.Error("le journal technique injecté n'a pas été retenu")
	}
}

// TestAnEmptyDirectoryOptionKeepsTheStationDirectory is the shipped case: nothing in
// catalog.options, and the source watches the directory the service owns and creates.
func TestAnEmptyDirectoryOptionKeepsTheStationDirectory(t *testing.T) {
	data := t.TempDir()
	got, owned := Directory(catalog.SourceConfig{DataDir: data})
	want := filepath.Join(data, "catalog", "incoming")
	if got != want {
		t.Errorf("répertoire = %q, attendu %q", got, want)
	}
	if !owned {
		t.Error("le répertoire par défaut appartient au service : il le crée lui-même")
	}
}

// TestANamedDirectoryIsWatchedAndNotOwned: somebody named a directory, so the service
// watches it and does NOT create it. A typo would otherwise build a tree nobody watches.
func TestANamedDirectoryIsWatchedAndNotOwned(t *testing.T) {
	chosen := t.TempDir()
	c := catalog.SourceConfig{
		DataDir: t.TempDir(),
		Catalog: domain.CatalogConfig{Options: driverOptions(t,
			`{"directory":`+strconv.Quote(chosen)+`}`)},
	}
	got, owned := Directory(c)
	if got != filepath.Clean(chosen) {
		t.Errorf("répertoire = %q, attendu %q", got, filepath.Clean(chosen))
	}
	if owned {
		t.Error("un répertoire nommé par un humain n'appartient pas au service")
	}
}

// TestABlankDirectoryOptionIsNoDirectoryAtAll: a field somebody opened and left with a
// space in it must not send the station watching " ".
func TestABlankDirectoryOptionIsNoDirectoryAtAll(t *testing.T) {
	data := t.TempDir()
	c := catalog.SourceConfig{
		DataDir: data,
		Catalog: domain.CatalogConfig{Options: driverOptions(t, `{"directory":"   "}`)},
	}
	got, owned := Directory(c)
	if got != filepath.Join(data, "catalog", "incoming") || !owned {
		t.Errorf("un champ blanc doit valoir le répertoire du poste, obtenu %q (owned=%v)", got, owned)
	}
}

// TestANamedDirectoryThatIsAbsentIsRefusedAtBuild: New does not create it, and says so
// rather than watching a path that will never receive anything.
func TestANamedDirectoryThatIsAbsentIsRefusedAtBuild(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "jamais-monte")
	_, err := New(catalog.SourceConfig{
		DataDir: t.TempDir(),
		Clock:   fake.NewClock(t0),
		Catalog: domain.CatalogConfig{Options: driverOptions(t,
			`{"directory":`+strconv.Quote(absent)+`}`)},
	})
	if err == nil {
		t.Fatal("un répertoire nommé et absent doit être refusé, pas créé")
	}
	if _, statErr := os.Stat(absent); statErr == nil {
		t.Error("le service a créé un répertoire qu'un humain avait nommé")
	}
}

// TestANamedDirectoryIsTheOneTheStationReallyWatches closes the loop between the option
// and the file: a directory that is merely remembered and never watched would read right
// on the administration screen and receive nothing for ever.
func TestANamedDirectoryIsTheOneTheStationReallyWatches(t *testing.T) {
	chosen := t.TempDir()
	source, err := New(catalog.SourceConfig{
		StationNumber: 2,
		DataDir:       t.TempDir(),
		Clock:         fake.NewClock(t0),
		Catalog: domain.CatalogConfig{
			Options:          driverOptions(t, `{"directory":`+strconv.Quote(chosen)+`}`),
			Images:           domain.ImagesConfig{Source: domain.ImageSourceCSV},
			FallbackCategory: "other",
		},
	})
	if err != nil {
		t.Fatalf("construction de la source : %v", err)
	}
	t.Cleanup(func() { source.Close() })

	if want := filepath.Join(chosen, "flv_2.csv"); source.Path() != want {
		t.Fatalf("le poste surveille %q, attendu %q", source.Path(), want)
	}
	drop(t, source, aCatalog)
	source.poll(context.Background())
	if batch, err := source.poll(context.Background()); batch == nil {
		t.Fatalf("le fichier déposé dans le répertoire nommé n'a pas été lu : %v", err)
	}
}

// TestTheDescriptorDeclaresTheDropDirectory: an option the schema does not carry is
// refused by control 9 long before it could ever be honoured, and an option whose USE the
// schema does not carry is one the drop probe of control 46 will never look at.
func TestTheDescriptorDeclaresTheDropDirectory(t *testing.T) {
	for _, option := range Descriptor().Options {
		if option.Key == DirectoryOption {
			if option.Kind != domain.OptionText {
				t.Errorf("le répertoire est déclaré %q, attendu du texte", option.Kind)
			}
			if option.Use != domain.UseDropDirectory {
				t.Errorf("le répertoire n'est pas déclaré comme répertoire de dépôt : "+
					"les contrôles 39 et 46 lisent cet usage, et rien d'autre ne leur dit "+
					"que %q nomme un répertoire", DirectoryOption)
			}
			return
		}
	}
	t.Errorf("le descripteur ne déclare pas %q", DirectoryOption)
}

// TestTheDomainActsOnTheUseThisPackageDeclares.
//
// The tie between the two sides USED to be control 47, which spelled `directory` inside
// internal/domain: one key written twice, and a third source that could not be added
// without editing the domain. The tie is now the SCHEMA — this package says which of its
// keys names a drop directory, and the controls act on that declaration without knowing
// any source by name (ADR-052).
//
// So what has to be proved is no longer that two spellings match, but that the
// declaration REACHES the control: an HTTP host typed into the drop path is refused
// (control 39, important-11) on a registry that carries nothing but this package's own
// descriptor.
func TestTheDomainActsOnTheUseThisPackageDeclares(t *testing.T) {
	registry := catalog.NewRegistry()
	registry.Register(Descriptor())
	registries := domain.Registries{CatalogSources: registry.Descriptors()}

	config := domain.Config{Catalog: domain.CatalogConfig{
		Type: domain.CatalogSourceLocalDrop,
		Options: driverOptions(t,
			`{"`+DirectoryOption+`":`+strconv.Quote("https://dav.example.org/partage")+`}`),
	}}

	for _, fault := range config.Validate(registries) {
		if fault.Field == "catalog.options."+DirectoryOption {
			return
		}
	}
	t.Fatalf("un hôte HTTP derrière %q n'est pas refusé : l'usage déclaré par ce paquet "+
		"n'atteint pas le contrôle 39", DirectoryOption)
}
