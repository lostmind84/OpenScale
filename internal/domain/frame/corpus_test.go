package frame

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openscale/internal/domain"
)

// corpusDir is the LIVING CORPUS of §15.4: every frame that ever caused an
// unexplained refusal on a station lands here and becomes a permanent test.
//
// It lives under internal/scale rather than next to this package because it is fed
// by `openscale capture` and by the "Rejouer cette trame" button of the journal,
// both of which belong to the scale driver. This test is what makes the corpus more
// than a folder of files.
const corpusDir = "../../scale/testdata/frames"

// TestLivingCorpusDecodesAsRecorded replays every captured file through the
// accumulator, the way the serial loop would.
//
// The expectation per file is written in the file name, not in this test: a
// contributor adding a capture should not have to edit Go to have it exercised.
//   - nominal-*   : every non-empty line must decode
//   - degraded-*  : the file mixes legal and illegal lines; what matters is that
//     nothing panics and that no line yields a WRONG mass
func TestLivingCorpusDecodesAsRecorded(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(corpusDir, "*.txt"))
	if err != nil {
		t.Fatalf("globbing the corpus: %v", err)
	}
	if len(files) == 0 {
		t.Skip("no capture in the corpus yet — L0 brings the bench and the first 30 minutes of frames")
	}

	for _, path := range files {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading %s: %v", name, err)
			}

			// Replay in 18-byte slices: the stride that lost one frame in two in the
			// legacy application, and therefore the one worth replaying.
			var accumulator Accumulator
			var decoded []domain.Measurement
			for start := 0; start < len(raw); start += 18 {
				end := start + 18
				if end > len(raw) {
					end = len(raw)
				}
				decoded = append(decoded, accumulator.Feed(raw[start:end], t0)...)
			}

			if accumulator.Pending() > MaxBuffer {
				t.Errorf("pending = %d > MaxBuffer", accumulator.Pending())
			}

			// Comments do not count, and the format says so: `openscale capture` writes
			// a header explaining itself and states that a line beginning with # is a
			// comment. A test that counted them would demand a measurement per line of
			// prose, which is how the first real capture arrived here already red.
			lines := 0
			for _, line := range strings.Split(string(raw), "\n") {
				if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
					lines++
				}
			}

			if strings.HasPrefix(name, "nominal-") {
				if len(decoded) != lines {
					t.Errorf("%d frames decoded out of %d lines — a nominal capture must lose none",
						len(decoded), lines)
				}
			}

			// True of every file, nominal or degraded: a decoded mass must be one the
			// grammar can express. A wrong mass is worse than a refusal.
			for i, m := range decoded {
				if m.Gross > 999_999_999 || m.Gross < -999_999_999 {
					t.Errorf("frame %d: %d g is outside the grammar", i, m.Gross)
				}
			}
			t.Logf("%d lines, %d frames decoded, %d resyncs", lines, len(decoded), accumulator.Resyncs)
		})
	}
}

// TestDegradedCorpusNeverInventsAMass is the corpus half of the "we do not guess"
// decision. The degraded file holds the exact artefacts of the 18-byte read; the
// one thing forbidden is turning ".996kg" into a mass.
func TestDegradedCorpusNeverInventsAMass(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(corpusDir, "degraded-18-byte-read.txt"))
	if err != nil {
		t.Skipf("no degraded capture: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		measurement, err := Parse([]byte(line), t0)
		if err != nil {
			continue // refused: the honest answer
		}
		// Accepted lines are legal frames, and each has exactly one right answer.
		want := map[string]domain.Grams{
			" 0.996kg": 996,
			"36KG":     36_000,
		}
		expected, known := want[line]
		if !known {
			t.Errorf("%q was accepted as %d g but is not in the expected set — either the "+
				"grammar widened or the corpus grew without this test being updated",
				line, measurement.Gross)
			continue
		}
		if measurement.Gross != expected {
			t.Errorf("%q = %d g, want %d", line, measurement.Gross, expected)
		}
	}
}
