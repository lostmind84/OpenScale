package replay

import (
	"os"
	"path/filepath"
	"testing"

	"openscale/internal/domain"
)

// corpusDir is the LIVING CORPUS of §15.4: every frame that ever caused an unexplained
// refusal on a station lands there and becomes a permanent test. This package is the one
// that plays it back — `openscale replay` and the « Rejouer cette trame » button — so
// this test is what makes the corpus more than a folder of files.
const corpusDir = "../testdata/frames"

// TestTheLivingCorpusReplaysThroughTheDriver walks each capture through a whole driver
// life: parsed, paced on the injected clock, decoded, published.
//
// The counts are an ACQUIS and they are frozen here on purpose. The degraded file holds
// the exact artefacts of the 18-byte read of the legacy application: three of its five
// lines are refused, and being refused is the correct answer — ".996kg" could have been
// 0.996, 1.996 or 10.996 kg, and this application does not guess (§9.2). Anything that
// made those three lines decode would be a regression, not a fix.
func TestTheLivingCorpusReplaysThroughTheDriver(t *testing.T) {
	cases := []struct {
		file  string
		lines int
		want  []domain.Grams
	}{
		{
			file:  "nominal-gram-xfoc.txt",
			lines: 7,
			// The seventh is the OL frame: the mass is meaningless, the flag is not, and
			// safeguard rule 1 is what reads it (§6.4).
			want: []domain.Grams{1236, 850, 1240, 1236, -282, 0, 99_999},
		},
		{
			file:  "degraded-18-byte-read.txt",
			lines: 5,
			// " 0.996kg" and "36KG" are legal frames and decode. ".996kg",
			// "ST,GS,+  1.2" and "ST,GS,+  1.236K" are not, and are dropped.
			want: []domain.Grams{996, 36_000},
		},
	}

	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			capture, err := os.ReadFile(filepath.Join(corpusDir, c.file))
			if err != nil {
				t.Fatalf("lecture du corpus : %v", err)
			}

			script, err := Parse(capture, 0)
			if err != nil {
				t.Fatalf("Parse : %v", err)
			}
			if len(script.Steps) != c.lines {
				t.Fatalf("%d enregistrements, attendu %d lignes", len(script.Steps), c.lines)
			}

			s := start(t, Source{Name: c.file, Frames: capture}, nil)
			events := s.drainUntilDone(t)
			s.awaitDone(t)

			var masses []domain.Grams
			for _, event := range events {
				if event.Measurement != nil {
					masses = append(masses, event.Measurement.Gross)
				}
			}
			if len(masses) != len(c.want) {
				t.Fatalf("%d trames décodées sur %d lignes, attendu %d : %v",
					len(masses), c.lines, len(c.want), masses)
			}
			for i, mass := range masses {
				if mass != c.want[i] {
					t.Errorf("trame %d : %d g, attendu %d g", i, mass, c.want[i])
				}
			}
		})
	}
}

func TestTheOverloadFlagSurvivesTheReplay(t *testing.T) {
	// A capture is replayed to explain a refusal, so everything the refusal depends on
	// has to survive the round trip — the OL flag first of all (§6.4).
	capture, err := os.ReadFile(filepath.Join(corpusDir, "nominal-gram-xfoc.txt"))
	if err != nil {
		t.Fatalf("lecture du corpus : %v", err)
	}
	s := start(t, Source{Name: "nominal", Frames: capture}, nil)
	events := s.drainUntilDone(t)
	s.awaitDone(t)

	overloads, unstable := 0, 0
	for _, event := range events {
		if event.Measurement == nil {
			continue
		}
		if event.Measurement.Overload {
			overloads++
		}
		if event.Measurement.Stability == domain.Unstable {
			unstable++
		}
	}
	if overloads != 1 {
		t.Errorf("%d trames en surcharge, attendu 1", overloads)
	}
	if unstable != 1 {
		t.Errorf("%d trames instables, attendu 1", unstable)
	}
}
