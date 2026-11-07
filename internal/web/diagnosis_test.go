package web

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
)

// fakePrinterDiagnostician is the test double of the printer diagnostic of page
// Matériel : no real printer, no real transport, just what each test needs to hand
// back or to wait on.
type fakePrinterDiagnostician struct {
	diagnosis PrinterDiagnosis
	err       error
	// block, when set, makes DiagnosePrinter wait on ctx instead of answering at
	// once -- TestTheDiagnosisIsBounded's double.
	block bool

	answer DiagnosisStep

	answerCalls int
	lastPrinted bool
	lastSeen    string
}

func (f *fakePrinterDiagnostician) DiagnosePrinter(ctx context.Context) (PrinterDiagnosis, error) {
	if f.block {
		<-ctx.Done()
		return PrinterDiagnosis{}, ctx.Err()
	}
	return f.diagnosis, f.err
}

func (f *fakePrinterDiagnostician) AnswerStep(printed bool, seen string) DiagnosisStep {
	f.answerCalls++
	f.lastPrinted, f.lastSeen = printed, seen
	return f.answer
}

var _ PrinterDiagnostician = (*fakePrinterDiagnostician)(nil)

// fiveDiagnosisSteps is one full, ordinary run : steps 1, 2 and 4 pass, step 3 is
// silent (decision 2, Global Constraints : silence is not failure), step 5 has not
// been put to the volunteer yet.
func fiveDiagnosisSteps() []DiagnosisStep {
	return []DiagnosisStep{
		{Number: 1, Title: "Le périphérique existe", State: "pass"},
		{Number: 2, Title: "Le poste a le droit d'y écrire", State: "pass"},
		{Number: 3, Title: "L'imprimante répond quand on lui demande son état", State: "unknown"},
		{Number: 4, Title: "Une étiquette de test est envoyée en entier", State: "pass"},
		{Number: 5, Title: "L'étiquette est-elle sortie ?", State: "unanswered"},
	}
}

// TestTheDiagnosisAnswersWithoutAPassword : a mot de passe is set on the station, but
// no session is opened -- neither route may answer 401 (decision 7).
func TestTheDiagnosisAnswersWithoutAPassword(t *testing.T) {
	diag := &fakePrinterDiagnostician{
		diagnosis: PrinterDiagnosis{Target: "ESC WS408", Steps: fiveDiagnosisSteps()},
		answer:    DiagnosisStep{Number: 5, State: "pass"},
	}
	b := newBench(t, func(o *benchOptions) { o.printerDiagnosis = diag })
	b.setPassword("mot-de-passe-long", "ABCD2345")

	if response := b.post("/admin/api/printer/diagnosis", `{}`); response.StatusCode != http.StatusOK {
		t.Fatalf("POST .../diagnosis sans session = %d, attendu 200", response.StatusCode)
	} else {
		response.Body.Close()
	}

	answerBody := `{"steps":["pass","pass","unknown","pass"],"printed":true,"seen":""}`
	if response := b.post("/admin/api/printer/diagnosis/answer", answerBody); response.StatusCode != http.StatusOK {
		t.Fatalf("POST .../diagnosis/answer sans session = %d, attendu 200", response.StatusCode)
	} else {
		response.Body.Close()
	}
}

// TestTheDiagnosisSaysWhatIsMissing : a station with no printer diagnostic wired
// answers 501 on both routes, named the same way every other unwired collaborator is
// (troubleshooting_test.go).
func TestTheDiagnosisSaysWhatIsMissing(t *testing.T) {
	b := newBench(t)

	for _, path := range []string{
		"/admin/api/printer/diagnosis",
		"/admin/api/printer/diagnosis/answer",
	} {
		response := b.post(path, `{}`)
		if response.StatusCode != http.StatusNotImplemented {
			t.Fatalf("%s sans collaborateur = %d, attendu 501", path, response.StatusCode)
		}
		if !strings.Contains(body(t, response), "pas disponible") {
			t.Fatalf("%s : le refus ne porte pas « pas disponible »", path)
		}
	}
}

// TestTheDiagnosisCarriesFiveSteps checks the JSON itself, decoded generically : the
// exact keys the plan promises, and step 3 left "unknown" rather than failed.
func TestTheDiagnosisCarriesFiveSteps(t *testing.T) {
	diag := &fakePrinterDiagnostician{
		diagnosis: PrinterDiagnosis{Target: "ESC WS408", Steps: fiveDiagnosisSteps()},
	}
	b := newBench(t, func(o *benchOptions) { o.printerDiagnosis = diag })

	raw := decodeStatus[map[string]any](t, b.post("/admin/api/printer/diagnosis", `{}`), http.StatusOK)

	if raw["target"] != "ESC WS408" {
		t.Fatalf("target = %v, attendu ESC WS408", raw["target"])
	}
	steps, ok := raw["steps"].([]any)
	if !ok || len(steps) != 5 {
		t.Fatalf("steps = %#v, attendu 5 éléments", raw["steps"])
	}
	first, ok := steps[0].(map[string]any)
	if !ok {
		t.Fatalf("étape 1 illisible : %#v", steps[0])
	}
	for _, key := range []string{"number", "title", "state", "observed", "remedy", "doc"} {
		if _, ok := first[key]; !ok {
			t.Fatalf("l'étape 1 ne porte pas la clé %q : %#v", key, first)
		}
	}
	third, ok := steps[2].(map[string]any)
	if !ok || third["state"] != "unknown" {
		t.Fatalf("état de l'étape 3 = %v, attendu unknown", third["state"])
	}
}

// TestAnAnswerIsJournalledOnce is the Review Focus of the plan : one line per
// parcours, never two, by whichever route the parcours actually stops at.
func TestAnAnswerIsJournalledOnce(t *testing.T) {
	t.Run("une réponse écrit une ligne warn", func(t *testing.T) {
		diag := &fakePrinterDiagnostician{answer: DiagnosisStep{Number: 5, State: "fail"}}
		b := newBench(t, func(o *benchOptions) { o.printerDiagnosis = diag })
		before := len(b.technical.lines)

		answerBody := `{"steps":["pass","pass","unknown","pass"],"printed":false,` +
			`"seen":"le voyant 1 clignote"}`
		response := b.post("/admin/api/printer/diagnosis/answer", answerBody)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("statut %d, attendu 200 : %s", response.StatusCode, body(t, response))
		} else {
			response.Body.Close()
		}

		lines := b.technical.lines[before:]
		if len(lines) != 1 {
			t.Fatalf("%d ligne(s) journalisée(s), attendu 1 : %+v", len(lines), lines)
		}
		if lines[0].Level != domain.LevelWarn {
			t.Fatalf("niveau %q, attendu %q", lines[0].Level, domain.LevelWarn)
		}
		if !strings.Contains(lines[0].Detail, "3=unknown") {
			t.Fatalf("détail %q ne porte pas 3=unknown", lines[0].Detail)
		}
		if !strings.Contains(lines[0].Detail, "le voyant 1 clignote") {
			t.Fatalf("détail %q ne porte pas le texte du bénévole", lines[0].Detail)
		}
	})

	t.Run("une étape 5 sans réponse n'écrit rien", func(t *testing.T) {
		diag := &fakePrinterDiagnostician{
			diagnosis: PrinterDiagnosis{Target: "x", Steps: fiveDiagnosisSteps()},
		}
		b := newBench(t, func(o *benchOptions) { o.printerDiagnosis = diag })
		before := len(b.technical.lines)

		response := b.post("/admin/api/printer/diagnosis", `{}`)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("statut %d, attendu 200", response.StatusCode)
		}
		response.Body.Close()

		if got := len(b.technical.lines) - before; got != 0 {
			t.Fatalf("%d ligne(s) journalisée(s) pour une étape 5 sans réponse, attendu 0", got)
		}
	})

	t.Run("une étape 5 sans objet écrit une ligne", func(t *testing.T) {
		steps := fiveDiagnosisSteps()
		steps[4].State = "not_applicable"
		diag := &fakePrinterDiagnostician{diagnosis: PrinterDiagnosis{Target: "x", Steps: steps}}
		b := newBench(t, func(o *benchOptions) { o.printerDiagnosis = diag })
		before := len(b.technical.lines)

		response := b.post("/admin/api/printer/diagnosis", `{}`)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("statut %d, attendu 200", response.StatusCode)
		}
		response.Body.Close()

		if got := len(b.technical.lines) - before; got != 1 {
			t.Fatalf("%d ligne(s) journalisée(s) pour une étape 5 sans objet, attendu 1", got)
		}
	})

	// A driver with no printer behind it (preview, Transport "") leaves step 5
	// "not_applicable" with nothing failed: the one line must say the run reached its
	// end, not that it was cut short -- and it is no warning (final review, I2).
	t.Run("un driver sans imprimante écrit une ligne info, pas une interruption", func(t *testing.T) {
		steps := []DiagnosisStep{
			{Number: 1, State: "not_applicable"},
			{Number: 2, State: "not_applicable"},
			{Number: 3, State: "not_applicable"},
			{Number: 4, State: "pass"},
			{Number: 5, State: "not_applicable"},
		}
		diag := &fakePrinterDiagnostician{diagnosis: PrinterDiagnosis{Target: "x", Steps: steps}}
		b := newBench(t, func(o *benchOptions) { o.printerDiagnosis = diag })
		before := len(b.technical.lines)

		response := b.post("/admin/api/printer/diagnosis", `{}`)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("statut %d, attendu 200", response.StatusCode)
		}
		response.Body.Close()

		lines := b.technical.lines[before:]
		if len(lines) != 1 {
			t.Fatalf("%d ligne(s) journalisée(s), attendu 1 : %+v", len(lines), lines)
		}
		if lines[0].Level != domain.LevelInfo {
			t.Errorf("niveau %q, attendu %q : rien n'a échoué", lines[0].Level, domain.LevelInfo)
		}
		if strings.Contains(lines[0].Message, "interrompu") {
			t.Errorf("message %q : rien n'a interrompu ce parcours", lines[0].Message)
		}
	})
}

// TestAnAnswerIsRefusedWhenMalformed : none of the four malformed bodies may reach
// AnswerStep or the journal.
func TestAnAnswerIsRefusedWhenMalformed(t *testing.T) {
	diag := &fakePrinterDiagnostician{answer: DiagnosisStep{Number: 5, State: "pass"}}
	b := newBench(t, func(o *benchOptions) { o.printerDiagnosis = diag })

	cases := map[string]string{
		"trois valeurs seulement": `{"steps":["pass","pass","unknown"],"printed":true,"seen":""}`,
		"une valeur inconnue":     `{"steps":["pass","pass","unknown","ok"],"printed":true,"seen":""}`,
		"201 caractères":          `{"steps":["pass","pass","unknown","pass"],"printed":false,"seen":"` + strings.Repeat("a", 201) + `"}`,
		"un retour à la ligne":    `{"steps":["pass","pass","unknown","pass"],"printed":false,"seen":"a\nb"}`,
	}
	for name, reqBody := range cases {
		t.Run(name, func(t *testing.T) {
			before := len(b.technical.lines)
			response := b.post("/admin/api/printer/diagnosis/answer", reqBody)
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("statut %d, attendu 400 : %s", response.StatusCode, body(t, response))
			} else {
				response.Body.Close()
			}
			if got := len(b.technical.lines) - before; got != 0 {
				t.Fatalf("%d ligne(s) journalisée(s), attendu 0", got)
			}
			if diag.answerCalls != 0 {
				t.Fatalf("AnswerStep appelé %d fois sur une réponse mal formée", diag.answerCalls)
			}
		})
	}
}

// TestTheDiagnosisIsBounded : a diagnostician that never answers must not hold the
// request past deviceBudget (server.go) -- the volunteer is standing in front of the
// screen.
func TestTheDiagnosisIsBounded(t *testing.T) {
	diag := &fakePrinterDiagnostician{block: true}
	b := newBench(t, func(o *benchOptions) { o.printerDiagnosis = diag })

	done := make(chan *http.Response, 1)
	go func() {
		done <- b.post("/admin/api/printer/diagnosis", `{}`)
	}()

	settle(t, func() bool {
		waiters, _ := b.clock.Pending()
		return waiters >= 1
	})
	b.clock.Advance(deviceBudget)

	var response *http.Response
	select {
	case response = <-done:
	case <-time.After(hang):
		t.Fatal("le diagnostic n'a jamais répondu")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("statut %d, attendu 502 : %s", response.StatusCode, body(t, response))
	}
}
