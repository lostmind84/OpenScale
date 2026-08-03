package main

import (
	"context"
	"errors"
	"strings"

	"openscale/internal/domain"
)

// This file is the « Rejouer cette trame » button of the journal page (§14.4): one
// recorded frame pushed back through THIS station's own grammar, and handed to the Hub
// exactly as a driver would hand it over — so the station reacts as it really would.

// stationDecoder builds a decoder of the protocol THIS station declares.
//
// A fresh one per call, never a field of this struct: the « Rejouer cette trame » button
// can be pressed twice, and a decoder that kept half of the first frame would complete it
// with the beginning of the second — the fabricated mass the grammar exists to refuse.
//
// A station that declares no protocol is refused by name. It is the case of a station
// running without a scale, and « rejouer une trame » there is a question with no grammar
// to answer it; saying so is more useful than decoding with somebody else's.
func (h adminHardware) stationDecoder() (domain.Decoder, error) {
	if h.scales == nil {
		return nil, errors.New("aucun protocole de balance n'est embarqué dans ce binaire : " +
			"il n'y a pas de grammaire pour lire cette trame")
	}
	var declared string
	if h.config != nil {
		declared = h.config().Scale.Type
	}
	if strings.TrimSpace(declared) == "" {
		return nil, errors.New("ce poste ne déclare aucun protocole de balance (scale.type) : " +
			"une trame se rejoue dans la grammaire du protocole qui l'a émise, jamais dans " +
			"une autre. Renseignez scale.type sur la page Matériel")
	}
	decoder, err := h.scales.NewDecoder(declared)
	if err != nil {
		return nil, err
	}
	return decoder, nil
}

// Replay pushes one recorded frame back through the decoder (§14.4, page Journal).
//
// # What it is for
//
// A frame that caused an unexplained refusal becomes a permanent test, without a trip to
// the shop and without a scale (§15.4). It goes through the SAME grammar the driver uses
// — there is one — so « ça se décode » here means « ça se décode en service ».
//
// # What it deliberately does
//
// The decoded measurement is handed to the Hub exactly as a driver would hand it over, so
// the station reacts as it really would: the safeguards run, the banner appears, the state
// moves. A decoder called in isolation would answer the easy half of the question.
//
// # It decodes with THIS station's protocol
//
// The frame comes from the journal of this station, so the grammar that has to read it is
// the one scale.type names — not whichever the registry holds first. A frame replayed
// through the wrong grammar decodes to nothing and says « la balance a émis quelque chose
// que la grammaire refuse », which would be a lie about the scale and an invitation to go
// and look at it.
func (h adminHardware) Replay(ctx context.Context, raw string) error {
	decoder, err := h.stationDecoder()
	if err != nil {
		return err
	}
	// A frame copied from a screen has lost whatever closed it. Adding a terminator back
	// is not tolerance about the format: it is the byte a copy-paste cannot carry, and it
	// is added ONLY when the protocol says the frame is still incomplete — a transmission
	// that closes on its own control codes needs nothing and must not be padded.
	if decoder.FrameEnd([]byte(raw)) < 0 {
		raw += "\r\n"
	}
	measurements := decoder.Feed([]byte(raw), h.clock.Now())
	if len(measurements) == 0 {
		return errors.New("cette trame ne se décode pas : aucune mesure. C'est la réponse — " +
			"la balance a émis quelque chose que la grammaire de ce protocole refuse")
	}

	h.technical.Technical(domain.LevelInfo, "scale", "",
		"Trame rejouée depuis le journal.", strings.TrimSpace(raw))
	for _, measurement := range measurements {
		select {
		case h.hub.Measurements() <- domain.ScaleEvent{
			Status: domain.StatusConnected, Measurement: &measurement}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
