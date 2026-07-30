package gramxfoc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/domain/frame"
	"openscale/internal/scale/corpus"
)

// corpusRoot is the LIVING CORPUS of §15.4, filed by protocol. This package claims two
// directories of it — one per registry entry — and the shared harness reads each one
// through THIS driver's decoder.
//
// It lives under internal/scale rather than next to the grammar because it is fed by
// `openscale capture` and by the « Rejouer cette trame » button of the journal, both of
// which belong to the scale driver.
const corpusRoot = "../testdata/frames"

// TestTheLivingCorpusDecodesAsRecorded replays every capture of the two GRAM models
// through the accumulator, the way the serial loop would.
//
// The expectation per file is written in the file NAME and not here, so a contributor
// adding a capture does not have to edit Go to have it exercised — and, since the corpus
// is filed by protocol, dropping the capture of ANOTHER scale no longer turns this test
// red. That gesture now lands in that protocol's own directory, read by its own decoder.
func TestTheLivingCorpusDecodesAsRecorded(t *testing.T) {
	for _, driver := range Drivers() {
		corpus.Check(t, corpusRoot, driver)
	}
}

// TestDegradedCorpusNeverInventsAMass is the corpus half of the « we do not guess »
// decision. The degraded file holds the exact artefacts of the 18-byte read; the one
// thing forbidden is turning ".996kg" into a mass.
//
// It stays SPELLED OUT, file by file and value by value, where Check only counts: what
// is frozen here is not that the lines decode but WHAT THEY DECODE TO, and there is
// exactly one right answer per legal line.
func TestDegradedCorpusNeverInventsAMass(t *testing.T) {
	path := filepath.Join(corpusRoot, IDRS, "degraded-18-byte-read.txt")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no degraded capture: %v", err)
	}
	// Accepted lines are legal frames, and each has exactly one right answer.
	want := map[string]domain.Grams{
		" 0.996kg": 996,
		"36KG":     36_000,
	}
	at := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)

	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		measurement, err := frame.Parse([]byte(line), at)
		if err != nil {
			continue // refused: the honest answer
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
