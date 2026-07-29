package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"net"
	"strings"
	"time"

	"openscale/internal/domain"
	"openscale/internal/domain/frame"
	"openscale/internal/platform"
	"openscale/internal/printing"
	"openscale/internal/printing/preview"
	"openscale/internal/scale/serial"
	"openscale/internal/station"
	"openscale/internal/station/ports"
	"openscale/internal/web"
)

// This file answers the « what is actually plugged in? » questions of the expert screens
// (§14.4). Every one of them is platform-specific, which is why none of them lives in
// internal/web.

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

// adminHardware is everything the administration screens ask of the machine itself.
type adminHardware struct {
	clock ports.Clock
	// hub is read for the label in flight and written for a replayed frame. It is the
	// station's own loop: nothing here decides anything about a weighing.
	hub *station.Hub
	// registries is what this binary actually carries, which is what a detection names
	// when frames decode: adding a scale model changes the sentence with no edit here.
	registries domain.Registries
	// technical is where an administrative action is recorded — a replayed frame is one.
	technical ports.TechnicalLog
	// config reports the configuration IN FORCE, because a detection listens with the
	// link settings THIS station declares and not only with those of the parc — a scale
	// that is not at 9600 bauds would otherwise stay undetectable.
	//
	// It is a function and not h.hub.Config() on purpose: Hub.Config dereferences the
	// station's own configuration, so a test building this struct without a hub would
	// panic. nil therefore means « nothing declared yet », which is exactly the case of
	// a station being installed — the very moment this route is used.
	config func() domain.Config

	// open opens a serial port. nil means the real one, so the production path needs no
	// wiring — and a test drives the detection through a stream it hands back, exactly as
	// `openscale capture` does (§9.1).
	open serial.Opener
	// dial and subnets are the two seams of the network scan, for the same reason.
	dial    func(ctx context.Context, address string) (net.Conn, error)
	subnets func() ([]net.IP, error)
}

// Ports enumerates the serial ports of this machine, with the USB description that makes
// one recognisable (§14.4).
func (h adminHardware) Ports(ctx context.Context) ([]web.PortInfo, error) {
	found, err := platform.SerialPorts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]web.PortInfo, 0, len(found))
	for _, port := range found {
		out = append(out, web.PortInfo{
			Name: port.Name, Description: port.Description, VID: port.VID, PID: port.PID,
		})
	}
	return out, nil
}

// Printers enumerates the print destinations the platform knows about.
func (h adminHardware) Printers(ctx context.Context) ([]web.PrinterInfo, error) {
	queues, err := platform.PrintQueues(ctx)
	if err != nil {
		return nil, err
	}
	return printersOf(queues), nil
}

// DiscoverPrinters scans the local /24 for something listening on the raw print port.
//
// What it finds is a CANDIDATE and not a printer, and the wording says so: a host that
// accepts a connection on 9100 may be a proxy or a switch. The operator picks from the
// list; nothing here writes an address into a configuration.
func (h adminHardware) DiscoverPrinters(ctx context.Context) ([]web.PrinterInfo, error) {
	found, err := platform.DiscoverPrinters(ctx, platform.DiscoverOptions{
		Clock: h.clock, Budget: platform.DiscoverBudget,
		Dial: h.dial, Subnets: h.subnets,
	})
	if err != nil {
		return nil, err
	}
	return printersOf(found), nil
}

// printersOf converts what the platform enumerated.
func printersOf(queues []platform.PrintQueue) []web.PrinterInfo {
	out := make([]web.PrinterInfo, 0, len(queues))
	for _, queue := range queues {
		out = append(out, web.PrinterInfo{
			Name: queue.Name, Detail: queue.Detail, Default: queue.Default,
		})
	}
	return out
}

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
	frames, measurements, read, err := h.listen(ctx, port, detectWindow)
	if err != nil {
		return web.ScaleDetection{}, err
	}

	report := web.ScaleDetection{
		Port: port, ValidCount: len(measurements), Frames: frames,
	}
	switch {
	case len(measurements) > 0:
		report.Driver = h.firstScaleType()
		report.Message = fmt.Sprintf("%s : %d trame(s) valide(s) en %s — %s.",
			port, len(measurements), detectWindow, h.scaleModels())
	case read > 0:
		// Bytes, but no frame: something is talking on that port and it is not a scale
		// this binary understands. That is a different remedy — bitrate, or another
		// device on the same cable — and it must not be reported as silence.
		report.Message = fmt.Sprintf("%s : %d octet(s) reçus, aucune trame reconnue. "+
			"Vérifiez la vitesse de la liaison, ou l'appareil branché sur ce port.", port, read)
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
	if d <= 0 || d > captureCeiling {
		d = detectWindow
	}
	frames, _, _, err := h.listen(ctx, port, d)
	return frames, err
}

// listen opens the port, reads for one window and reports the raw frames and what they
// decoded to.
//
// It NEVER RECONNECTS, exactly like `openscale capture` and for the same reason: a
// cadence measured across an outage describes the outage. A link that drops ends the
// listening, and everything read up to then is still reported — the frames ARE the
// diagnosis, and a lost cable is not a failure of this function.
func (h adminHardware) listen(ctx context.Context, port string, window time.Duration) (
	[]string, []domain.Measurement, int, error) {
	link, err := h.linkFor(port)
	if err != nil {
		return nil, nil, 0, err
	}
	stream, err := link.Open(link)
	if err != nil {
		// Every reason the LINK itself could be unusable has already been named by linkFor,
		// with the key of scale.options to correct, so what is left here comes from the
		// system — and that is what makes the two causes below the only two. Whoever adds a
		// check upstream of this call must name its own refusal there, or a settings mistake
		// will once again reach a volunteer as a port that somebody else is holding.
		return nil, nil, 0, fmt.Errorf("le port %s n'a pas pu être ouvert : %w. Deux causes "+
			"possibles : un autre programme le tient — la balance de ce poste en premier, "+
			"un port série est EXCLUSIF sous Windows — ou bien ce port n'existe plus sur "+
			"cette machine", port, err)
	}
	defer stream.Close()

	var (
		decoder      = &frame.Accumulator{}
		measurements []domain.Measurement
		frames       []string
		pending      []byte
		read         int
	)
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
			read += n
			now := h.clock.Now()
			measurements = append(measurements, decoder.Feed(buffer[:n], now)...)
			pending, frames = cutFrames(append(pending, buffer[:n]...), frames)
		}
		if readErr != nil {
			break
		}
	}
	return frames, measurements, read, nil
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

// cutFrames splits what has arrived on the frame terminator and keeps the last ones.
//
// It reports the bytes still waiting for their terminator, so that a frame cut across two
// reads is one frame and not two halves — the defect the living corpus was built to
// reproduce.
func cutFrames(pending []byte, frames []string) ([]byte, []string) {
	for {
		end := indexTerminator(pending)
		if end < 0 {
			return pending, frames
		}
		line := trimTerminator(pending[:end+1])
		pending = pending[end+1:]
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		frames = append(frames, string(line))
		if len(frames) > framesKept {
			frames = frames[len(frames)-framesKept:]
		}
	}
}

// firstScaleType is the registry key a detection proposes for scale.type.
//
// It is the first entry of the registry rather than a literal: the two GRAM models share
// one decoder (§9.3), so the frames cannot tell them apart, and the sentence names both.
// What this value does is fill the form with something that WORKS.
func (h adminHardware) firstScaleType() string {
	if len(h.registries.Scales) == 0 {
		return ""
	}
	return h.registries.Scales[0].ID
}

// scaleModels names the models that share the decoder which recognised the frames.
//
// Built from the registry so that adding a model changes this sentence with no edit here,
// which is the point of cut 2 of §5.2.
func (h adminHardware) scaleModels() string {
	labels := make([]string, 0, len(h.registries.Scales))
	for _, descriptor := range h.registries.Scales {
		labels = append(labels, descriptor.Label)
	}
	switch len(labels) {
	case 0:
		return "aucun protocole n'est embarqué dans ce binaire"
	case 1:
		return labels[0]
	}
	return strings.Join(labels, " ou ") + " : même décodeur, le choix se lit sur l'étiquette " +
		"de la balance (§9.3)"
}

// LabelPreview renders the label as a PNG, through the SAME renderer that prints (A2).
//
// One renderer and not two is the whole of decision A2: an aperçu produced by a second
// code path would be a picture of what somebody hoped the printer would do. The offset is
// recomposed into the template on every call, so that a volunteer pressing the ±1 dot
// arrow sees the label move.
func (h adminHardware) LabelPreview(_ context.Context, q web.PreviewQuery) ([]byte, error) {
	cfg := h.hub.Config()
	templates, err := templatesFor(cfg, h.registries)
	if err != nil {
		return nil, err
	}
	name := q.Template
	if name == "" {
		name = cfg.Printer.Template
	}
	template, known := templates[name]
	if !known {
		return nil, fmt.Errorf("gabarit %q inconnu ; gabarits disponibles : %s",
			name, strings.Join(h.registries.TemplateNames(), ", "))
	}

	image, err := h.previewImage(cfg, template, q)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := preview.EncodePNG(&out, image); err != nil {
		return nil, fmt.Errorf("aperçu non encodé : %w", err)
	}
	return out.Bytes(), nil
}

// previewImage draws either the demonstration label or the one the station is holding.
//
// The station's own label is the default because that is what makes the aperçu a
// verification rather than an illustration: after a weighing, the screen shows the very
// label that came out. Demo is what the settings screen asks for while nobody is weighing.
func (h adminHardware) previewImage(cfg domain.Config, template domain.Template,
	q web.PreviewQuery) (*image.Gray, error) {
	if q.Demo {
		rules := cfg.Pricing
		if q.Dual {
			// The two-tier grid of the document, so that an operator sees the crowded case
			// — the one where a field can overflow — without having to configure it first.
			rules = domain.LaCagetteRules()
		}
		image, _, err := renderDemo(template, rules, printing.RenderOptions{})
		return image, err
	}

	snapshot := h.hub.State()
	label := snapshot.Label
	if label == nil {
		label = snapshot.LastLabel
	}
	if label == nil {
		return nil, errors.New("aucune étiquette en cours sur ce poste : demandez l'aperçu de " +
			"démonstration (demo=1), ou pesez un produit")
	}
	return printing.Rasterize(&template, *label, domain.LocaleFrench, printing.RenderOptions{})
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
func (h adminHardware) Replay(ctx context.Context, raw string) error {
	// The terminator is what closes a frame in the grammar of §9.2, and a frame copied
	// from a screen has lost it. Adding it back is not tolerance about the format: it is
	// the one byte a copy-paste cannot carry.
	if !strings.HasSuffix(raw, "\r") && !strings.HasSuffix(raw, "\n") {
		raw += "\r\n"
	}
	decoder := &frame.Accumulator{}
	measurements := decoder.Feed([]byte(raw), h.clock.Now())
	if len(measurements) == 0 {
		return fmt.Errorf("cette trame ne se décode pas : %d resynchronisation(s), aucune mesure. "+
			"C'est la réponse — la balance a émis quelque chose que la grammaire de §9.2 refuse",
			decoder.Resyncs)
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
