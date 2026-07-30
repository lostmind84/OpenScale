package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/scale"
	"openscale/internal/station/ports"
)

// The detection of §14.4 is the sentence « COM8 : 12 trames valides, GRAM XFOC », and it is
// the route that answers « y a-t-il une balance ? » instead of asking the operator.
//
// It is driven through the SAME seam `openscale capture` uses: a serial port cannot be
// opened by `go test`, so the stream is scripted and the clock is injected. What that buys
// is the three answers of the route — frames, bytes but no frame, and silence — exercised
// without a scale, which is the requirement §16.1 puts on every business rule of this
// application.

// TestTheDetectionCountsTheFramesAndNamesTheProtocol is the nominal answer.
func TestTheDetectionCountsTheFramesAndNamesTheProtocol(t *testing.T) {
	clock := fake.NewClock(time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC))
	stream := emitting(clock, 8)
	hardware := adminHardware{
		clock: clock, registries: benchRegistries(), scales: scaleRegistry(), open: stream.opener(),
	}

	report, err := hardware.DetectScale(context.Background(), "COM8")
	if err != nil {
		t.Fatalf("détection : %v", err)
	}
	if report.ValidCount != 8 {
		t.Fatalf("%d trame(s) valide(s), attendu 8", report.ValidCount)
	}
	// The protocol the FRAMES named, and the assertion is on that and not on a literal:
	// what the detection proposes is a driver that RECOGNISED the stream, so any entry of
	// the registry whose decoder answered is a correct answer, and the order the
	// composition root registers them in must not be able to make this test lie.
	if !registered(report.Driver) {
		t.Fatalf("driver proposé %q : la détection doit proposer un protocole qui a reconnu "+
			"les trames, parmi %v", report.Driver, registeredIDs())
	}
	if !strings.Contains(report.Message, "COM8") || !strings.Contains(report.Message, "8 trame") {
		t.Fatalf("le message ne dit pas ce qui a été observé : %q", report.Message)
	}
	if !strings.Contains(report.Message, "GRAM XFOC") {
		t.Fatalf("le message ne nomme pas le protocole reconnu : %q", report.Message)
	}
	if len(report.Frames) != 8 {
		t.Fatalf("%d trame(s) rendue(s), attendu les 8 lues : le visualiseur de §14.4 les "+
			"affiche telles quelles", len(report.Frames))
	}
	if report.Frames[0] != strings.TrimSuffix(nominalFrame, "\r\n") {
		t.Fatalf("la trame rendue est %q, attendu la trame brute sans son terminateur",
			report.Frames[0])
	}
	if stream.closes != 1 {
		t.Fatalf("le port a été fermé %d fois : une détection rend la main sur un port "+
			"EXCLUSIF", stream.closes)
	}
}

// TestBytesWithoutAFrameIsADifferentAnswerFromSilence is the distinction that decides what
// a volunteer does next.
//
// Silence means a cable or a switched-off scale. Bytes that decode to nothing mean something
// IS talking on that port and it is not a scale this binary understands — a bitrate, or
// another device on the same adapter — and reporting the two the same way would send
// somebody to check a cable that is fine.
func TestBytesWithoutAFrameIsADifferentAnswerFromSilence(t *testing.T) {
	clock := fake.NewClock(time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC))
	noise := newScriptedStream(clock, scriptedRead{after: cadence, data: "\x00\xff\x13garbage"})
	hardware := adminHardware{clock: clock, registries: benchRegistries(), scales: scaleRegistry(), open: noise.opener()}

	report, err := hardware.DetectScale(context.Background(), "COM8")
	if err != nil {
		t.Fatalf("détection : %v", err)
	}
	if report.ValidCount != 0 || report.Driver != "" {
		t.Fatalf("du bruit a été pris pour une balance : %+v", report)
	}
	if !strings.Contains(report.Message, "octet") {
		t.Fatalf("le message ne dit pas que des octets sont arrivés : %q", report.Message)
	}

	silent := newScriptedStream(clock)
	hardware.open = silent.opener()
	report, err = hardware.DetectScale(context.Background(), "COM9")
	if err != nil {
		t.Fatalf("détection : %v", err)
	}
	if !strings.Contains(report.Message, "aucun octet") {
		t.Fatalf("le silence doit être nommé comme tel : %q", report.Message)
	}
	if !strings.Contains(report.Message, "câble") {
		t.Fatalf("le message ne dit pas quoi vérifier : %q", report.Message)
	}
}

// TestAPortThatCannotBeOpenedSaysWhyItMightBeTaken is the failure a volunteer really meets
// on the Matériel page: they detect on the port that already works.
//
// A serial port is EXCLUSIVE under Windows, so the refusal has to say so — « accès refusé »
// alone would send somebody hunting for a permission problem that does not exist.
func TestAPortThatCannotBeOpenedSaysWhyItMightBeTaken(t *testing.T) {
	clock := fake.NewClock(time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC))
	hardware := adminHardware{
		clock: clock, registries: benchRegistries(), scales: scaleRegistry(),
		open: refusingOpener(errAccessDenied{}),
	}
	_, err := hardware.DetectScale(context.Background(), "COM8")
	if err == nil {
		t.Fatal("un port qui ne s'ouvre pas a été détecté")
	}
	if !strings.Contains(err.Error(), "COM8") {
		t.Fatalf("le refus ne nomme pas le port : %v", err)
	}
	if !strings.Contains(err.Error(), "EXCLUSIF") {
		t.Fatalf("le refus ne dit pas que le port est peut-être déjà tenu : %v", err)
	}
}

// TestTheDetectionOpensThePortWithUsableLinkSettings is the assertion this bench was
// missing, and the one that would have caught the defect on the first day.
//
// Every other test of this file hands a stream back whatever it is asked for, so none of
// them can see WHAT the detection puts in the link. A link with no bitrate, no character
// size, no parity and no stop bits is refused by the real opener before it reaches the
// device — on every port, on every machine — which made « Détecter automatiquement »
// unable to succeed anywhere.
func TestTheDetectionOpensThePortWithUsableLinkSettings(t *testing.T) {
	clock := fake.NewClock(time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC))
	stream := emitting(clock, 4)
	hardware := adminHardware{clock: clock, registries: benchRegistries(), scales: scaleRegistry(), open: stream.opener()}

	if _, err := hardware.DetectScale(context.Background(), "COM8"); err != nil {
		t.Fatalf("détection : %v", err)
	}
	link := stream.link
	if link.Port != "COM8" {
		t.Fatalf("port ouvert %q, attendu celui qui est sondé", link.Port)
	}
	if link.Baud != 9600 || link.Bits != 8 || link.Parity != "N" || link.Stop != 1 {
		t.Fatalf("liaison ouverte en %d bauds %d%s%d : les réglages n'ont pas été complétés, "+
			"et un vrai port série refuse cette liaison avant même d'essayer",
			link.Baud, link.Bits, link.Parity, link.Stop)
	}
	if link.Clock == nil {
		t.Fatal("la liaison n'emporte pas l'horloge injectée")
	}
}

// TestTheDetectionListensWithTheSettingsThisStationDeclares: the parc defaults are a
// fallback, never the last word.
//
// A station whose scale is not at 9600 bauds would stay undetectable if the detection
// listened at the figure of the parc, and the volunteer would be told the cable is fine
// and the scale is silent. The PORT, on the contrary, is always the one being probed:
// taking it from the configuration would make a scan interrogate the same port N times.
func TestTheDetectionListensWithTheSettingsThisStationDeclares(t *testing.T) {
	clock := fake.NewClock(time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC))
	stream := emitting(clock, 4)
	hardware := adminHardware{
		clock: clock, registries: benchRegistries(), scales: scaleRegistry(), open: stream.opener(),
		config: stationDeclaring(t, map[string]any{
			"port": "COM3", "baud": 19200, "parity": "E",
		}),
	}

	if _, err := hardware.DetectScale(context.Background(), "COM8"); err != nil {
		t.Fatalf("détection : %v", err)
	}
	if stream.link.Baud != 19200 || stream.link.Parity != "E" {
		t.Fatalf("liaison ouverte en %d bauds, parité %q : ce poste déclare 19200 et E",
			stream.link.Baud, stream.link.Parity)
	}
	if stream.link.Port != "COM3" && stream.link.Port != "COM8" {
		t.Fatalf("port ouvert %q, inattendu", stream.link.Port)
	}
	if stream.link.Port == "COM3" {
		t.Fatal("la détection a écouté le port de la configuration : un balayage " +
			"interrogerait alors N fois le même port au lieu des N ports de la machine")
	}
}

// TestAPortRefusedByItsSettingsIsNotReportedAsTaken is the counterpart of
// TestAPortThatCannotBeOpenedSaysWhyItMightBeTaken, and it is the sentence a volunteer
// reads on the telephone.
//
// « Le port COM7 est déjà utilisé » on a port nothing is holding sends somebody hunting
// for a process that does not exist. A refusal that comes from the settings of this
// station must name the setting, and it must not reach the port at all.
func TestAPortRefusedByItsSettingsIsNotReportedAsTaken(t *testing.T) {
	clock := fake.NewClock(time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC))
	stream := emitting(clock, 4)
	hardware := adminHardware{
		clock: clock, registries: benchRegistries(), scales: scaleRegistry(), open: stream.opener(),
		config: stationDeclaring(t, map[string]any{"port": "COM3", "parity": "K"}),
	}

	_, err := hardware.DetectScale(context.Background(), "COM8")
	if err == nil {
		t.Fatal("une parité inexistante a été acceptée par la détection")
	}
	if !strings.Contains(err.Error(), "parity") {
		t.Fatalf("le refus ne nomme pas le réglage à corriger : %v", err)
	}
	for _, accusation := range []string{"déjà utilisé", "EXCLUSIF", "autre programme"} {
		if strings.Contains(err.Error(), accusation) {
			t.Fatalf("un refus de réglages accuse un port occupé (%q) : %v", accusation, err)
		}
	}
	if stream.opens != 0 {
		t.Fatalf("le port a été ouvert %d fois : des réglages inutilisables se refusent "+
			"avant de toucher le matériel", stream.opens)
	}
}

// stationDeclaring is a configuration reader answering with these scale.options, the way
// the config.json of a station carries them (§11.2).
func stationDeclaring(t *testing.T, pairs map[string]any) func() domain.Config {
	t.Helper()
	options := domain.DriverOptions{}
	for key, value := range pairs {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("encodage de scale.options.%s : %v", key, err)
		}
		options[key] = encoded
	}
	cfg := domain.Config{Scale: domain.ScaleConfig{Present: true, Options: options}}
	return func() domain.Config { return cfg }
}

// TestTheDetectionRefusesAnEmptyPortByName keeps the message useful: a form submitted with
// an empty field gets told what to type.
func TestTheDetectionRefusesAnEmptyPortByName(t *testing.T) {
	hardware := adminHardware{clock: fake.NewClock(time.Now()), registries: benchRegistries(), scales: scaleRegistry()}
	if _, err := hardware.DetectScale(context.Background(), "  "); err == nil {
		t.Fatal("une détection sans port a été acceptée")
	}
	if _, err := hardware.CaptureFrames(context.Background(), "", time.Second); err == nil {
		t.Fatal("une capture sans port a été acceptée")
	}
}

// TestAFrameCutAcrossTwoReadsIsOneFrame is the defect the living corpus exists to
// reproduce.
//
// A 4 KiB read on a 9600 bps link really does hand back half a frame, and the legacy corpus
// is full of the halves: the accumulator has to keep what is waiting for its terminator. The
// frame is the FIRST one of the capture on purpose — put at the end of a long run it would be
// dropped by the twenty-frame ceiling and this assertion would prove nothing.
func TestAFrameCutAcrossTwoReadsIsOneFrame(t *testing.T) {
	clock := fake.NewClock(time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC))
	stream := newScriptedStream(clock,
		scriptedRead{after: 100 * time.Millisecond, data: "ST,GS,+  1.2"},
		scriptedRead{after: 100 * time.Millisecond, data: "36KG\r\n"},
		scriptedRead{after: 100 * time.Millisecond, data: nominalFrame},
	)
	hardware := adminHardware{clock: clock, registries: benchRegistries(), scales: scaleRegistry(), open: stream.opener()}

	frames, err := hardware.CaptureFrames(context.Background(), "COM8", time.Second)
	if err != nil {
		t.Fatalf("capture : %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("%d trame(s) rendue(s), attendu 2 : une trame coupée entre deux lectures est "+
			"UNE trame, jamais deux moitiés — %q", len(frames), frames)
	}
	for rank, frame := range frames {
		if frame != strings.TrimSuffix(nominalFrame, "\r\n") {
			t.Fatalf("trame n° %d = %q, attendu la trame recollée", rank+1, frame)
		}
	}
}

// TestTheFrameViewerCutsWithTheProtocolAndNotOnALineEnding is the defect of 29/07 found
// one storey up, on the Matériel page.
//
// A GRAM XFOC PLUS delimits its transmissions with control codes and sends NO CR or LF at
// all. The viewer of §14.4 cut what it read on a line ending of its own, so on the very
// hardware of the L0 bench it showed an EMPTY list of raw frames while the line above it
// announced twelve valid ones — and a support call then has the count and not the bytes,
// which is the one thing it needed. Where a frame ends is now asked of the decoder.
func TestTheFrameViewerCutsWithTheProtocolAndNotOnALineEnding(t *testing.T) {
	clock := fake.NewClock(time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC))
	stream := newScriptedStream(clock,
		scriptedRead{after: cadence, data: framedPlus("S  1,236KG")},
		scriptedRead{after: cadence, data: framedPlus("U- 0,432KG")},
	)
	hardware := adminHardware{
		clock: clock, registries: benchRegistries(), scales: scaleRegistry(),
		open: stream.opener(),
	}

	report, err := hardware.DetectScale(context.Background(), "COM7")
	if err != nil {
		t.Fatalf("détection : %v", err)
	}
	if report.ValidCount != 2 {
		t.Fatalf("%d trame(s) valide(s), attendu 2 : la grammaire lit le tramage de contrôle",
			report.ValidCount)
	}
	if len(report.Frames) != 2 {
		t.Fatalf("%d trame(s) brute(s) rendue(s) pour %d décodées : le visualiseur découpe sur "+
			"un terminateur que ce protocole n'envoie jamais — %q",
			len(report.Frames), report.ValidCount, report.Frames)
	}
	for rank, shown := range report.Frames {
		if !strings.Contains(shown, "KG") {
			t.Errorf("trame n° %d = %q : ce n'est pas la trame telle qu'elle est arrivée",
				rank+1, shown)
		}
	}
}

// framedPlus wraps a payload in the control framing a GRAM XFOC PLUS really emits, read
// off the L0 bench of 28/07/2026 and frozen in the living corpus:
//
//	SOH STX  payload  XOR  ETX EOT  flags
//
// The checksum is COMPUTED and not written down, so that a test cannot pass on a frame no
// scale would have sent.
func framedPlus(payload string) string {
	var checksum byte
	for i := 0; i < len(payload); i++ {
		checksum ^= payload[i]
	}
	return "\x01\x02" + payload + string(checksum) + "\x03\x04\x00"
}

// TestTheCaptureKeepsOnlyTheLastTwentyFrames is the « visualiseur des 20 dernières trames
// brutes » of §14.4: a scale that babbles must not hand a screen a thousand lines.
func TestTheCaptureKeepsOnlyTheLastTwentyFrames(t *testing.T) {
	clock := fake.NewClock(time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC))
	stream := emitting(clock, 30)
	hardware := adminHardware{clock: clock, registries: benchRegistries(), scales: scaleRegistry(), open: stream.opener()}

	frames, err := hardware.CaptureFrames(context.Background(), "COM8", 20*time.Second)
	if err != nil {
		t.Fatalf("capture : %v", err)
	}
	if len(frames) != framesKept {
		t.Fatalf("%d trame(s) rendue(s) pour trente émises, attendu %d", len(frames), framesKept)
	}
}

// TestACaptureBeyondTheCeilingFallsBackOnTheDetectionWindow keeps an HTTP handler from being
// held for a minute by a number somebody typed.
//
// The half-hour campaign of §21 n° 3 is `openscale capture`, on the command line, where
// nobody is waiting in front of a screen.
func TestACaptureBeyondTheCeilingFallsBackOnTheDetectionWindow(t *testing.T) {
	clock := fake.NewClock(time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC))
	started := clock.Now()
	stream := emitting(clock, 200)
	hardware := adminHardware{clock: clock, registries: benchRegistries(), scales: scaleRegistry(), open: stream.opener()}

	if _, err := hardware.CaptureFrames(context.Background(), "COM8", time.Hour); err != nil {
		t.Fatalf("capture : %v", err)
	}
	if elapsed := clock.Now().Sub(started); elapsed > detectWindow+time.Second {
		t.Fatalf("la capture a écouté %s : une heure demandée retombe sur la fenêtre de "+
			"détection (%s)", elapsed, detectWindow)
	}
}

// TestTheDetectionNamesEveryModelThatRecognisedTheFrames is what keeps the sentence true
// when a model is added: it is built from WHAT ANSWERED, not from the whole registry and
// not from a literal.
//
// The two GRAM entries differ by the name on the sticker and by nothing on the wire (§9.3),
// so both recognise the same stream, the frames cannot tell them apart, and the message
// says so instead of picking one. The day a THIRD protocol is registered with a grammar of
// its own, the sentence must stop naming it — which is exactly what listing the whole
// registry could not do.
func TestTheDetectionNamesEveryModelThatRecognisedTheFrames(t *testing.T) {
	clock := fake.NewClock(time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC))
	stream := emitting(clock, 8)
	hardware := adminHardware{
		clock: clock, registries: benchRegistries(), scales: scaleRegistry(),
		open: stream.opener(),
	}

	report, err := hardware.DetectScale(context.Background(), "COM8")
	if err != nil {
		t.Fatalf("détection : %v", err)
	}
	for _, model := range []string{"GRAM XFOC RS", "GRAM XFOC +"} {
		if !strings.Contains(report.Message, model) {
			t.Fatalf("le message ne nomme pas %s : %q", model, report.Message)
		}
	}
	if !strings.Contains(report.Message, "même décodeur") {
		t.Fatalf("le message ne dit pas pourquoi les deux sont nommés : %q", report.Message)
	}
}

// TestABinaryWithNoDetectableProtocolSaysSoAndOpensNothing is the case the driver is
// ALLOWED to declare: a protocol that cannot be recognised by listening.
//
// The remedy is a sentence and not a silence — « choisissez-le à la main » — and the port
// is never opened, because a serial port is exclusive and taking one for three seconds to
// hand back nothing is how a volunteer concludes the cable is at fault.
func TestABinaryWithNoDetectableProtocolSaysSoAndOpensNothing(t *testing.T) {
	clock := fake.NewClock(time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC))
	stream := emitting(clock, 8)
	registry := scale.NewRegistry()
	registry.Register(undetectableDriver())
	hardware := adminHardware{
		clock: clock, scales: registry, open: stream.opener(),
		registries: domain.Registries{Scales: registry.Descriptors()},
	}

	report, err := hardware.DetectScale(context.Background(), "COM8")
	if err != nil {
		t.Fatalf("détection : %v", err)
	}
	if !strings.Contains(report.Message, "à la main") {
		t.Fatalf("le message ne dit pas quoi faire à la place : %q", report.Message)
	}
	if !strings.Contains(report.Message, "Balance muette") {
		t.Fatalf("le message ne nomme pas le protocole à choisir : %q", report.Message)
	}
	if report.Driver != "" || report.ValidCount != 0 {
		t.Fatalf("un protocole a été proposé sans qu'aucune trame ne soit reconnue : %+v", report)
	}
	if stream.opens != 0 {
		t.Fatalf("le port a été ouvert %d fois alors qu'aucun protocole ne sait s'y "+
			"reconnaître : un port série est EXCLUSIF", stream.opens)
	}

	if _, err := hardware.CaptureFrames(context.Background(), "COM8", time.Second); err == nil {
		t.Fatal("une capture a été rendue alors qu'aucune grammaire ne sait découper une trame")
	}
}

// TestABinaryWithNoProtocolAtAllSaysThat keeps the two « rien » apart: a binary carrying
// no weighing protocol is a different sentence from one whose protocols cannot be
// detected, and the second one names what to choose.
func TestABinaryWithNoProtocolAtAllSaysThat(t *testing.T) {
	hardware := adminHardware{
		clock: fake.NewClock(time.Now()), scales: scale.NewRegistry(),
	}
	report, err := hardware.DetectScale(context.Background(), "COM8")
	if err != nil {
		t.Fatalf("détection : %v", err)
	}
	if !strings.Contains(report.Message, "aucun protocole n'est embarqué dans ce binaire") {
		t.Fatalf("un binaire sans protocole doit le dire : %q", report.Message)
	}
}

// undetectableDriver is a protocol that declares it cannot be recognised by listening —
// EndpointNone — which is the zero value and therefore what a driver says by saying
// nothing.
//
// Its decoder is a stub and NOT the accumulator of the GRAM: this bench exists to prove
// that the detection speaks to whatever grammar a driver hands over, and reaching for the
// one grammar of the parc here would prove the opposite.
func undetectableDriver() scale.Driver {
	return scale.Driver{
		Descriptor: domain.ScaleDescriptor{
			ID: "silent-protocol", Label: "Balance muette", NominalRate: time.Second,
		},
		NewDecoder: func() domain.Decoder { return stubDecoder{} },
		New: func(domain.DriverOptions, ports.Clock, ports.TechnicalLog) (ports.Scale, error) {
			return nil, errors.New("ce driver n'est là que pour déclarer qu'il ne se détecte pas")
		},
	}
}

// stubDecoder is a grammar that recognises nothing at all.
type stubDecoder struct{}

func (stubDecoder) Feed([]byte, time.Time) []domain.Measurement { return nil }
func (stubDecoder) Reset()                                      {}
func (stubDecoder) FrameEnd([]byte) int                         { return -1 }
func (stubDecoder) Resyncs() int                                { return 0 }

// benchRegistries is what this binary really carries, which is what the detection names.
func benchRegistries() domain.Registries {
	return domain.Registries{Scales: scaleRegistry().Descriptors()}
}

// registeredIDs is the set of protocols this binary carries, for an assertion that must
// not depend on the order the composition root registers them in.
func registeredIDs() []string {
	descriptors := scaleRegistry().Descriptors()
	ids := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		ids = append(ids, descriptor.ID)
	}
	return ids
}

// registered reports whether id is one of them.
func registered(id string) bool {
	for _, known := range registeredIDs() {
		if known == id {
			return true
		}
	}
	return false
}

// errAccessDenied is what Windows answers on a port another process holds.
type errAccessDenied struct{}

// Error reports the refusal in the words the operating system uses.
func (errAccessDenied) Error() string { return "Access is denied." }
