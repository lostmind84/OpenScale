package example

import (
	"testing"

	"openscale/internal/scale/corpus"
)

// corpusRoot is where the captures of this package live.
//
// IT IS NOT THE LIVING CORPUS. A real protocol files its captures under
// internal/scale/testdata/frames/<scale.type>/, and cmd/openscale/corpus_test.go then fails
// on any directory no REGISTERED protocol answers to — which is the guard that keeps a
// capture from sitting there being read by nothing. This package is registered nowhere on
// purpose, so its captures stay beside it.
//
// TODO(driver): your own captures go under internal/scale/testdata/frames/<your ID>/, and
// this constant becomes "../testdata/frames".
const corpusRoot = "testdata/frames"

// TestTheCorpusDecodesAsRecorded replays every capture of this protocol through its own
// decoder, the way the serial loop would — three lines, and it is the whole harness.
//
// The expectation is written in the file NAME and not here, which is what lets a
// contributor drop a capture into the directory and have it exercised WITHOUT editing Go:
//
//	nominal-*   every non-comment line must decode, none may be lost
//	degraded-*  legal and illegal lines mixed: nothing may panic, and no line may yield a
//	            mass the grammar could not express
//
// The replay happens at a stride of eighteen bytes, and that figure is not arbitrary: it is
// the CommRead(NumPort, strData, 18, …) of the legacy application, on frames that were
// themselves eighteen bytes long, where one byte of drift cut every following frame in half.
// A decoder that does not care where a read ends is the property the whole corpus defends.
func TestTheCorpusDecodesAsRecorded(t *testing.T) {
	corpus.Check(t, corpusRoot, Driver())
}
