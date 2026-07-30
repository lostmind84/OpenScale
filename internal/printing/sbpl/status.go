package sbpl

import "openscale/internal/station/ports"

// This file is the STATUS half of the protocol: the byte a station sends to ask a SATO
// printer how it is, and what the answer means.
//
// It sits in this package for the reason Encode does. The frame is SBPL and nothing else
// — the same ENQ, the same eleven bytes and the same fault table serve the `raster`
// driver and the `sbpl` driver of §8.1, which « produce THE SAME BYTES and differ only in
// who carries them to the head ». Held by one of the two, the other would either import
// its neighbour — a driver depending on a driver — or keep a second copy of a table
// measured once on a bench, and a divergence between the two copies would show up as a
// volunteer being told the wrong thing about a printer that had answered correctly.
//
// What this file deliberately does NOT hold is the three levels of §8.5, the Condition
// vocabulary and the rule that combines them: those live in internal/printing/status.go
// and they are GENERIC. A printer language names its own faults; it does not get to
// decide how a station weighs what its levels saw.

// Enquiry is the status request: ENQ, one byte (§8.5, level N3).
//
// ANY non-empty answer means the printer is alive. What more the answer says is read by
// FaultOfStatusFrame, and the raw bytes travel back in PrinterStatus.Raw whatever that
// reading concludes, so that a frame nobody expected can be looked at in hexadecimal on
// the administration screen rather than being lost.
//
// A function and not an exported slice: an exported variable would let any caller rewrite
// the one byte every station of the parc sends.
func Enquiry() []byte { return []byte{0x05} }

// statusFrameLength is the eleven bytes of the answer to ENQ:
//
//	STX  <job id, 2 bytes>  <status, 1 byte>  <labels remaining, 6 digits>  ETX
//
// Measured on the WS408 of the L0 bench, and identical to what the SATO programming
// reference describes: two SPACES for the job id when no <ID> was declared, and six
// ZEROS when nothing is printing.
const statusFrameLength = 11

// StatusFault is one row of the fault table: what to conclude, and what to say.
type StatusFault struct {
	// Health is what a station may conclude. It is never PrinterReady — see
	// FaultOfStatusFrame.
	Health ports.PrinterHealth
	// Reason is FRENCH: it is read by a volunteer on the troubleshooting screen.
	Reason string
}

// statusFaults are the status bytes that name a fault, from the « Return Status of
// Status3 » table of the SATO programming reference. Lower case is where the errors
// live; digits and upper case are the online, waiting and printing states.
//
// PAPER END IS NOT A FAULT, and that is important-9 rather than an oversight: the last
// label did come out, and turning the end of a roll into a red screen once sent a
// customer away holding a valid label and a message telling them to fetch a volunteer —
// so they stuck two on, or weighed again, and the till counted twice (§8.5).
var statusFaults = map[byte]StatusFault{
	'0': {ports.PrinterFaulted, "l'imprimante est hors ligne"},
	'a': {ports.PrinterFaulted, "la mémoire de réception de l'imprimante est saturée"},
	'b': {ports.PrinterFaulted, "la tête de l'imprimante est ouverte"},
	'c': {ports.PrinterConsumable, "plus d'étiquettes : le rouleau est à changer"},
	'd': {ports.PrinterFaulted, "l'imprimante n'a plus de ruban"},
	'e': {ports.PrinterFaulted, "l'imprimante refuse le média ou l'impression"},
	'f': {ports.PrinterFaulted, "bourrage papier, ou le capteur ne trouve plus l'étiquette"},
	'g': {ports.PrinterFaulted, "la tête de l'imprimante est en erreur"},
	'h': {ports.PrinterFaulted, "le capot de l'imprimante est ouvert"},
}

// FaultOfStatusFrame names the fault a status frame reports, if it reports one.
//
// # WHY THIS NAMES FAULTS AND NEVER READINESS
//
// The obvious other half — « the status byte says online, therefore the printer is
// ready » — was tried at the bench on 29/07/2026 and MEASURED FALSE. With the print
// head latched open and the printer showing its error lamp, three consecutive ENQ
// requests came back with the very same byte as on a healthy idle printer: 'A'. The
// reference explains it in passing — the status is described for a printer that is
// PRINTING, « including QTY is not 0, offline and error » — and it forbids the other
// route in as many words: « Please do not send ENQ while sending print data ».
//
// So an idle WS408 answers 'A' whatever its condition, and a health check built on
// that byte would have reported READY over an open head. That is precisely the failure
// this driver refuses (§14.5), and it would have been written in the belief that a
// measurement backed it.
//
// A fault code, on the other hand, is information: nothing but a real condition puts
// one there. Naming it costs nothing and turns « l'imprimante est vivante » into
// « elle n'a plus de papier » on the troubleshooting screen.
func FaultOfStatusFrame(answer []byte) (StatusFault, bool) {
	// 0x02 is the STX openTransmission emits at the other end of the wire: a frame that
	// does not start with it is not one of these eleven bytes, whatever it is.
	if len(answer) < statusFrameLength || answer[0] != 0x02 {
		return StatusFault{}, false
	}
	named, is := statusFaults[answer[3]]
	return named, is
}
