// Package corpus runs the LIVING CORPUS of §15.4 against the protocol that recorded it.
//
// # What the corpus is
//
// Every frame that ever caused an unexplained refusal on a station lands in
// internal/scale/testdata/frames and becomes a permanent test (§15.4). `openscale
// capture` writes files in that format, the « Rejouer cette trame » button of the
// journal feeds it, and this package is what makes the folder more than a folder.
//
// # Why the corpus is filed BY PROTOCOL
//
// It was one flat directory read by one test that ran the GRAM grammar over every file
// in it, at 100 % for anything named nominal-*. The comment on that test invited a
// contributor to drop a capture in « without editing Go » — and dropping the capture of
// any other scale did exactly that and turned the suite red, because a GRAM accumulator
// recognises nothing in another protocol's frames. THE GESTURE THE FILE ENCOURAGED WAS
// THE ONE THAT BROKE IT.
//
// A capture is now filed under the ID of the protocol that produced it:
//
//	internal/scale/testdata/frames/<scale.type>/nominal-….txt
//
// and Check runs it through THAT driver's own decoder. The promise holds literally: a
// contributor drops a file into the directory of their protocol and it is exercised, no
// Go edited. A capture of a protocol this binary does not carry lands in a directory no
// driver claims, and Unclaimed says so out loud instead of letting it sit unread.
//
// # The expectation is written in the file NAME
//
//   - nominal-*  : every non-empty, non-comment line must decode. None may be lost.
//   - degraded-* : the file mixes legal and illegal lines. Nothing may panic, and no
//     line may yield a mass the grammar could not express — a WRONG mass is worse than
//     a refusal, which is the whole of the « we do not guess » decision.
package corpus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/scale"
	"openscale/internal/scale/conformance"
)

// readStride is how many bytes are handed to the decoder at a time.
//
// EIGHTEEN, and it is not an arbitrary number: it is the CommRead(NumPort, strData, 18,
// …) of the legacy application, on frames that are themselves eighteen bytes long, where
// one byte of drift cut every following frame in half. Replaying the corpus at that
// stride is what proves a decoder does not care where a read ends — the property the
// whole living corpus exists to defend (§9.1, §18).
const readStride = 18

// decodedAt is the instant every frame of a corpus file is decoded at.
//
// A constant, because a decoder reads no clock: the instant is received, and a corpus
// replayed twice must decode to the same thing forever.
var decodedAt = time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)

// Check replays every capture filed under the protocol d names, as the serial loop
// would.
//
// root is the corpus root — internal/scale/testdata/frames — and the directory actually
// read is root/<driver ID>. A protocol with no capture yet is SKIPPED and not failed:
// the corpus grows from the bench, and a driver that has never met its hardware has
// nothing to show.
//
// One FRESH decoder per file, from the driver's own factory: a file must not be able to
// pass because the previous one left the right bytes pending.
func Check(t *testing.T, root string, d scale.Driver) {
	t.Helper()
	dir := filepath.Join(root, d.Descriptor.ID)
	files, err := filepath.Glob(filepath.Join(dir, "*.txt"))
	if err != nil {
		t.Fatalf("globbing the corpus of %s: %v", d.Descriptor.ID, err)
	}
	if len(files) == 0 {
		t.Skipf("no capture filed under %s yet — a corpus grows from the bench", dir)
		return
	}

	t.Run(d.Descriptor.ID, func(t *testing.T) {
		for _, path := range files {
			name := filepath.Base(path)
			t.Run(name, func(t *testing.T) {
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("reading %s: %v", name, err)
				}
				checkOneFile(t, name, raw, d.NewDecoder())
			})
		}
	})
}

// checkOneFile holds one capture to what its name promises.
func checkOneFile(t *testing.T, name string, raw []byte, decoder domain.Decoder) {
	t.Helper()

	var decoded []domain.Measurement
	for start := 0; start < len(raw); start += readStride {
		end := min(start+readStride, len(raw))
		decoded = append(decoded, decoder.Feed(raw[start:end], decodedAt)...)
	}

	lines := FrameLines(raw)
	if strings.HasPrefix(name, "nominal-") && len(decoded) != lines {
		t.Errorf("%d frames decoded out of %d lines — a nominal capture must lose none",
			len(decoded), lines)
	}

	// True of every file, nominal or degraded: a decoded mass must be one the grammar
	// can express. A mass no frame could have carried means the decoder invented digits,
	// and the barcode carries five of them.
	for i, m := range decoded {
		if m.Gross > conformance.MaxExpressibleGrams || m.Gross < -conformance.MaxExpressibleGrams {
			t.Errorf("frame %d: %d g is outside ±%d g, everything the grammar can express",
				i, m.Gross, conformance.MaxExpressibleGrams)
		}
	}
	t.Logf("%d lines, %d frames decoded", lines, len(decoded))
}

// FrameLines counts the lines of a capture that carry a frame.
//
// Comments do not count, and the format says so: `openscale capture` writes a header
// explaining itself and states that a line beginning with # is a comment. A count that
// included them would demand a measurement per line of prose, which is how the first
// real bench capture arrived in the corpus already red.
//
// It is exported because a driver's own corpus test asserts figures against it — how
// many lines a given file holds is an ACQUIS, and recomputing it by hand in each test is
// how two counts of one file start to disagree.
func FrameLines(raw []byte) int {
	lines := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			lines++
		}
	}
	return lines
}

// Unclaimed lists the sub-directories of the corpus root that no protocol of ids
// answers to.
//
// It is the guard that keeps the filing honest. A capture dropped under a name nobody
// carries — a typo, a protocol removed from the binary, a driver that was never
// registered — would otherwise sit there being read by nothing, which is exactly the
// silence this whole lot exists to remove. The caller is the composition root, because
// it is the only place that knows every protocol the binary was built with.
//
// Loose FILES at the root are reported too, under their own name: the corpus is filed by
// protocol, and a capture left at the top level belongs to no grammar at all.
func Unclaimed(root string, ids []string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	claimed := make(map[string]bool, len(ids))
	for _, id := range ids {
		claimed[id] = true
	}

	var orphans []string
	for _, entry := range entries {
		if !entry.IsDir() {
			if strings.HasSuffix(entry.Name(), ".txt") {
				orphans = append(orphans, entry.Name())
			}
			continue
		}
		if !claimed[entry.Name()] {
			orphans = append(orphans, entry.Name())
		}
	}
	return orphans, nil
}
