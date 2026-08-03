package main

import (
	"errors"
	"strings"

	"openscale/internal/domain"
	"openscale/internal/scale"
)

// This file answers, for `openscale capture` and `openscale replay`, WHICH grammar a
// stream of bytes is read with: the list a usage line offers, the one used when nobody
// says, and the decoder the registry hands back for it.

// protocolList is the French tail of every sentence that offers a choice of grammar.
func protocolList(r *scale.Registry) string {
	descriptors := r.Descriptors()
	if len(descriptors) == 0 {
		return "aucun protocole n'est embarqué dans ce binaire"
	}
	ids := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		ids = append(ids, descriptor.ID)
	}
	return strings.Join(ids, ", ")
}

// defaultProtocol is the protocol these two diagnostic commands decode with when nobody
// says otherwise: the first the composition root registered.
//
// # Why a default is legitimate HERE and not in the detection
//
// The detection answers « y a-t-il une balance ? » and its answer goes into a
// configuration file: naming the first entry of a registry there is a GUESS presented as
// a finding, and it stops being true the day a second grammar is registered. These two
// commands answer a different question. Somebody is standing in front of the hardware,
// they know which scale it is, and both commands PRINT the protocol they used — in the
// summary and, for a capture, in the header of the file it writes. A default that is
// announced is a convenience; a default that is silent is the defect of 29/07.
//
// An empty registry yields an empty string, and decoderOf turns that into a refusal that
// names the situation rather than a nil decoder three frames deeper.
func defaultProtocol(r *scale.Registry) string {
	descriptors := r.Descriptors()
	if len(descriptors) == 0 {
		return ""
	}
	return descriptors[0].ID
}

// decoderOf resolves the --type flag into a protocol and a decoder of its own.
//
// It returns the ID as well as the decoder because both commands SAY which grammar they
// used: a capture writes it into the file it produces, so that `openscale replay` never
// has to guess, and a replay prints it above the frames it re-displays.
func decoderOf(r *scale.Registry, requested string) (string, domain.Decoder, error) {
	chosen := strings.TrimSpace(requested)
	if chosen == "" {
		chosen = defaultProtocol(r)
	}
	if chosen == "" {
		return "", nil, errors.New("aucun protocole de balance n'est embarqué dans ce binaire : " +
			"il n'y a aucune grammaire pour décoder ces octets")
	}
	decoder, err := r.NewDecoder(chosen)
	if err != nil {
		return "", nil, err
	}
	return chosen, decoder, nil
}
