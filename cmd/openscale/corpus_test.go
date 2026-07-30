package main

import (
	"testing"

	"openscale/internal/scale/corpus"
)

// corpusRoot is the LIVING CORPUS of §15.4 seen from the composition root.
const corpusRoot = "../../internal/scale/testdata/frames"

// TestEveryCaptureOfTheCorpusIsClaimedByAProtocolOfThisBinary is the guard that keeps the
// filing honest.
//
// The corpus is filed by protocol — one directory per scale.type — and each driver's own
// test reads its own drawer through its own decoder. That arrangement has exactly one
// failure mode: a capture dropped under a name no driver answers to would sit there being
// read by NOTHING, which is the same silence this whole cut exists to remove. The check
// belongs here because the composition root is the only place that knows every protocol
// the binary was built with.
//
// A capture left loose at the top level is reported too: it belongs to no grammar at all,
// and the message says where to put it.
func TestEveryCaptureOfTheCorpusIsClaimedByAProtocolOfThisBinary(t *testing.T) {
	orphans, err := corpus.Unclaimed(corpusRoot, registeredIDs())
	if err != nil {
		t.Fatalf("lecture du corpus vivant : %v", err)
	}
	if len(orphans) > 0 {
		t.Errorf("le corpus vivant porte %v, qu'aucun protocole de ce binaire ne relit. "+
			"Une capture se dépose dans %s/<scale.type>/ — protocoles enregistrés : %v",
			orphans, corpusRoot, registeredIDs())
	}
}
