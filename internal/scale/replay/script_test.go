package replay

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseReadsTheFormatThatCaptureWrites(t *testing.T) {
	cases := []struct {
		name    string
		capture string
		cadence time.Duration
		delays  []time.Duration
		raws    []string
	}{
		{
			name:    "des lignes nues prennent la cadence donnée, sauf la première",
			capture: "ST,GS,+  1.236KG\r\nST,GS,+  0.850KG\r\n",
			cadence: 400 * time.Millisecond,
			delays:  []time.Duration{0, 400 * time.Millisecond},
			raws:    []string{"ST,GS,+  1.236KG\r\n", "ST,GS,+  0.850KG\r\n"},
		},
		{
			name:    "les horodatages déclarés font foi",
			capture: "@0 ST,GS,+  1.236KG\n@430 ST,GS,+  0.850KG\n@1000 ST,GS,+  1.240KG\n",
			cadence: 400 * time.Millisecond,
			delays:  []time.Duration{0, 430 * time.Millisecond, 570 * time.Millisecond},
			raws: []string{"ST,GS,+  1.236KG\n", "ST,GS,+  0.850KG\n",
				"ST,GS,+  1.240KG\n"},
		},
		{
			name:    "le premier horodatage non nul est respecté",
			capture: "@250 ST,GS,+  1.236KG\n",
			cadence: 400 * time.Millisecond,
			delays:  []time.Duration{250 * time.Millisecond},
			raws:    []string{"ST,GS,+  1.236KG\n"},
		},
		{
			name:    "une ligne sans horodatage, entre deux qui en portent, prend la cadence",
			capture: "@0 ST,GS,+  1.236KG\nST,GS,+  0.850KG\n@900 ST,GS,+  1.240KG\n",
			cadence: 100 * time.Millisecond,
			delays: []time.Duration{0, 100 * time.Millisecond,
				900 * time.Millisecond},
			raws: []string{"ST,GS,+  1.236KG\n", "ST,GS,+  0.850KG\n",
				"ST,GS,+  1.240KG\n"},
		},
		{
			name:    "commentaires et lignes vides sont ignorés",
			capture: "# openscale capture --port COM8  \n\n   \nST,GS,+  1.236KG\n",
			cadence: 400 * time.Millisecond,
			delays:  []time.Duration{0},
			raws:    []string{"ST,GS,+  1.236KG\n"},
		},
		{
			name: "les blancs d'une trame survivent à l'horodatage",
			// The GRAM right-aligns its number inside a fixed field, so the padding is
			// part of the protocol: eating it would change 1.236 kg into something else.
			capture: "@40 \t 0.996kg\n",
			cadence: 400 * time.Millisecond,
			delays:  []time.Duration{40 * time.Millisecond},
			raws:    []string{"\t 0.996kg\n"},
		},
		{
			name:    "une dernière ligne sans terminateur reste une trame",
			capture: "ST,GS,+  1.236KG",
			cadence: 400 * time.Millisecond,
			delays:  []time.Duration{0},
			raws:    []string{"ST,GS,+  1.236KG"},
		},
		{
			name:    "CR seul, LF seul et CRLF sont tous des terminateurs",
			capture: "ST,GS,+  1.236KG\rST,GS,+  0.850KG\nST,GS,+  1.240KG\r\n",
			cadence: time.Second,
			delays:  []time.Duration{0, time.Second, time.Second},
			raws: []string{"ST,GS,+  1.236KG\r", "ST,GS,+  0.850KG\n",
				"ST,GS,+  1.240KG\r\n"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			script, err := Parse([]byte(c.capture), c.cadence)
			if err != nil {
				t.Fatalf("Parse : %v", err)
			}
			if len(script.Steps) != len(c.delays) {
				t.Fatalf("%d trames, attendu %d", len(script.Steps), len(c.delays))
			}
			for i, step := range script.Steps {
				if step.Delay != c.delays[i] {
					t.Errorf("trame %d : délai %s, attendu %s", i, step.Delay, c.delays[i])
				}
				if string(step.Raw) != c.raws[i] {
					t.Errorf("trame %d : %q, attendu %q — les octets sont rejoués tels quels",
						i, step.Raw, c.raws[i])
				}
			}
		})
	}
}

func TestItReadsWhatOpenscaleCaptureWrites(t *testing.T) {
	// The other half of one contract. cmd/openscale/capture.go writes the living corpus
	// and says so in as many words — « THE FORMAT IS DEFINED BY internal/scale/replay » —
	// so this is the replica of a capture file, header and trailing fragment included.
	// Two readers of the living corpus is the failure worth avoiding: the corpus is only
	// permanent evidence if everything reads it the same way (§15.4).
	capture := "# openscale capture — COM8 · 9600 bauds 8N1 · 2026-07-25T09:30:00Z · durée demandée 30s\n" +
		"# Corpus vivant (§15.4) : une trame par ligne, telle que la balance l'a émise.\n" +
		"@0 ST,GS,+  1.236KG\r\n" +
		"@412 ST,GS,+  0.850KG\r\n" +
		"# fin de capture, trame incomplète et donc NON rejouée : \"ST,GS,+  1.2\"\n"

	script, err := Parse([]byte(capture), DefaultCadence)
	if err != nil {
		t.Fatalf("Parse : %v", err)
	}
	if len(script.Steps) != 2 {
		t.Fatalf("%d trames, attendu 2 : l'en-tête et le fragment final sont des commentaires",
			len(script.Steps))
	}
	if got := script.Steps[1].Delay; got != 412*time.Millisecond {
		t.Errorf("intervalle %s, attendu 412ms — c'est ce que la capture a mesuré", got)
	}
	if got := string(script.Steps[0].Raw); got != "ST,GS,+  1.236KG\r\n" {
		t.Errorf("trame %q : les octets sont rejoués tels que la balance les a émis", got)
	}
}

func TestParseRefusesWhatItCannotReplay(t *testing.T) {
	cases := []struct {
		name    string
		capture string
		wants   string
	}{
		{"capture vide", "", "aucune trame"},
		{"que des commentaires", "# rien\n# ici\n", "aucune trame"},
		{"marqueur sans nombre", "@ ST,GS,+  1.236KG\n", "ligne 1"},
		{"horodatage sans trame", "@400\n", "ligne 1"},
		{"horodatage collé à la trame", "@400ST,GS,+  1.236KG\n", "ligne 1"},
		{"horodatage qui recule", "@800 ST,GS,+  1.236KG\n@400 ST,GS,+  0.850KG\n", "ligne 2"},
		{"horodatage démesuré", "@999999999 ST,GS,+  1.236KG\n", "journée"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.capture), 400*time.Millisecond)
			if err == nil {
				t.Fatal("accepté sans erreur")
			}
			if !strings.Contains(err.Error(), c.wants) {
				t.Errorf("message %q, attendu qu'il contienne %q — c'est un bénévole qui "+
					"le lit", err, c.wants)
			}
		})
	}
}

func TestAnEmptyCaptureIsNamedAsSuch(t *testing.T) {
	// The « Rejouer cette trame » button on a weighing whose frame column is empty: the
	// volunteer is told, instead of watching a station that publishes nothing (§15.4).
	if _, err := Parse(nil, 0); !errors.Is(err, ErrEmptyCapture) {
		t.Fatalf("erreur %v, attendu ErrEmptyCapture", err)
	}
}

func TestTheCadenceFallsBackOnTheDeclaredDefault(t *testing.T) {
	script, err := Parse([]byte("ST,GS,+  1.236KG\nST,GS,+  0.850KG\n"), 0)
	if err != nil {
		t.Fatalf("Parse : %v", err)
	}
	if got := script.Steps[1].Delay; got != DefaultCadence {
		t.Errorf("cadence %s, attendu %s", got, DefaultCadence)
	}
}

func TestThePaceIsTheMedianOfTheDeclaredIntervals(t *testing.T) {
	// The median, as §6.5 gives the rate meter, and for the same reason: the long pause
	// while somebody changed the bag must not move the announced cadence.
	script, err := Parse([]byte(
		"@0 ST,GS,+  1.236KG\n"+
			"@400 ST,GS,+  0.850KG\n"+
			"@800 ST,GS,+  1.240KG\n"+
			"@9000 ST,GS,+  1.236KG\n"+
			"@9400 ST,GS,+  0.000KG\n"), 0)
	if err != nil {
		t.Fatalf("Parse : %v", err)
	}
	if got := script.Pace(); got != 400*time.Millisecond {
		t.Errorf("cadence médiane %s, attendu 400ms malgré le trou de 8,2 s", got)
	}
}

func TestAScriptOfOneFrameDeclaresNoPace(t *testing.T) {
	script, err := Parse([]byte("ST,GS,+  1.236KG\n"), 0)
	if err != nil {
		t.Fatalf("Parse : %v", err)
	}
	if got := script.Pace(); got != 0 {
		t.Errorf("cadence %s, attendu 0 : une seule trame ne déclare aucun intervalle", got)
	}
}

func TestAnEvenNumberOfIntervalsAveragesTheTwoMiddleOnes(t *testing.T) {
	// The same rounding rule as RateMeter.Median, so that a script and the meter that
	// will observe it never disagree.
	script, err := Parse([]byte(
		"@0 ST,GS,+  1.236KG\n@100 ST,GS,+  1.236KG\n@400 ST,GS,+  1.236KG\n"), 0)
	if err != nil {
		t.Fatalf("Parse : %v", err)
	}
	if got := script.Pace(); got != 200*time.Millisecond {
		t.Errorf("cadence médiane %s, attendu 200ms (moyenne de 100ms et 300ms)", got)
	}
}
