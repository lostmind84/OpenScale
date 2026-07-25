package printing

import (
	"strings"
	"testing"

	"openscale/internal/station/ports"
)

// ok and failed are the two things level N1 can ever observe.
func wroteOK() *WriteOutcome { return &WriteOutcome{OK: true, Detail: "file « SATO WS408_2 »"} }
func wroteFailed() *WriteOutcome {
	return &WriteOutcome{Detail: "file « SATO WS408_2 » : accès refusé"}
}

// TestAPrinterKnownOnlyAtN1IsNeverAnnouncedReady is the assertion this whole file
// exists for.
//
// N1 is a write that was accepted by a TRANSPORT. A Windows queue accepts jobs while
// the printer is unplugged, a device node accepts them with the cover open, and the
// `file` transport accepts everything by construction. Announcing « prête » on that
// evidence costs a roll: a volunteer reads a green light, walks away, and the labels
// pile up in a queue.
//
// The property is checked over EVERY shape of N1 observation, alone and beside the two
// levels that could not look — a silent probe and an unsupported one — because « nobody
// answered » must never be readable as « nothing is wrong ».
func TestAPrinterKnownOnlyAtN1IsNeverAnnouncedReady(t *testing.T) {
	silent := &Native{}           // asked, nothing came back
	unsupported := (*Native)(nil) // one-way transport: cannot ask
	for _, c := range []struct {
		name string
		seen Observations
	}{
		{"écriture réussie, rien d'autre", Observations{Write: wroteOK()}},
		{"écriture réussie, imprimante muette", Observations{Write: wroteOK(), Native: silent}},
		{"écriture réussie, transport unidirectionnel", Observations{Write: wroteOK(), Native: unsupported}},
		{"écriture échouée", Observations{Write: wroteFailed()}},
		{"écriture échouée, imprimante muette", Observations{Write: wroteFailed(), Native: silent}},
		{"aucune observation", Observations{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			report := Assess(c.seen)
			if report.Health == ports.PrinterReady || report.Ready() {
				t.Fatalf("santé = prête au niveau %s : une imprimante connue au seul niveau N1 "+
					"n'est jamais annoncée prête (§8.5). Détail : %s", report.Level, report.Detail)
			}
			if report.Level > LevelN1 {
				t.Errorf("niveau = %s : seul N1 a observé quelque chose ici", report.Level)
			}
			if report.Detail == "" {
				t.Error("détail vide : c'est la phrase que lit un bénévole sur l'écran de dépannage")
			}
		})
	}
}

// TestOnlyTheLevelsThatCanSeeAllClearMayReport permits the green light exactly where
// §8.5 rates it certain, and nowhere else.
func TestOnlyTheLevelsThatCanSeeAllClearMayReport(t *testing.T) {
	for _, c := range []struct {
		name  string
		seen  Observations
		ready bool
		level Level
	}{
		{
			name:  "la file du système ne signale rien",
			seen:  Observations{Write: wroteOK(), Queue: &Queue{}},
			ready: true,
			level: LevelN2,
		},
		{
			name:  "l'imprimante répond et la trame est décodée",
			seen:  Observations{Write: wroteOK(), Native: &Native{Raw: []byte{0x30}, Condition: &Condition{}}},
			ready: true,
			level: LevelN3,
		},
		{
			name:  "l'imprimante répond mais la trame n'est pas décodée",
			seen:  Observations{Write: wroteOK(), Native: &Native{Raw: []byte{0x30, 0x41}}},
			ready: false,
			level: LevelN3,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			report := Assess(c.seen)
			if report.Ready() != c.ready {
				t.Errorf("prête = %v, attendu %v (santé %d, niveau %s) — %s",
					report.Ready(), c.ready, report.Health, report.Level, report.Detail)
			}
			if report.Level != c.level {
				t.Errorf("niveau = %s, attendu %s", report.Level, c.level)
			}
		})
	}
}

// TestAnUndecodedAnswerSaysAliveAndNotReady pins the sentence itself: it is the one a
// volunteer reads, and « vivante » and « prête » are not the same promise.
func TestAnUndecodedAnswerSaysAliveAndNotReady(t *testing.T) {
	report := Assess(Observations{Native: &Native{Raw: []byte{0x30, 0x41, 0x42}}})

	if report.Health != ports.PrinterUnknown {
		t.Errorf("santé = %d, attendu inconnue : la trame revenue peut dire PAPER OUT", report.Health)
	}
	if !strings.Contains(report.Detail, "vivante") || !strings.Contains(report.Detail, "3 octet") {
		t.Errorf("détail « %s » : il doit dire que l'imprimante est vivante et combien d'octets "+
			"elle a renvoyés", report.Detail)
	}
	if len(report.Raw) != 3 {
		t.Errorf("Raw = %d octets : c'est la trame affichée en hexa dans l'admin, elle remonte "+
			"toujours (§8.5)", len(report.Raw))
	}
}

// TestAFaultSeenAtAnyLevelIsNeverTalkedAwayByAnother: no level may overrule another
// level's fault. A queue that looks fine does not undo a write that failed.
func TestAFaultSeenAtAnyLevelIsNeverTalkedAwayByAnother(t *testing.T) {
	for _, c := range []struct {
		name string
		seen Observations
	}{
		{"N1 en panne, N2 tout va bien", Observations{Write: wroteFailed(), Queue: &Queue{}}},
		{"N1 en panne, N3 décodée sans faute", Observations{
			Write: wroteFailed(), Native: &Native{Condition: &Condition{}}}},
		{"N2 hors ligne, N3 décodée sans faute", Observations{
			Write: wroteOK(), Queue: &Queue{Condition: Condition{Offline: true}},
			Native: &Native{Condition: &Condition{}}}},
		{"N3 en panne, N2 tout va bien", Observations{
			Write: wroteOK(), Queue: &Queue{}, Native: &Native{Failed: true}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if report := Assess(c.seen); report.Health != ports.PrinterFaulted {
				t.Fatalf("santé = %d, attendu en panne : une panne vue à un niveau ne se fait pas "+
					"contredire par un autre. Détail : %s", report.Health, report.Detail)
			}
		})
	}
}

// TestTheEndOfARollIsNeverAFailure — important-9, from the status side. The queue
// saying PAPER_OUT must produce the amber maintenance state, never a fault: the last
// label came out.
func TestTheEndOfARollIsNeverAFailure(t *testing.T) {
	report := Assess(Observations{Write: wroteOK(), Queue: &Queue{Condition: Condition{PaperOut: true}}})

	if report.Health != ports.PrinterConsumable {
		t.Fatalf("santé = %d, attendu consommable : la dernière étiquette est sortie, "+
			"la pesée reste un succès (important-9)", report.Health)
	}
	if !strings.Contains(report.Detail, "rouleau") {
		t.Errorf("détail « %s » : il doit nommer le rouleau, c'est le geste attendu", report.Detail)
	}
}

// TestEveryConditionIsSaidAndTheWorstDecides: a printer that is offline AND out of
// paper needs both gestures, so both are said — and the health is the worst of them.
func TestEveryConditionIsSaidAndTheWorstDecides(t *testing.T) {
	for _, c := range []struct {
		name   string
		cond   Condition
		health ports.PrinterHealth
		says   []string
	}{
		{"rien", Condition{}, ports.PrinterReady, []string{"prête"}},
		{"hors ligne", Condition{Offline: true}, ports.PrinterFaulted, []string{"hors ligne"}},
		{"bourrage", Condition{PaperJam: true}, ports.PrinterFaulted, []string{"bourrage"}},
		{"erreur", Condition{Error: true}, ports.PrinterFaulted, []string{"erreur"}},
		{"sans papier", Condition{PaperOut: true}, ports.PrinterConsumable, []string{"rouleau"}},
		{"hors ligne et sans papier", Condition{Offline: true, PaperOut: true},
			ports.PrinterFaulted, []string{"hors ligne", "rouleau"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := c.cond.Health(); got != c.health {
				t.Errorf("santé = %d, attendu %d", got, c.health)
			}
			detail := c.cond.Detail()
			for _, want := range c.says {
				if !strings.Contains(detail, want) {
					t.Errorf("détail « %s » : il doit contenir « %s »", detail, want)
				}
			}
			if c.cond.OK() != (c.health == ports.PrinterReady) {
				t.Errorf("OK() = %v pour la santé %d", c.cond.OK(), c.health)
			}
		})
	}
}

// TestTheQueueDepthTravelsAndOnlyFromN2: PendingJobs is the one figure no other level
// can produce, and §8.5 names N2 as its only source.
func TestTheQueueDepthTravelsAndOnlyFromN2(t *testing.T) {
	report := Assess(Observations{Queue: &Queue{PendingJobs: 4}})
	if report.PendingJobs != 4 {
		t.Errorf("travaux en attente = %d, attendu 4", report.PendingJobs)
	}
	if !strings.Contains(report.Detail, "4 travail") {
		t.Errorf("détail « %s » : une file qui grossit doit se lire", report.Detail)
	}
	if n := Assess(Observations{Write: wroteOK(), Native: &Native{Raw: []byte{1}}}).PendingJobs; n != 0 {
		t.Errorf("travaux en attente = %d sans niveau N2 : aucun autre niveau ne connaît "+
			"la profondeur d'une file", n)
	}
}

// TestASilentPrinterRaisesNothing: a probe that got no answer LEARNED NOTHING, so it
// neither raises the level nor changes the health. The contrapositive of « toute
// réponse non vide = vivante » is « on ne sait pas », never « morte » (§8.5).
func TestASilentPrinterRaisesNothing(t *testing.T) {
	silent := Assess(Observations{Write: wroteOK(), Native: &Native{}})
	alone := Assess(Observations{Write: wroteOK()})

	if silent.Level != alone.Level || silent.Health != alone.Health {
		t.Fatalf("un silence a changé la conclusion : %s/%d contre %s/%d",
			silent.Level, silent.Health, alone.Level, alone.Health)
	}
	if silent.Health == ports.PrinterFaulted {
		t.Error("un silence est devenu une panne : c'est exactement ce que §8.5 interdit")
	}
}

// TestTheReportConvertsToWhatTheHubConsumes: the Hub sees ports.PrinterStatus and
// nothing else; every field has to survive the conversion.
func TestTheReportConvertsToWhatTheHubConsumes(t *testing.T) {
	report := Assess(Observations{
		Queue:  &Queue{Condition: Condition{PaperOut: true}, PendingJobs: 2},
		Native: &Native{Raw: []byte{0x05, 0x06}},
	})
	status := report.Status()

	if status.Health != report.Health || status.Detail != report.Detail ||
		status.PendingJobs != 2 || len(status.Raw) != 2 {
		t.Errorf("conversion incomplète : %+v contre %+v", status, report)
	}
}

// TestADriverConclusionBecomesAnObservation covers the mapping of §8.5's N3 row: what
// a driver ANSWERS is a conclusion, and this package needs the evidence under it.
func TestADriverConclusionBecomesAnObservation(t *testing.T) {
	for _, c := range []struct {
		name   string
		status ports.PrinterStatus
		want   Native
	}{
		{"transport en panne", ports.PrinterStatus{Health: ports.PrinterFaulted, Detail: "d"},
			Native{Detail: "d", Failed: true}},
		{"sans papier", ports.PrinterStatus{Health: ports.PrinterConsumable},
			Native{Condition: &Condition{PaperOut: true}}},
		{"rien à signaler", ports.PrinterStatus{Health: ports.PrinterReady},
			Native{Condition: &Condition{}}},
		{"vivante non décodée", ports.PrinterStatus{Health: ports.PrinterUnknown, Raw: []byte{9}},
			Native{Raw: []byte{9}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := nativeFrom(c.status)
			if got.Failed != c.want.Failed || got.Detail != c.want.Detail ||
				len(got.Raw) != len(c.want.Raw) {
				t.Fatalf("observation = %+v, attendu %+v", *got, c.want)
			}
			switch {
			case (got.Condition == nil) != (c.want.Condition == nil):
				t.Fatalf("condition = %v, attendu %v", got.Condition, c.want.Condition)
			case got.Condition != nil && *got.Condition != *c.want.Condition:
				t.Fatalf("condition = %+v, attendu %+v", *got.Condition, *c.want.Condition)
			}
		})
	}
}

// TestEachLevelSpellsItselfOnce: one spelling per value, shared by the journal, the
// database and the screen.
func TestEachLevelSpellsItselfOnce(t *testing.T) {
	spellings := map[Level]string{LevelNone: "none", LevelN1: "N1", LevelN2: "N2", LevelN3: "N3"}
	seen := map[string]bool{}
	for level, want := range spellings {
		got := level.String()
		if got != want {
			t.Errorf("niveau %d s'écrit %q, attendu %q", level, got, want)
		}
		if seen[got] {
			t.Errorf("l'orthographe %q sert deux fois", got)
		}
		seen[got] = true
	}
	if got := Level(9).String(); got != "unknown" {
		t.Errorf("un niveau inconnu s'écrit %q", got)
	}
}
