// Package serial is the reader loop of every serial scale, written once.
//
// It is 95 % of a serial driver (§9.1): opening the port, reading what has
// arrived, handing the bytes to a decoder, publishing what comes out, and
// reconnecting for as long as the station runs. What a model brings on top of it
// is its descriptor, its link defaults and its domain.Decoder — which is why
// adding a scale is one package and one line in cmd/openscale/drivers.go (§5.2).
//
// The six defects it corrects, each with the figure that names it:
//
//  1. a 4 KiB read buffer, where SetupComm(h, 16, 16) gave the legacy application
//     a queue of SIXTEEN bytes for frames of eighteen;
//  2. reading what is AVAILABLE and accumulating, where it read 18 FIXED bytes and
//     cut a frame in two at every cycle — the ".996kg" and " 0.996kg" of
//     testdata/frames/degraded-18-byte-read.txt are that read's artefact, not a
//     property of the scale;
//  3. "COM10" reachable, where the legacy application built the device path by
//     hand and never wrote the \\.\ prefix that COM10 and above require;
//  4. a blocking read with a timeout, where a Form_Timer polled every 400 ms;
//  5. an exponential backoff FROM THE FIRST ERROR with the status reported at
//     once, where reconnection waited for ONE THOUSAND consecutive errors — about
//     seven minutes of frozen screen;
//  6. an expiry DERIVED from the observed cadence, where `return
//     gPoidsBalanceConnectee` handed back the last value ever seen, ageless.
//
// The sixth is not in this package and that is the point: the loop stamps every
// measurement with the instant it was decoded, on the INJECTED clock, and the Hub
// derives the expiry from the cadence it observes (§6.5). A driver that read the
// real clock would put that computation out of reach of a test.
//
// Testing it without hardware. A serial port cannot be opened in a test, so the
// one thing this package hides behind a seam is the OPENING of the port:
// Options.Open returns an io.ReadCloser, production leaves it nil and gets
// OpenSystemPort, and the tests hand back a stream that yields bytes, errors and
// closes on demand. That is what makes the reconnection, the backoff progression,
// the frame cut between two reads and the blocking close testable at all.
package serial
