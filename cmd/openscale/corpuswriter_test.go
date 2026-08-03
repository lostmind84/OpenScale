package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"openscale/internal/fake"
	"openscale/internal/scale/serial"
)

// The file a capture WRITES — the living corpus of §15.4. It has to be self-describing,
// it must never turn a truncated frame into a line somebody replays, it must not split a
// CRLF across two lines, and it must give up loudly rather than silently when it cannot
// write.

// TestCaptureFileIsSelfDescribing: a capture outlives the session that produced it.
// It lands in the living corpus months later, or inside a diagnostic.zip with no
// context at all, and it has to say which port, which link settings and which day it
// came from.
func TestCaptureFileIsSelfDescribing(t *testing.T) {
	clock := fake.NewClock(captureStart)
	_, file, _ := runCaptureOnScript(t, emitting(clock, 3), clock, 5*time.Second, true)

	const wantHeader = "# openscale capture — COM8 · 9600 bauds 8N1 · 2026-07-25T09:30:00Z · durée demandée 5s"
	if !strings.HasPrefix(file, wantHeader) {
		t.Errorf("en-tête inattendu :\n%s", file)
	}
	for _, want := range []string{
		"# Corpus vivant (§15.4)",
		"# Toute ligne commençant par # est un commentaire.",
	} {
		if !strings.Contains(file, want) {
			t.Errorf("le fichier ne se décrit pas : %q absent de\n%s", want, file)
		}
	}
}

// TestCaptureKeepsAnUnterminatedFrameAsAComment: a fragment is not a frame. Writing
// "ST,GS,+  1.2" as a line of the corpus would add a frame no scale ever sent to the
// permanent tests, and turning a truncated frame into a mass is the one thing
// frame.Parse exists to refuse. It is kept, quoted, because it is evidence.
func TestCaptureKeepsAnUnterminatedFrameAsAComment(t *testing.T) {
	clock := fake.NewClock(captureStart)
	stream := newScriptedStream(clock,
		scriptedRead{after: cadence, data: nominalFrame},
		scriptedRead{after: cadence, data: "ST,GS,+  1.2"},
	)
	_, file, path := runCaptureOnScript(t, stream, clock, 5*time.Second, true)

	if !strings.Contains(file, `# fin de capture, trame incomplète et donc NON rejouée : "ST,GS,+  1.2"`) {
		t.Errorf("le reliquat n'a pas été conservé en commentaire :\n%s", file)
	}
	if strings.Contains(file, "@412 ST,GS,+  1.2\n") {
		t.Errorf("le reliquat a été écrit comme une trame :\n%s", file)
	}
	// And replaying it back decodes the one frame that was whole, and only that one.
	var out bytes.Buffer
	if err := runReplay([]string{path, "--quiet"}, &out); err != nil {
		t.Fatalf("runReplay : %v", err)
	}
	requireLine(t, out.String(), "1 trame décodée sur 1 ligne, 0 resynchronisation")
}

// TestCaptureDoesNotSplitACRLFAcrossTwoLines: a terminator delivered in two reads is
// still ONE terminator, exactly as frame.Accumulator treats it. The opposite would
// double the line count of every capture taken on a busy machine.
func TestCaptureDoesNotSplitACRLFAcrossTwoLines(t *testing.T) {
	clock := fake.NewClock(captureStart)
	stream := newScriptedStream(clock,
		scriptedRead{after: cadence, data: "ST,GS,+  1.236KG\r"},
		scriptedRead{after: 2 * time.Millisecond, data: "\nST,GS,+  0.850KG\r\n"},
	)
	screen, file, _ := runCaptureOnScript(t, stream, clock, 5*time.Second, true)

	requireLine(t, screen, "2 trames décodées sur 2 lignes, 0 resynchronisation")
	if got := strings.Count(file, "\n"); got != 9 {
		t.Errorf("%d lignes dans le fichier, 7 de commentaire + 2 de trame attendues :\n%s", got, file)
	}
	if !strings.Contains(file, "@0 ST,GS,+  1.236KG\r\n") {
		t.Errorf("la trame coupée n'a pas été recollée :\n%q", file)
	}
}

// failingWriter is a disk that fills up in the middle of a capture.
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

// TestCorpusWriterGivesUpLoudlyWhenItCannotWrite. A capture that lost frames to a
// full disk and said nothing would produce a corpus file that LOOKS complete, and the
// cadence measured from it would be a fiction -- the exact failure the living corpus
// exists to make impossible.
func TestCorpusWriterGivesUpLoudlyWhenItCannotWrite(t *testing.T) {
	_, decoder := benchProtocol(t)
	writer := &corpusWriter{to: failingWriter{err: errors.New("disque plein")}, cut: decoder}
	if err := writer.feed([]byte(nominalFrame), captureStart); err == nil {
		t.Error("une trame perdue n'a pas été signalée")
	}
	// A fragment waits for its terminator, so feeding it writes nothing; it is finish
	// that has to fail on it.
	if err := writer.feed([]byte("ST,GS,+  1.2"), captureStart); err != nil {
		t.Errorf("une trame incomplète a été écrite avant son terminateur : %v", err)
	}
	if err := writer.finish(); err == nil {
		t.Error("un reliquat perdu n'a pas été signalé")
	}
	if err := writer.header(captureRequest{link: serial.Options{Port: "COM8"}}, captureStart); err == nil {
		t.Error("un en-tête perdu n'a pas été signalé")
	}
}
