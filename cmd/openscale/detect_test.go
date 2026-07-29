package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
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
		clock: clock, registries: benchRegistries(), open: stream.opener(),
	}

	report, err := hardware.DetectScale(context.Background(), "COM8")
	if err != nil {
		t.Fatalf("détection : %v", err)
	}
	if report.ValidCount != 8 {
		t.Fatalf("%d trame(s) valide(s), attendu 8", report.ValidCount)
	}
	if report.Driver != "gram-xfoc-rs" {
		t.Fatalf("driver proposé %q, attendu la première entrée du registre", report.Driver)
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
	hardware := adminHardware{clock: clock, registries: benchRegistries(), open: noise.opener()}

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
		clock: clock, registries: benchRegistries(),
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
	hardware := adminHardware{clock: clock, registries: benchRegistries(), open: stream.opener()}

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
		clock: clock, registries: benchRegistries(), open: stream.opener(),
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
		clock: clock, registries: benchRegistries(), open: stream.opener(),
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
	hardware := adminHardware{clock: fake.NewClock(time.Now()), registries: benchRegistries()}
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
	hardware := adminHardware{clock: clock, registries: benchRegistries(), open: stream.opener()}

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

// TestTheCaptureKeepsOnlyTheLastTwentyFrames is the « visualiseur des 20 dernières trames
// brutes » of §14.4: a scale that babbles must not hand a screen a thousand lines.
func TestTheCaptureKeepsOnlyTheLastTwentyFrames(t *testing.T) {
	clock := fake.NewClock(time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC))
	stream := emitting(clock, 30)
	hardware := adminHardware{clock: clock, registries: benchRegistries(), open: stream.opener()}

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
	hardware := adminHardware{clock: clock, registries: benchRegistries(), open: stream.opener()}

	if _, err := hardware.CaptureFrames(context.Background(), "COM8", time.Hour); err != nil {
		t.Fatalf("capture : %v", err)
	}
	if elapsed := clock.Now().Sub(started); elapsed > detectWindow+time.Second {
		t.Fatalf("la capture a écouté %s : une heure demandée retombe sur la fenêtre de "+
			"détection (%s)", elapsed, detectWindow)
	}
}

// TestTheDetectionNamesEveryModelThatSharesTheDecoder is what keeps the sentence true when
// a model is added: it is built from the REGISTRY, not from a literal.
//
// The two GRAM entries differ by the name on the sticker and by nothing on the wire (§9.3),
// so the frames cannot tell them apart and the message says so instead of picking one.
func TestTheDetectionNamesEveryModelThatSharesTheDecoder(t *testing.T) {
	hardware := adminHardware{registries: benchRegistries()}
	message := hardware.scaleModels()
	for _, model := range []string{"GRAM XFOC RS", "GRAM XFOC +"} {
		if !strings.Contains(message, model) {
			t.Fatalf("le message ne nomme pas %s : %q", model, message)
		}
	}
	if !strings.Contains(message, "même décodeur") {
		t.Fatalf("le message ne dit pas pourquoi les deux sont nommés : %q", message)
	}

	empty := adminHardware{}
	if got := empty.firstScaleType(); got != "" {
		t.Fatalf("un binaire sans protocole propose %q", got)
	}
	if !strings.Contains(empty.scaleModels(), "aucun protocole") {
		t.Fatalf("un binaire sans protocole doit le dire : %q", empty.scaleModels())
	}
}

// benchRegistries is what this binary really carries, which is what the detection names.
func benchRegistries() domain.Registries {
	return domain.Registries{Scales: scaleRegistry().Descriptors()}
}

// errAccessDenied is what Windows answers on a port another process holds.
type errAccessDenied struct{}

// Error reports the refusal in the words the operating system uses.
func (errAccessDenied) Error() string { return "Access is denied." }
