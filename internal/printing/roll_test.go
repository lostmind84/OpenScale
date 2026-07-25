package printing

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"openscale/internal/domain"
)

// memoryRoll is the persistence of a station, reduced to what a roll counter uses.
type memoryRoll struct {
	mu      sync.Mutex
	printed int64
	known   bool
	// addErr and setErr break the store on demand: a full disk, a locked database.
	addErr, setErr, readErr error
	adds, sets              int
}

func (m *memoryRoll) AddLabels(_ context.Context, n int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.adds++
	if m.addErr != nil {
		return 0, m.addErr
	}
	m.printed += n
	m.known = true
	return m.printed, nil
}

func (m *memoryRoll) SetLabels(_ context.Context, n int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sets++
	if m.setErr != nil {
		return m.setErr
	}
	m.printed, m.known = n, true
	return nil
}

func (m *memoryRoll) Labels(context.Context) (int64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.readErr != nil {
		return 0, false, m.readErr
	}
	return m.printed, m.known, nil
}

// recordedLog captures what a component tells the technical journal.
type recordedLog struct {
	mu    sync.Mutex
	lines []string
}

func (r *recordedLog) Technical(level, source, code, message, detail string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, strings.Join([]string{level, source, code, message, detail}, "|"))
}

func (r *recordedLog) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lines...)
}

// TestTheAmberLightComesOnWhereTheDocumentPutsIt reproduces §8.5 and §15.4 to the
// label: capacity 1000, 900 printed, « environ 100 étiquettes restantes », amber.
//
// The figures are the document's, not this test's: they are the only two the project
// states (roll_capacity 1000 in config-lacagette.json, the 90 % threshold in §8.5), and
// §21 n° 12 says plainly that the real capacity of a roll is still to be read off the
// supplier's label.
func TestTheAmberLightComesOnWhereTheDocumentPutsIt(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name    string
		printed int64
		level   string
		says    string
	}{
		{"rouleau neuf", 0, domain.LevelInfo, "environ 1000 étiquettes restantes"},
		{"juste avant le seuil", 899, domain.LevelInfo, "environ 101 étiquettes restantes"},
		{"au seuil de 90 %", 900, domain.LevelWarn, "environ 100 étiquettes restantes"},
		{"une seule restante", 999, domain.LevelWarn, "environ 1 étiquette restantes"},
		{"rouleau épuisé", 1000, domain.LevelWarn, "probablement fini"},
		{"au-delà de la capacité", 1400, domain.LevelWarn, "probablement fini"},
	} {
		t.Run(c.name, func(t *testing.T) {
			counter := NewRollCounter(&memoryRoll{}, 1000, nil)
			if err := counter.SetPrinted(ctx, c.printed); err != nil {
				t.Fatalf("SetPrinted(%d) : %v", c.printed, err)
			}
			state := counter.State()
			if state.Level != c.level {
				t.Errorf("feu = %q, attendu %q à %d étiquettes imprimées", state.Level, c.level, c.printed)
			}
			if !strings.Contains(state.Message, c.says) {
				t.Errorf("message « %s » : il devait contenir « %s ». C'est la phrase que lit "+
					"un bénévole (§15.4)", state.Message, c.says)
			}
			if state.Level == domain.LevelError {
				t.Error("feu rouge : une fin de rouleau est une maintenance, jamais une panne")
			}
		})
	}
}

// TestASingleLabelIsSpelledSingular — « environ 1 étiquettes restantes » is the kind of
// carelessness that teaches a volunteer to stop reading the light.
func TestASingleLabelIsSpelledSingular(t *testing.T) {
	counter := NewRollCounter(nil, 1000, nil)
	if err := counter.SetPrinted(context.Background(), 999); err != nil {
		t.Fatalf("SetPrinted : %v", err)
	}
	if got := counter.State().Message; !strings.Contains(got, "1 étiquette ") {
		t.Errorf("message « %s » : au singulier, « 1 étiquette »", got)
	}
}

// TestAFreshStationSaysItDoesNotKnow: a counter nobody has ever written must not
// announce « environ 1000 étiquettes restantes » about a roll nobody has described.
func TestAFreshStationSaysItDoesNotKnow(t *testing.T) {
	counter := NewRollCounter(&memoryRoll{}, 1000, nil)
	if err := counter.Load(context.Background()); err != nil {
		t.Fatalf("Load : %v", err)
	}
	state := counter.State()

	if state.Known {
		t.Error("le compteur se dit renseigné alors que rien ne l'a jamais écrit")
	}
	if !strings.Contains(state.Message, "non renseigné") {
		t.Errorf("message « %s » : il doit dire que le rouleau n'est pas renseigné", state.Message)
	}
	if state.Level != domain.LevelInfo {
		t.Errorf("feu = %q : ne rien savoir d'un rouleau neuf n'est pas une alerte", state.Level)
	}
}

// TestIChangedTheRollPutsItBackToZero — the button of §14.4, and the only gesture that
// tells this application anything true about the paper.
func TestIChangedTheRollPutsItBackToZero(t *testing.T) {
	ctx := context.Background()
	store := &memoryRoll{}
	counter := NewRollCounter(store, 1000, nil)

	counter.Printed(ctx, 950)
	if state := counter.State(); state.Level != domain.LevelWarn {
		t.Fatalf("feu = %q après 950 étiquettes, attendu orange", state.Level)
	}
	if err := counter.Changed(ctx); err != nil {
		t.Fatalf("« J'ai changé le rouleau » : %v", err)
	}

	state := counter.State()
	if state.Printed != 0 || state.Remaining != 1000 || state.Level != domain.LevelInfo {
		t.Errorf("après le changement : %+v", state)
	}
	if store.printed != 0 {
		t.Errorf("la base porte encore %d : le compteur survit à un redémarrage, "+
			"la remise à zéro doit y aller aussi", store.printed)
	}
}

// TestTheCounterIsRecalibratedByHand: a half-used roll, a bigger roll, a restored
// database. The figure is the volunteer's, and only a negative one is refused.
func TestTheCounterIsRecalibratedByHand(t *testing.T) {
	ctx := context.Background()
	counter := NewRollCounter(&memoryRoll{}, 1000, nil)

	if err := counter.SetPrinted(ctx, 400); err != nil {
		t.Fatalf("recalage à 400 : %v", err)
	}
	if got := counter.State().Remaining; got != 600 {
		t.Errorf("restantes = %d, attendu 600", got)
	}
	// A roll bigger than the configured capacity is a real thing: the capacity is a
	// default nobody has measured (§21 n° 12).
	if err := counter.SetPrinted(ctx, 1500); err != nil {
		t.Errorf("recalage au-delà de la capacité refusé : %v", err)
	}
	err := counter.SetPrinted(ctx, -1)
	if err == nil {
		t.Fatal("un compteur négatif a été accepté : il n'existe pas moins quatre étiquettes")
	}
	if !strings.Contains(err.Error(), "négatif") {
		t.Errorf("message « %s » : il doit dire pourquoi, en français", err)
	}
}

// TestABrokenDatabaseNeverStopsTheCount is the design of Printed in one test: the
// signature returns nothing, so a persistence failure has nowhere to travel. The count
// carries on in memory and the journal says so.
func TestABrokenDatabaseNeverStopsTheCount(t *testing.T) {
	ctx := context.Background()
	log := &recordedLog{}
	store := &memoryRoll{addErr: errors.New("database is locked")}
	counter := NewRollCounter(store, 1000, log)

	counter.Printed(ctx, 1)
	counter.Printed(ctx, 1)

	if got := counter.State().Printed; got != 2 {
		t.Errorf("compteur = %d, attendu 2 : une base en panne ne fait pas oublier "+
			"les étiquettes déjà sorties", got)
	}
	lines := log.all()
	if len(lines) != 2 {
		t.Fatalf("%d ligne(s) au journal technique, attendu 2", len(lines))
	}
	if !strings.HasPrefix(lines[0], domain.LevelWarn+"|printer|") ||
		!strings.Contains(lines[0], "l'impression, elle, a bien eu lieu") {
		t.Errorf("ligne « %s » : elle doit dire que l'impression a eu lieu", lines[0])
	}
}

// TestTheStoreWinsWhenItAnswers: two stations, a restart mid-roll, and the in-memory
// tally would drift away from the row that survives.
func TestTheStoreWinsWhenItAnswers(t *testing.T) {
	ctx := context.Background()
	store := &memoryRoll{printed: 700, known: true}
	counter := NewRollCounter(store, 1000, nil)

	counter.Printed(ctx, 1) // the counter starts at 0 in memory, the store at 700
	if got := counter.State().Printed; got != 701 {
		t.Errorf("compteur = %d, attendu 701 : c'est la base qui fait foi quand elle répond", got)
	}
}

// TestAStoreThatCannotBeReadIsReportedAtStartUp: Load is the ONE place a failure may
// reach the caller, because nobody is waiting at the scale when it runs.
func TestAStoreThatCannotBeReadIsReportedAtStartUp(t *testing.T) {
	counter := NewRollCounter(&memoryRoll{readErr: errors.New("disque")}, 1000, nil)
	err := counter.Load(context.Background())
	if err == nil {
		t.Fatal("Load a caché une base illisible")
	}
	if !strings.Contains(err.Error(), "compteur de rouleau") {
		t.Errorf("message « %s » : il doit nommer le compteur de rouleau", err)
	}
}

// TestARecalibrationThatCannotBeSavedIsReported: unlike Printed, this one is an admin
// gesture, and a volunteer who pressed a button must learn that it did nothing.
func TestARecalibrationThatCannotBeSavedIsReported(t *testing.T) {
	store := &memoryRoll{setErr: errors.New("disque plein")}
	counter := NewRollCounter(store, 1000, nil)

	if err := counter.Changed(context.Background()); err == nil {
		t.Fatal("« J'ai changé le rouleau » a répondu « c'est fait » sans rien enregistrer")
	}
}

// TestACounterWithNoCapacityFallsBackOnTheShippedOne: control 41 of Config.Validate has
// already refused anything under 50 labels, so what is left here is a caller that
// passed nothing — and a station is not worth refusing to start over a figure whose
// only job is to colour a light.
func TestACounterWithNoCapacityFallsBackOnTheShippedOne(t *testing.T) {
	for _, capacity := range []int{0, -1} {
		if got := NewRollCounter(nil, capacity, nil).Capacity(); got != DefaultRollCapacity {
			t.Errorf("capacité %d -> %d, attendu %d", capacity, got, DefaultRollCapacity)
		}
	}
}

// TestACounterWithNoStoreStillCounts: a station whose database is not open yet, and
// every test that does not care.
func TestACounterWithNoStoreStillCounts(t *testing.T) {
	ctx := context.Background()
	counter := NewRollCounter(nil, 1000, nil)

	if err := counter.Load(ctx); err != nil {
		t.Fatalf("Load sans base : %v", err)
	}
	counter.Printed(ctx, 3)
	counter.Printed(ctx, 0)  // nothing came out: nothing is counted
	counter.Printed(ctx, -5) // and a negative count is not a correction
	if got := counter.State().Printed; got != 3 {
		t.Errorf("compteur = %d, attendu 3", got)
	}
}
