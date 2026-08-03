package main

import (
	"fmt"
	"io"
	"strconv"
	"time"

	"openscale/internal/domain"
)

// This file writes the LIVING CORPUS of §15.4 — one frame per line, exactly the bytes
// the scale sent, each preceded by its delay since the first. THE FORMAT IS DEFINED BY
// internal/scale/replay, which reads it back; this is the other half of that one
// contract.

// corpusWriter writes the LIVING CORPUS format of §15.4:
//
//	# openscale capture — COM8, 2026-07-25
//	@0 ST,GS,+  1.236KG
//	@412 ST,GS,+  0.850KG
//
// One frame per line, exactly the bytes the scale sent, terminator included, optionally
// preceded by "@<ms> " -- the delay since the FIRST frame, separated by ONE space.
//
// THE FORMAT IS DEFINED BY internal/scale/replay, NOT HERE. That package parses it for
// `openscale replay`, for the « Rejouer cette trame » button of the journal and for the
// tests; this writer is the other half of that one contract, and the round trip is
// frozen by a test. Two readers of the living corpus is the failure worth avoiding:
// the corpus is only permanent evidence if everything reads it the same way.
//
// WHY '@' rather than a bare number: the grammar of §9.2 lets a frame begin with a
// status letter, a sign, a blank OR A DIGIT, so a leading number could be a timestamp
// or the start of a frame. '@' can be neither, and needs no rule to tell them apart.
//
// WHY the offset is worth its bytes: without it a replay can only space the frames at
// the NOMINAL cadence, and the "median cadence measured" of the L3 criterion would be
// the nominal rate handed straight back -- the very figure §21 n° 3 exists to replace.
// A file with no timestamp cannot measure a cadence, and replay says so instead of
// printing a plausible number.
//
// The only byte the writer does not reproduce verbatim is a lone CR terminator, which
// becomes CR LF so the file stays line-oriented. No scale of this parc sends one.
type corpusWriter struct {
	to io.Writer
	// cut is the decoder of the captured protocol, asked ONE question: where does the
	// frame at the head of these bytes end. It is the same decoder the summary decodes
	// with, which is the point — a file cut by one grammar and counted by another is how
	// a capture came back announcing 194 frames over a file holding none.
	cut domain.Decoder
	// pending holds the bytes of a frame whose end has not arrived yet.
	pending []byte
	// origin is the instant of the FIRST line, which is t = 0 of the file. Offsets are
	// relative so that a capture is self-contained and two captures are comparable.
	origin time.Time
	// lines counts the frames written.
	lines int
}

// header writes the comment lines that make a capture file self-describing.
//
// Self-describing because it outlives the session that produced it: a file that lands
// in the living corpus in six months has to say which port, which link settings and
// which day it came from, and a support archive (diagnostic.zip) carries it with no
// context at all.
func (w *corpusWriter) header(req captureRequest, start time.Time) error {
	_, err := fmt.Fprintf(w.to,
		"# openscale capture — %s · %d bauds %d%s%d · %s · durée demandée %s\n"+
			"# "+protocolMarker+"%s\n"+
			"# Corpus vivant (§15.4) : une trame par ligne, telle que la balance l'a émise.\n"+
			"# « @<ms> » en tête de ligne porte l'écart en millisecondes depuis la PREMIÈRE\n"+
			"# trame. Sans ce marqueur la ligne est la trame entière — c'est le format des\n"+
			"# fichiers déjà présents dans internal/scale/testdata/frames/.\n"+
			"# Toute ligne commençant par # est un commentaire.\n",
		req.link.Port, req.link.Baud, captureBits, captureParity, captureStop,
		start.UTC().Format(time.RFC3339), req.duration, req.protocol)
	return err
}

// protocolMarker is the header line that names the grammar a capture was cut with.
//
// It exists so that `openscale replay` reads the protocol off the FILE instead of
// guessing: the two commands are one round trip, and a capture that did not say which
// grammar produced it would have to be replayed on the memory of whoever ran it. It is
// written as a comment, so every reader of the format already skips it.
const protocolMarker = "protocole : "

// feed appends the bytes of one read and writes every line the stream now completes.
//
// now is the instant of that read, and it becomes the offset of every frame it
// completes: the resolution of the file is one read, which is exactly the resolution
// the driver itself has.
func (w *corpusWriter) feed(p []byte, now time.Time) error {
	w.pending = append(w.pending, p...)
	for {
		// The DECODER OF THE CAPTURED PROTOCOL and NOT a terminator search of our own: a
		// GRAM XFOC PLUS delimits with control codes and sends no CR or LF at all, so a
		// writer that looked for line endings wrote a file with no frames in it while the
		// summary above it counted 194. Asking the decoder rather than one package's
		// function is what lets this command write the corpus of a protocol whose frames
		// carry no delimiter at all.
		consumed := w.cut.FrameEnd(w.pending)
		if consumed < 0 {
			return nil
		}
		line := w.pending[:consumed]
		w.pending = w.pending[consumed:]
		if err := w.writeLine(line, now); err != nil {
			return err
		}
	}
}

// finish writes whatever the last read left unterminated, as a COMMENT.
//
// As a comment because a fragment is not a frame. Writing "ST,GS,+  1.2" as a line of
// the corpus would add to the permanent tests a frame no scale ever sent, and turning
// a truncated frame into a mass is the one thing frame.Parse exists to refuse. It is
// kept, quoted, because it is evidence: it is precisely the artefact of the 18-byte
// read that fills degraded-18-byte-read.txt.
func (w *corpusWriter) finish() error {
	if len(w.pending) == 0 {
		return nil
	}
	_, err := fmt.Fprintf(w.to, "# fin de capture, trame incomplète et donc NON rejouée : %q\n", w.pending)
	w.pending = nil
	return err
}

// writeLine writes one frame with its offset, adding the single byte that keeps the
// file line-oriented and nothing else.
func (w *corpusWriter) writeLine(line []byte, now time.Time) error {
	if len(trimTerminator(line)) == 0 {
		return nil // a bare terminator carries no frame
	}
	if w.lines == 0 {
		w.origin = now
	}
	w.lines++

	// "@<ms> " then the frame VERBATIM. Exactly one separator, because a frame of this
	// grammar may legitimately begin with a blank -- " 0.996kg" is one of the corpus --
	// and eating a second space would change the mass.
	out := make([]byte, 0, len(line)+16)
	out = append(out, '@')
	out = strconv.AppendInt(out, now.Sub(w.origin).Milliseconds(), 10)
	out = append(out, ' ')
	out = append(out, line...)
	if out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	_, err := w.to.Write(out)
	return err
}

// trimTerminator returns the line without its trailing CR, LF or CRLF.
//
// It is the last piece of line-ending knowledge left in this command, and it is only ever
// applied to a frame the DECODER has already cut: it strips the bytes a text protocol
// ends on so that a corpus line stays line-oriented, and does nothing at all to a
// transmission delimited by control codes. Its companion — a search for the first CR or
// LF, which is how this command used to decide where a frame ENDED — is gone, because
// that decision belongs to the grammar and to nothing else.
func trimTerminator(line []byte) []byte {
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	return line
}
