package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"openscale/internal/station/ports"
)

// probeBufferSize is how many bytes one status read may hand back.
//
// The SBPL status frame is NOT documented in the material this project has: §8.5 files
// its decoding under « à qualifier » and shows the raw bytes in hex in the
// administration screen precisely so that somebody can complete it later without
// travelling to the shop. A transport must therefore not interpret it, and must not
// truncate it either — 512 bytes is more than any status reply a device can dribble out
// inside the half-second budget §8.5 allows, at any line rate this parc uses.
const probeBufferSize = 512

// interrogate is level N3 of §8.5, and it is shared by the two bidirectional transports
// because the sequence has nothing device-specific in it: hand the request over — ENQ,
// 0x05 — then read for AT MOST budget and report what came back.
//
// # An empty answer is not an error
//
// « Toute réponse non vide = imprimante vivante » has a contrapositive, and it is not
// « l'imprimante est morte » : it is « on ne sait pas », which is the whole reason
// ports.PrinterUnknown exists. A transport that turned silence into a failure would push
// its caller into announcing a verdict nobody observed, which is the habit important-7
// spends a section removing. So silence comes back as (nil, nil) and the printer driver
// decides.
//
// # One read, and only one
//
// The question is « is anything there? », not « what exactly did it say ». One read
// answers it, and it keeps the probe from turning into a parser for a frame nobody has
// specified yet.
//
// The budget is measured on the INJECTED clock, so the timeout of this probe is exercised
// in microseconds instead of half a second per case (§5.3, §16.4).
func interrogate(ctx context.Context, clk ports.Clock, target string,
	open func() (Duplex, error), request []byte, budget time.Duration) ([]byte, error) {
	switch {
	case len(request) == 0:
		return nil, fmt.Errorf("%s : aucune requête de statut à envoyer ; l'interrogation native "+
			"est un ENQ (0x05) et non une lecture à vide", target)
	case budget <= 0:
		return nil, fmt.Errorf("%s : un délai d'attente de statut doit être positif (lu %s)", target, budget)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	link, err := open()
	if err != nil {
		return nil, err
	}
	var once sync.Once
	shut := func() { once.Do(func() { _ = link.Close() }) }
	defer shut()

	if _, err := writeAll(link, request, target); err != nil {
		return nil, err
	}

	type answer struct {
		p   []byte
		err error
	}
	read := make(chan answer, 1)
	go func() {
		buffer := make([]byte, probeBufferSize)
		n, err := link.Read(buffer)
		if n < 0 {
			n = 0
		}
		read <- answer{p: buffer[:n], err: err}
	}()

	select {
	case got := <-read:
		shut()
		return statusOf(got.p, got.err, target)
	case <-clk.After(budget):
		// Closing is what returns a read parked in the kernel; waiting for the goroutine
		// afterwards is what keeps « nothing is left behind » true. Whatever reached the
		// buffer before the close still counts: a printer that answered late answered.
		shut()
		got := <-read
		return statusOf(got.p, nil, target)
	case <-ctx.Done():
		shut()
		<-read
		return nil, ctx.Err()
	}
}

// statusOf turns one read into the answer §8.5 expects: bytes if any arrived, silence if
// none did, and an error only when the link itself broke.
//
// io.EOF is silence and not a breakage: a device that closed the channel without saying
// anything told us nothing, which is a legitimate outcome of a probe whose decoding is
// still to be qualified.
func statusOf(p []byte, err error, target string) ([]byte, error) {
	if len(p) > 0 {
		return p, nil
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s : lecture du statut : %w", target, err)
	}
	return nil, nil
}
