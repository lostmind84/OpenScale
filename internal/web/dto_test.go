package web

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/station"
	"openscale/internal/station/ports"
)

// rewriteGolden rewrites the golden files instead of comparing against them. It is
// the one way this file may be made to pass without thinking, so it is a flag
// somebody has to type.
//
// The Go identifier is NOT « update »: this package now imports internal/update,
// and a package-level variable of that name would shadow it across the whole test
// binary. The command-line flag keeps its name -- it is what people type.
var rewriteGolden = flag.Bool("update", false, "réécrit les fichiers golden au lieu de les comparer")

// TestStateGoldenFreezesTheWireContract is the test §14.5 asks for: the DTO is
// DECOUPLED from the core, and the price of that decoupling is one conversion
// function frozen by a golden JSON file.
//
// What it protects, concretely: a volunteer updates three stations out of four, and
// the fourth serves last month's bundle against this month's service. Renaming
// domain.Label.NetWeight must not reach that screen. This file goes red the moment a
// JSON name moves, and that redness is the review that decides whether the front end
// has to be shipped in the same hour.
func TestStateGoldenFreezesTheWireContract(t *testing.T) {
	server := goldenServer(t)
	got, err := json.MarshalIndent(server.stateOf(richSnapshot()), "", "  ")
	if err != nil {
		t.Fatalf("sérialisation du DTO : %v", err)
	}
	got = append(got, '\n')

	golden := filepath.Join("testdata", "state.json")
	if *rewriteGolden {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatalf("écriture du golden : %v", err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("lecture du golden : %v (relancer avec -update pour le créer)", err)
	}
	if string(got) != string(want) {
		t.Fatalf("le DTO a changé de forme.\n--- attendu ---\n%s\n--- obtenu ---\n%s\n"+
			"Si le changement est voulu, il casse l'IHM d'un poste non mis à jour : "+
			"relancer avec -update ET livrer le front en même temps.", want, got)
	}
}

// TestEveryPublishedFieldNamesItselfExplicitly walks the DTO and refuses a field
// without a json tag.
//
// Without it, adding a Go field would publish it under its GO NAME — the very
// coupling this package exists to remove, and one no golden file catches, because a
// field added to both sides at once agrees with itself.
func TestEveryPublishedFieldNamesItselfExplicitly(t *testing.T) {
	for _, published := range []any{
		stateDTO{}, weightDTO{}, productDTO{}, labelDTO{}, priceDTO{}, reprintDTO{},
		messageDTO{}, diagnosticDTO{}, scaleDTO{}, printerDTO{}, degradationDTO{},
		catalogDTO{}, categoryDTO{}, catalogProductDTO{}, ackDTO{}, weighingDTO{},
		importDTO{}, findingDTO{}, decisionDTO{}, technicalLineDTO{},
	} {
		assertTagged(t, reflect.TypeOf(published))
	}
}

// assertTagged fails for every exported field with no json name of its own.
func assertTagged(t *testing.T, typ reflect.Type) {
	t.Helper()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		tag, declared := field.Tag.Lookup("json")
		if !declared || strings.HasPrefix(tag, ",") || tag == "" {
			t.Errorf("%s.%s ne déclare pas son nom JSON : il serait publié sous son nom Go",
				typ.Name(), field.Name)
		}
	}
}

// TestTheDTONeverCarriesACoreType is cut 4 of §5.2, checked rather than reviewed.
//
// A field typed domain.Grams would serialize correctly today and would inherit any
// MarshalJSON that type ever grows — silently, and on the wire of a station nobody is
// updating that day.
func TestTheDTONeverCarriesACoreType(t *testing.T) {
	seen := make(map[reflect.Type]bool)
	assertNoCoreType(t, reflect.TypeOf(stateDTO{}), seen)
	assertNoCoreType(t, reflect.TypeOf(catalogDTO{}), seen)
	assertNoCoreType(t, reflect.TypeOf(weighingDTO{}), seen)
}

// assertNoCoreType walks a DTO and fails on any type declared outside this package.
func assertNoCoreType(t *testing.T, typ reflect.Type, seen map[reflect.Type]bool) {
	t.Helper()
	if seen[typ] {
		return
	}
	seen[typ] = true
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		inner := field.Type
		for inner.Kind() == reflect.Pointer || inner.Kind() == reflect.Slice {
			inner = inner.Elem()
		}
		switch {
		case inner.PkgPath() == "":
			// A builtin: string, int64, bool. Exactly what a DTO is made of.
		case inner.PkgPath() == "encoding/json":
			// json.RawMessage carries a document somebody else already framed.
		case strings.HasPrefix(inner.PkgPath(), "openscale/internal/web"):
			assertNoCoreType(t, inner, seen)
		default:
			t.Errorf("%s.%s est de type %s : le DTO ne sérialise jamais un type du noyau (coupe 4)",
				typ.Name(), field.Name, inner.String())
		}
	}
}

// TestNoListEverComesBackAsNull holds the promise every list of §14.5 makes: a list
// with nothing in it is written `[]`, and never `null`.
//
// A Go slice left nil serializes as `null`, and the TypeScript contract declares those
// fields as arrays — `findings: FindingDTO[]`, never `FindingDTO[] | null`. So the
// screen filters them, maps them or spreads them the instant it has read them, and a
// `null` is an uncaught TypeError: the ERR-UI-01 net reports it and RELOADS the page, so
// what a volunteer sees is an administration that closes itself in their face.
//
// It has happened twice. `retiredOrEmpty` in config.go is the scar of the first, on the
// very first render after a successful login. The second was the Catalogue page of a
// station with no catalog — the nominal state of a station installed this morning:
// `GET /admin/api/imports` is read with no `?id=` because there is no import to name,
// answered `"findings": null`, and the page died on the first filter.
//
// The station under this bench has read nothing yet, which is precisely the state that
// tests built on rich fixtures cannot see.
func TestNoListEverComesBackAsNull(t *testing.T) {
	b := adminBench(t)

	// The dashboard is walked through its OWN type rather than a list of names: it is
	// the widest payload of §14.5, it grows, and a list added tomorrow has to be covered
	// without anybody remembering this test exists.
	assertListsAreWritten(t, "/admin/api/health",
		decodeStatus[map[string]json.RawMessage](t, b.get("/admin/api/health"), http.StatusOK),
		reflect.TypeOf(adminHealthDTO{}))

	// The other routes frame their answer in an anonymous struct, which has no type to
	// walk. They carry one or two lists each, and here they are named.
	for _, route := range []struct {
		path  string
		lists []string
	}{
		{"/admin/api/imports", []string{"imports", "findings"}},
		{"/admin/api/journal", []string{"weighings"}},
		{"/admin/api/technical", []string{"entries"}},
		{"/api/v1/catalog", []string{"products", "categories"}},
	} {
		payload := decodeStatus[map[string]json.RawMessage](t, b.get(route.path), http.StatusOK)
		for _, name := range route.lists {
			if string(payload[name]) == "null" {
				t.Errorf("%s : %q répond null, et le contrat le déclare liste", route.path, name)
			}
		}
	}

	// A station with NO journal at all (ADR-013) still draws its dashboard, and it is the
	// harshest case: nothing is read, so every list is left exactly as Go declared it.
	bare := newBench(t, func(o *benchOptions) { o.noStore = true })
	assertListsAreWritten(t, "/admin/api/health sans journal",
		decodeStatus[map[string]json.RawMessage](t, bare.get("/admin/api/health"), http.StatusOK),
		reflect.TypeOf(adminHealthDTO{}))
}

// assertListsAreWritten fails for every field the struct declares as a list and the
// payload answered `null` for, walking into the objects that payload carries.
//
// A field the payload omits is skipped: `omitempty` makes absence the contract, and the
// TypeScript side declares those optional.
func assertListsAreWritten(
	t *testing.T, where string, payload map[string]json.RawMessage, typ reflect.Type,
) {
	t.Helper()
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		raw, published := payload[name]
		if name == "" || name == "-" || !published {
			continue
		}
		inner := typ.Field(i).Type
		if inner.Kind() == reflect.Pointer {
			inner = inner.Elem()
		}
		switch inner.Kind() {
		case reflect.Slice:
			if string(raw) == "null" {
				t.Errorf("%s : %q répond null, et le contrat le déclare liste", where, name)
			}
		case reflect.Struct:
			// A null object is a legitimate answer — « ce poste n'a pas de rouleau » — and
			// it carries no list to check.
			var nested map[string]json.RawMessage
			if err := json.Unmarshal(raw, &nested); err == nil && nested != nil {
				assertListsAreWritten(t, where+" → "+name, nested, inner)
			}
		}
	}
}

// goldenServer is a Server with nothing but a clock and a Hub that answers from a
// fixed snapshot: the conversion is a pure function of what it is given.
func goldenServer(t *testing.T) *Server {
	t.Helper()
	server, err := New(Options{Clock: fake.NewClock(epoch), Hub: stubHub{}})
	if err != nil {
		t.Fatalf("web.New : %v", err)
	}
	return server
}

// richSnapshot fills EVERY field of a snapshot, including the ones a nominal station
// leaves empty.
//
// A golden built from a station at rest would freeze half a contract: the message, the
// degradation, the diagnostics and the reprint bar only exist on a bad day, and a bad
// day is exactly when a screen must not break.
func richSnapshot() station.Snapshot {
	product := domain.Product{
		ID: garlicID, Name: "AIL", Reference: "0493021000003",
		Mode: domain.ByWeight, PriceSuffix: " €/kg", UnitPrice: 532,
		CategoryCode: "vegetables", Qualification: domain.Weighable,
		ImageSHA: strings.Repeat("ab", 32),
	}
	member := domain.PriceLine{
		Tier:      domain.PriceTier{Code: "MEMBER", Label: "Adhérent", Abbrev: "A", Rank: 1},
		UnitPrice: 479, Amount: 592,
	}
	solidarity := domain.PriceLine{
		Tier:      domain.PriceTier{Code: "SOLIDARITY", Label: "Solidaire", Abbrev: "S", Rank: 2},
		UnitPrice: 532, Amount: 658,
	}
	label := domain.Label{
		Product: product, Mode: domain.ByWeight,
		GrossWeight: 1236, Tare: 0, NetWeight: 1236, Quantity: 1,
		Lines:       []domain.PriceLine{member, solidarity},
		PrimaryLine: &member, ReferenceLine: &solidarity,
		Barcode: garlicBarcode, JobID: "01J9F2ABC",
	}
	return station.Snapshot{
		Revision: 42, At: epoch, State: domain.Printing, Station: 2,
		Weight: station.Weight{
			Gross: 1236, Tare: 0, Net: 1236, Quantity: 1,
			Stability: domain.Stable, Latched: true, Seq: 7,
			Age: 120 * time.Millisecond, Expiry: 1200 * time.Millisecond,
		},
		HasWeight: true, Expired: false,
		Product: &product, Tare: 0, Units: 1,
		Label: &label, LastLabel: &label,
		LastPrintedAt: epoch.Add(-3 * time.Second), ReprintAvailable: true,
		Message: &station.Message{
			Level: domain.LevelInfo, Code: "", Text: "Étiquette envoyée.",
			ExpiresAt: epoch.Add(5 * time.Second),
		},
		Sound: "ok",
		Diagnostics: []domain.Diagnostic{{
			Code: domain.CodeWeightUnstable, Severity: domain.Info,
			Message: domain.DefaultMessage(domain.CodeWeightUnstable), ProductID: garlicID,
		}},
		FaultCode:       "ERR-PRN-01",
		ArmingExpiresAt: epoch.Add(10 * time.Second),
		Catalog:         garlicCatalog(),
		Scale: station.ScaleHealth{
			Connected: true, Median: 400 * time.Millisecond, Observations: 64,
			Provisional: false, TooSlow: false,
		},
		Printer: station.PrinterHealth{
			Health: ports.PrinterConsumable, Detail: "Fin de rouleau proche.",
			PendingJobs: 1, ObservedAt: epoch.Add(-time.Second),
		},
		Degraded: &station.Degradation{
			Since: epoch.Add(-time.Hour), Code: "ERR-SCL-03",
			Reason: "Le port de la balance ne peut pas être ouvert.",
		},
		UnloggedWeighings: 3,
	}
}

// stubHub answers from a fixed snapshot and starts no goroutine.
type stubHub struct{}

func (stubHub) State() station.Snapshot { return richSnapshot() }

func (stubHub) Submit(context.Context, domain.Event, string) (domain.Ack, error) {
	return domain.Ack{Accepted: true, State: domain.Idle}, nil
}

func (stubHub) Subscribe() (<-chan station.Snapshot, func()) {
	ch := make(chan station.Snapshot)
	return ch, func() {}
}

func (stubHub) Config() domain.Config    { return domain.Config{} }
func (stubHub) Catalog() *domain.Catalog { return garlicCatalog() }

// stubCatalogAt is the instant the stub says its catalog entered service.
var stubCatalogAt = time.Date(2026, 7, 27, 8, 6, 48, 0, time.UTC)

func (stubHub) CatalogUpdatedAt() time.Time { return stubCatalogAt }

var _ Hub = stubHub{}
