package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"openscale/internal/domain"
	"openscale/internal/scale"
	"openscale/internal/scale/serial"
	"openscale/internal/web"
)

// This file is the detection of §14.4: it opens ONE port, listens for a bounded
// window, feeds the same bytes to every protocol this binary carries, and says what
// answered — « COM8 : 12 trames valides, GRAM XFOC ». It is the detection that answers
// « y a-t-il une balance ? », never the operator.

// detectWindow is how long a detection listens on one port: three seconds, the figure
// §11.4 gives the « Appliquer et tester » step.
//
// It is spent on the INJECTED clock, so a test of the detection runs in microseconds and
// a real one takes the three seconds a volunteer is standing there for.
const detectWindow = 3 * time.Second

// captureCeiling bounds what the capture route may ask for.
//
// Sixty seconds: long enough to catch an intermittent cable, short enough that an HTTP
// handler is never held for a minute — the volunteer pressed a button and is watching a
// spinner. The half-hour campaign of §21 n° 3 is `openscale capture`, on the command
// line, where nobody is waiting.
const captureCeiling = 60 * time.Second

// framesKept is how many raw frames a detection reports back.
//
// Twenty, which is the « visualiseur des 20 dernières trames brutes » of §14.4. A
// three-second window at the nominal cadence yields about eight, so the ceiling only
// bites on a scale that babbles — and there, twenty lines is already the diagnosis.
const framesKept = 20

// bytesKept bounds the raw stream a listening window holds on to before it is cut into
// frames.
//
// The cut happens at the END and not read by read, because which protocol's cut applies
// is only known once something has been recognised. Sixty-four kibibytes is more than a
// minute at 9600 bauds — the whole of captureCeiling — so on the hardware of this parc
// nothing is ever dropped; what the bound really covers is a device babbling on a wrong
// bitrate, and there the TAIL is what a diagnosis wants anyway.
const bytesKept = 64 * 1024

// DetectScale opens one port, applies the parser and says what answered — « COM8 :
// 12 trames valides, GRAM XFOC » (§14.4).
//
// # It is the detection that answers « y a-t-il une balance ? », not the operator
//
// That is the sentence §14.4 uses, and it is why this route exists at all: an operator
// choosing a protocol from a drop-down list is guessing, and a station configured on a
// guess fails at the counter. Here the port is opened, the grammar of §9.2 is applied to
// what comes out of it, and the answer is a count.
//
// # A refusal has to say WHICH refusal it is
//
// On Windows a port already held by the scale of this station cannot be opened a second
// time, and « accès refusé » alone would send a volunteer hunting for a permission
// problem that does not exist. That hint is only true of a refusal that came from the
// SYSTEM, though: link settings no port could accept are named — with the key of
// scale.options to correct — before anything is opened, and must never reach the screen
// as a port somebody else is holding.
func (h adminHardware) DetectScale(ctx context.Context, port string) (web.ScaleDetection, error) {
	if strings.TrimSpace(port) == "" {
		return web.ScaleDetection{}, errors.New("indiquez le port à écouter : COM8, /dev/balance-serial")
	}
	candidates := h.serialCandidates()
	if len(candidates) == 0 {
		// Nothing to recognise WITH, so nothing is opened: a serial port is exclusive, and
		// holding one for three seconds to hand back silence is the answer that sends a
		// volunteer looking for a cable. The screen says what to do instead.
		return web.ScaleDetection{Port: port, Message: h.nothingDetectable(port)}, nil
	}

	reading, err := h.listen(ctx, port, detectWindow, candidates)
	if err != nil {
		return web.ScaleDetection{}, err
	}
	recognised := reading.recognised()

	report := web.ScaleDetection{
		Port: port, ValidCount: reading.validCount(), Frames: reading.frames(),
	}
	switch {
	case len(recognised) > 0:
		// The protocol the FRAMES named, never the first entry of a registry. What goes
		// into the form is the driver that recognised what came out of the cable, and the
		// sentence names every model that recognised the same stream — which is what the
		// two GRAM entries do, since they share one grammar and differ only by the sticker.
		report.Driver = recognised[0].Descriptor.ID
		report.Message = fmt.Sprintf("%s : %d trame(s) valide(s) en %s — %s.",
			port, report.ValidCount, detectWindow, modelsRecognising(recognised))
	case reading.read > 0:
		// Bytes, but no frame: something is talking on that port and it is not a scale
		// this binary understands. That is a different remedy — bitrate, or another
		// device on the same cable — and it must not be reported as silence.
		report.Message = fmt.Sprintf("%s : %d octet(s) reçus, aucune trame reconnue. "+
			"Vérifiez la vitesse de la liaison, ou l'appareil branché sur ce port.", port, reading.read)
	default:
		report.Message = fmt.Sprintf("%s : aucun octet reçu en %s. La balance est-elle "+
			"allumée, et le câble branché sur ce port ?", port, detectWindow)
	}
	return report, nil
}

// CaptureFrames records the raw frames of one port for a bounded duration, which is what
// a support call needs: the bytes, not our reading of them (§14.4, §15.4).
func (h adminHardware) CaptureFrames(ctx context.Context, port string, d time.Duration) ([]string, error) {
	if strings.TrimSpace(port) == "" {
		return nil, errors.New("indiquez le port à écouter : COM8, /dev/balance-serial")
	}
	candidates := h.serialCandidates()
	if len(candidates) == 0 {
		// Where a frame ENDS is a fact about a protocol, so a binary with no protocol has
		// no way to cut a stream into frames. Saying so is the honest answer; handing back
		// an empty list would look like a silent cable.
		return nil, errors.New(h.nothingDetectable(port))
	}
	if d <= 0 || d > captureCeiling {
		d = detectWindow
	}
	reading, err := h.listen(ctx, port, d, candidates)
	if err != nil {
		return nil, err
	}
	return reading.frames(), nil
}

// serialCandidates is one fresh decoder per protocol that declares it can be recognised
// on a serial port.
//
// THE ENUMERATION IS OURS, THE RECOGNITION IS THEIRS. Which ports this machine has is a
// question for the operating system, answered by Ports in hardware.go; what a frame of a
// given protocol looks like is a question for the driver, and it answers it by handing
// over a decoder. Between the two there is nothing left here that has to change when a
// model is added.
func (h adminHardware) serialCandidates() []scale.Candidate {
	if h.scales == nil {
		return nil
	}
	return h.scales.Candidates(scale.EndpointSerialPort)
}

// nothingDetectable is what the screen reads when no protocol of this binary knows how to
// be recognised by listening to a port.
//
// It is a LEGITIMATE outcome and not a fault: a scale that only speaks when it is polled
// cannot be found by listening, and a driver saying so is more useful than a button whose
// only possible answer is silence (§9.3, ADR-025). The sentence therefore ends on what to
// do — choose the protocol by hand — and names the ones that exist, because « aucun » with
// no list is how a volunteer concludes the machine is broken.
func (h adminHardware) nothingDetectable(port string) string {
	labels := make([]string, 0, len(h.registries.Scales))
	for _, descriptor := range h.registries.Scales {
		labels = append(labels, descriptor.Label)
	}
	if len(labels) == 0 {
		return fmt.Sprintf("%s : aucun protocole n'est embarqué dans ce binaire, il n'y a "+
			"rien à détecter.", port)
	}
	return fmt.Sprintf("%s : aucun protocole de ce binaire ne sait se détecter en écoutant "+
		"un port série. Choisissez-le à la main dans la liste : %s.",
		port, strings.Join(labels, ", "))
}

// portReading is everything one listening window observed on a port.
type portReading struct {
	// read is how many bytes arrived, all of them, which is what tells silence from a
	// device talking a language nobody here speaks.
	read int
	// raw is the tail of the stream, kept verbatim so that it can be cut into frames once
	// it is known WHICH protocol's cut applies. Bounded by bytesKept.
	raw []byte
	// candidates are the protocols that were tried, in registration order, each holding
	// the decoder it was tried with and what that decoder made of the stream.
	candidates []candidateReading
}

// candidateReading is what one protocol made of the stream.
type candidateReading struct {
	scale.Candidate
	// count is how many measurements this protocol's decoder yielded. Zero means it did
	// not recognise the stream, which is an answer about the stream and not about the
	// driver.
	count int
}

// recognised reports the protocols whose decoder yielded at least one measurement, in
// registration order.
//
// SEVERAL is the normal case of this parc and not an ambiguity to resolve: the GRAM XFOC
// RS and the GRAM XFOC + share one grammar, so both recognise the same bytes, and the
// choice between them is read off the sticker (§9.3). Picking one and staying quiet about
// the other is what the screen may not do.
func (r portReading) recognised() []candidateReading {
	var out []candidateReading
	for _, candidate := range r.candidates {
		if candidate.count > 0 {
			out = append(out, candidate)
		}
	}
	return out
}

// validCount is how many frames the detection announces, which is the count of the
// protocol that recognised the most.
func (r portReading) validCount() int {
	best := 0
	for _, candidate := range r.candidates {
		if candidate.count > best {
			best = candidate.count
		}
	}
	return best
}

// frames cuts the stream into the raw frames the viewer of §14.4 shows.
//
// It is cut by the decoder that RECOGNISED, and by the first candidate when none did.
// That second case is a presentation choice and claims nothing: the bytes are shown so
// that somebody can read them out on the telephone, and slicing them on a grammar that
// refused them is still more legible than one line of 3 000 characters.
//
// It asks the decoder rather than searching for CR or LF, and that is the whole point of
// the method being on the decoder: a GRAM XFOC PLUS delimits with control codes and sends
// no line ending at all, so this viewer showed NOTHING on the very hardware of the bench
// while announcing twelve valid frames beside it — the defect that cost the capture of
// 29/07, one storey up.
func (r portReading) frames() []string {
	decoder := r.cutter()
	if decoder == nil {
		return nil
	}
	var frames []string
	for pending := r.raw; len(pending) > 0; {
		end := decoder.FrameEnd(pending)
		if end < 0 {
			break // the rest of this frame never arrived
		}
		line := trimTerminator(pending[:end])
		pending = pending[end:]
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		frames = append(frames, string(line))
		if len(frames) > framesKept {
			frames = frames[len(frames)-framesKept:]
		}
	}
	return frames
}

// cutter is the decoder whose cut the frame viewer shows.
func (r portReading) cutter() domain.Decoder {
	if recognised := r.recognised(); len(recognised) > 0 {
		return recognised[0].Decoder
	}
	if len(r.candidates) > 0 {
		return r.candidates[0].Decoder
	}
	return nil
}

// listen opens the port, reads for one window and reports what every candidate protocol
// made of what came out of it.
//
// It NEVER RECONNECTS, exactly like `openscale capture` and for the same reason: a
// cadence measured across an outage describes the outage. A link that drops ends the
// listening, and everything read up to then is still reported — the frames ARE the
// diagnosis, and a lost cable is not a failure of this function.
//
// Every candidate is fed the SAME bytes, each into its own decoder. Feeding them one
// after another would need the port opened once per protocol, on a link that is exclusive
// and a scale that is not repeating itself.
func (h adminHardware) listen(ctx context.Context, port string, window time.Duration,
	candidates []scale.Candidate) (portReading, error) {
	link, err := h.linkFor(port)
	if err != nil {
		return portReading{}, err
	}
	stream, err := link.Open(link)
	if err != nil {
		// Every reason the LINK itself could be unusable has already been named by linkFor,
		// with the key of scale.options to correct, so what is left here comes from the
		// system — and that is what makes the two causes below the only two. Whoever adds a
		// check upstream of this call must name its own refusal there, or a settings mistake
		// will once again reach a volunteer as a port that somebody else is holding.
		return portReading{}, fmt.Errorf("le port %s n'a pas pu être ouvert : %w. Deux causes "+
			"possibles : un autre programme le tient — la balance de ce poste en premier, "+
			"un port série est EXCLUSIF sous Windows — ou bien ce port n'existe plus sur "+
			"cette machine", port, err)
	}
	defer stream.Close()

	reading := portReading{candidates: make([]candidateReading, 0, len(candidates))}
	for _, candidate := range candidates {
		reading.candidates = append(reading.candidates, candidateReading{Candidate: candidate})
	}

	deadline := h.clock.Now().Add(window)
	buffer := make([]byte, defaultReadBuffer)
	for h.clock.Now().Before(deadline) {
		if ctx.Err() != nil {
			// The browser gave up, or the budget of the handler ran out. What was read is
			// still worth reporting: the caller decides, and a cancelled context is not a
			// reason to throw eight frames away.
			break
		}
		// The Opener contract requires this Read to BLOCK until bytes arrive or its own
		// timeout elapses (internal/scale/serial): a Read returning (0, nil) at once would
		// make this loop spin.
		n, readErr := stream.Read(buffer)
		if n > 0 {
			now := h.clock.Now()
			reading.read += n
			reading.raw = keepTail(append(reading.raw, buffer[:n]...), bytesKept)
			for i := range reading.candidates {
				reading.candidates[i].count += len(reading.candidates[i].Decoder.Feed(buffer[:n], now))
			}
		}
		if readErr != nil {
			break
		}
	}
	return reading, nil
}

// keepTail bounds a growing buffer by dropping from the FRONT.
//
// From the front because the viewer shows the LAST frames: on a device that babbles, what
// is worth reading is what it is saying now.
func keepTail(data []byte, limit int) []byte {
	if len(data) <= limit {
		return data
	}
	return append([]byte(nil), data[len(data)-limit:]...)
}

// linkFor is the link a detection listens on: the settings THIS station declares,
// completed by the defaults of the parc, on the port being probed.
//
// # The completion is written here on purpose
//
// A link assembled field by field carried no bitrate, no character size, no parity and no
// stop bits, and the real opener refuses such a link before it reaches the device: the
// detection could not succeed on any port of any machine, and every port of a scan came
// back accused of being taken. Calling serial.Options.Complete at the place where the
// port is BOUND is what makes that visible to the next reader, and what lets the caller
// tell a refusal of these settings from a refusal of the system.
//
// # The port always wins over the configuration
//
// A scan probes the ports of the machine one after the other. Taking the port from the
// configuration would interrogate the same one N times and report the other N-1 as silent.
func (h adminHardware) linkFor(port string) (serial.Options, error) {
	link, err := h.declaredLink()
	if err != nil {
		return serial.Options{}, fmt.Errorf("les réglages série de ce poste sont refusés, "+
			"corrigez-les avant de détecter : %w", err)
	}
	link.Port = port
	link.Clock = h.clock
	if h.open != nil {
		link.Open = h.open
	}
	return link, nil
}

// declaredLink reads the link settings this station declares and completes them.
//
// A station that has declared nothing yet — the one being installed, which is precisely
// when this route is used — falls back on the defaults of the parc. A station that
// declares 19200 bauds is listened to at 19200: at the figure of the parc its scale would
// answer bytes that decode to nothing, and the screen would send somebody to check a
// cable that is fine.
func (h adminHardware) declaredLink() (serial.Options, error) {
	var declared domain.DriverOptions
	if h.config != nil {
		declared = h.config().Scale.Options
	}
	link, err := serial.ParseOptions(declared)
	if err != nil {
		return serial.Options{}, err
	}
	return link.Complete()
}

// defaultReadBuffer is how many bytes one read may hand back.
//
// The same 4 KiB as the production loop, and NOT the 16 of the legacy SetupComm: a queue
// smaller than one 18-byte frame is one of the two reasons the legacy corpus is full of
// half frames (§9.1).
const defaultReadBuffer = 4096

// modelsRecognising names the models whose decoder recognised the frames that were read.
//
// It is built from WHAT ANSWERED and no longer from the whole registry, and the two are
// the same sentence only as long as one grammar exists. The moment a second protocol is
// registered, « même décodeur » becomes false of the pair — and the honest list is the
// one the bytes themselves drew.
//
// Several models is the normal case here: the GRAM XFOC RS and the GRAM XFOC + are one
// grammar and two stickers, the frames cannot tell them apart, and the sentence says so
// instead of picking one (§9.3).
func modelsRecognising(recognised []candidateReading) string {
	labels := make([]string, 0, len(recognised))
	for _, candidate := range recognised {
		labels = append(labels, candidate.Descriptor.Label)
	}
	if len(labels) == 1 {
		return labels[0]
	}
	return strings.Join(labels, " ou ") + " : même décodeur, le choix se lit sur l'étiquette " +
		"de la balance (§9.3)"
}
