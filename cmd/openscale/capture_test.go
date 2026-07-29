package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openscale/internal/fake"
	"openscale/internal/scale/serial"
)

// captureStart is the instant every capture test begins at. A fixed one, because the
// clock is INJECTED and nothing here has any business reading the real one.
var captureStart = time.Date(2026, 7, 25, 9, 30, 0, 0, time.UTC)

// nominalFrame and unstableFrame are the reference frames of the corpus. Eighteen
// bytes each, terminator included -- the length that made the 18-byte read of the
// legacy application look like it worked.
const (
	nominalFrame  = "ST,GS,+  1.236KG\r\n"
	unstableFrame = "US,GS,+  1.240KG\r\n"
	// cadence is the interval the scripted scale emits at. It is NOT 400 ms on purpose:
	// 400 ms is the figure this whole pair of commands exists to stop repeating, and a
	// test that used it could not tell a measurement from a default.
	cadence = 412 * time.Millisecond
)

// --- the port ---------------------------------------------------------------------

// scriptedRead is one answer of a scripted port: how long the read took, what it
// hands back, and whether it fails.
type scriptedRead struct {
	after time.Duration
	data  string
	err   error
}

// scriptedStream is the io.ReadCloser a test hands back instead of a serial port.
//
// Every read ADVANCES THE INJECTED CLOCK by the delay the script gives it, which is
// what lets a thirty-minute capture be exercised in microseconds and without a single
// time.Sleep: the instants the capture records are the ones the script decided, and
// the cadence it measures is the one the script emitted at.
type scriptedStream struct {
	clock  *fake.Clock
	script []scriptedRead
	at     int
	closes int
	// silence is what the stream does once the script runs dry: it comes back with no
	// byte and no error, which is what a real port does between two frames, and it is
	// what lets the capture reach its deadline.
	silence time.Duration
	// endErr, when set, is what the port answers instead of staying silent -- a cable
	// pulled in the middle of a measurement campaign.
	endErr error
	// link is the last set of options the opener was handed, and opens counts how many
	// times it was called at all.
	//
	// A double that IGNORED its options cannot tell a caller which built a usable link
	// from one which handed over a struct with no bitrate, no parity and no stop bits --
	// and a real port refuses the second before it touches the device. Recording them is
	// what makes that assertion possible; internal/scale/gramxfoc does the same with the
	// port name.
	link  serial.Options
	opens int
}

// newScriptedStream returns a port that answers these reads, in order, and then goes
// quiet one read timeout at a time.
func newScriptedStream(clock *fake.Clock, script ...scriptedRead) *scriptedStream {
	return &scriptedStream{clock: clock, script: script, silence: time.Second}
}

// emitting returns a port that sends count frames at the nominal cadence of the
// script, marking the frames at the given ranks unstable.
func emitting(clock *fake.Clock, count int, unstableRanks ...int) *scriptedStream {
	unstable := make(map[int]bool, len(unstableRanks))
	for _, rank := range unstableRanks {
		unstable[rank] = true
	}
	script := make([]scriptedRead, 0, count)
	for rank := 1; rank <= count; rank++ {
		frame := nominalFrame
		if unstable[rank] {
			frame = unstableFrame
		}
		script = append(script, scriptedRead{after: cadence, data: frame})
	}
	return newScriptedStream(clock, script...)
}

func (s *scriptedStream) Read(buffer []byte) (int, error) {
	if s.at >= len(s.script) {
		s.clock.Advance(s.silence)
		return 0, s.endErr
	}
	read := s.script[s.at]
	s.at++
	s.clock.Advance(read.after)
	return copy(buffer, read.data), read.err
}

func (s *scriptedStream) Close() error {
	s.closes++
	return nil
}

// opener is the seam capture is injected through: a serial port cannot be opened by
// `go test`, so the whole command is exercised through this.
//
// It KEEPS what it was handed, so that a test can assert on the link a caller built and
// not only on the bytes it read back.
func (s *scriptedStream) opener() serial.Opener {
	return func(o serial.Options) (io.ReadCloser, error) {
		s.link = o
		s.opens++
		return s, nil
	}
}

// refusingOpener is a port that is not there: the commonest failure of the bench, and
// the one a volunteer meets when the adapter is on another COM number.
func refusingOpener(err error) serial.Opener {
	return func(serial.Options) (io.ReadCloser, error) { return nil, err }
}

// --- the bench --------------------------------------------------------------------

// runCaptureOnScript captures a scripted port into a temporary file and returns what
// the operator saw and what the file holds.
func runCaptureOnScript(t *testing.T, stream *scriptedStream, clock *fake.Clock,
	duration time.Duration, quiet bool) (screen string, file string, path string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "frames.txt")
	var out bytes.Buffer
	err := capture(captureRequest{
		link: serial.Options{
			Port: "COM8", Baud: 9600, Clock: clock, Open: stream.opener(),
		},
		duration: duration,
		path:     path,
		quiet:    quiet,
	}, &out)
	if err != nil {
		t.Fatalf("capture : %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("relecture du fichier de trames : %v", err)
	}
	return out.String(), string(raw), path
}

// requireLine fails unless the text carries this line.
//
// Runs of blank space are collapsed on both sides: what these tests freeze is WHAT is
// written and in which order, never the column alignment, which is a presentation
// choice nobody should have to update a test for.
func requireLine(t *testing.T, text, want string) {
	t.Helper()
	want = strings.Join(strings.Fields(want), " ")
	for _, line := range strings.Split(text, "\n") {
		if strings.Join(strings.Fields(line), " ") == want {
			return
		}
	}
	t.Errorf("ligne absente de la sortie :\n  attendue : %s\n  sortie :\n%s", want, text)
}

// --- the demonstration criterion ---------------------------------------------------

// TestDemonstrationCriteriaOfWorkPackageThree freezes, word for word, what §18 asks
// L3 to produce on the real GRAM of the bench:
//
//	« openscale capture --duration 30s produit un fichier ; openscale replay
//	  frames.txt réaffiche les poids décodés avec l'état de figeage ET LA CADENCE
//	  MÉDIANE MESURÉE. Le test « découpage 18 octets » décode 100 trames sur 100 là
//	  où l'existant en perdait une sur deux. »
//
// It is the exit criterion of the work package, so it belongs in a test and not only
// in a terminal somebody ran once. The scale is scripted and the clock is injected:
// what the bench will add is a real cable, not a different assertion.
func TestDemonstrationCriteriaOfWorkPackageThree(t *testing.T) {
	clock := fake.NewClock(captureStart)
	stream := emitting(clock, 12, 5, 6)
	screen, file, path := runCaptureOnScript(t, stream, clock, 30*time.Second, true)

	t.Run("openscale capture --duration 30s produit un fichier", func(t *testing.T) {
		requireLine(t, screen, "12 trames décodées sur 12 lignes, 0 resynchronisation")
		requireLine(t, screen, "cadence observée : médiane 412 ms sur 11 intervalles")
		requireLine(t, screen, "trames stables : 10 sur 12 (83,3 %) · instables : 2 · sans indication : 0")

		// The file is the LIVING CORPUS format: one frame per line, exactly the bytes the
		// scale sent, preceded by the delay since the first frame.
		const wantFirstFrames = "@0 ST,GS,+  1.236KG\r\n@412 ST,GS,+  1.236KG\r\n"
		if !strings.Contains(file, wantFirstFrames) {
			t.Errorf("le fichier ne porte pas les trames attendues :\n%q", file)
		}
	})

	t.Run("openscale replay frames.txt réaffiche poids, figeage et cadence", func(t *testing.T) {
		var out bytes.Buffer
		if err := runReplay([]string{path}, &out); err != nil {
			t.Fatalf("runReplay : %v", err)
		}
		screen := out.String()

		// The decoded weight, its latch state and the median cadence: the three things
		// the criterion names, in the order it names them.
		requireLine(t, screen, "1 +0,000 s 1,236 kg stable non figé (1,236 kg depuis 0 ms)")
		requireLine(t, screen, "2 +0,412 s 1,236 kg stable FIGÉ à 1,236 kg (tenu 412 ms)")
		requireLine(t, screen, "cadence observée : médiane 412 ms sur 11 intervalles")
		requireLine(t, screen, "péremption dérivée : 1236 ms (facteur 3, plancher 1200 ms, plafond 5000 ms)")
	})

	t.Run("le découpage 18 octets décode 100 trames sur 100", func(t *testing.T) {
		var out bytes.Buffer
		if err := runReplay([]string{path, "--read-size", "18", "--quiet"}, &out); err != nil {
			t.Fatalf("runReplay : %v", err)
		}
		requireLine(t, out.String(),
			"Lectures de 18 octets FIXES : 12 trames décodées sur 12 lignes (100,0 %).")
	})
}

// TestCaptureMeasuresWhatUnknownNumberThreeAsksFor: the cadence and the stable ratio
// are not a nicety of the summary, they are the DELIVERABLE. Unknown No 3 blocks the
// freezing of expiry_floor_ms, expiry_ceiling_ms and expiry_factor, and the enabling
// of the blocking stability mode, until a real capture answers them.
func TestCaptureMeasuresWhatUnknownNumberThreeAsksFor(t *testing.T) {
	cases := []struct {
		name          string
		count         int
		unstableRanks []int
		wantCadence   string
		wantStable    string
	}{
		{"une balance saine", 12, []int{5, 6},
			"cadence observée : médiane 412 ms sur 11 intervalles",
			"trames stables : 10 sur 12 (83,3 %) · instables : 2 · sans indication : 0"},

		// Below eight intervals RateMeter refuses to answer, and so does the summary. A
		// median over three frames would be a number, not a measurement.
		{"trop peu d'intervalles", 4, nil,
			"cadence : pas encore mesurable — 3 intervalles observés, il en faut 8",
			"trames stables : 4 sur 4 (100,0 %) · instables : 0 · sans indication : 0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clock := fake.NewClock(captureStart)
			screen, _, _ := runCaptureOnScript(t, emitting(clock, c.count, c.unstableRanks...),
				clock, 30*time.Second, true)
			requireLine(t, screen, c.wantCadence)
			requireLine(t, screen, c.wantStable)
		})
	}
}

// TestCaptureLightsTheAmberSentenceOfTheTroubleshootingScreen: a scale that emits
// every 2.4 s expires its own weight before the next frame arrives, and the station
// goes silent for no visible reason. The condition is the SINGLE one shared by the
// dashboard, `openscale doctor` and failure test 3 bis, and capture must reach the
// same verdict from the same numbers.
func TestCaptureLightsTheAmberSentenceOfTheTroubleshootingScreen(t *testing.T) {
	clock := fake.NewClock(captureStart)
	script := make([]scriptedRead, 0, 12)
	for i := 0; i < 12; i++ {
		script = append(script, scriptedRead{after: 2400 * time.Millisecond, data: nominalFrame})
	}
	screen, _, _ := runCaptureOnScript(t, newScriptedStream(clock, script...), clock, 5*time.Minute, true)

	requireLine(t, screen, "cadence observée : médiane 2400 ms sur 11 intervalles")
	if !strings.Contains(screen, "la balance émet toutes les 2,4 s") ||
		!strings.Contains(screen, "périmé au") || !strings.Contains(screen, "bout de 5 s") {
		t.Errorf("la phrase d'alerte de §15.4 est absente :\n%s", screen)
	}
}

// TestCaptureDumpsHexadecimalAndText: both columns, always. The hexadecimal tells a
// truncated frame from a parity problem, and the text column is what a volunteer
// reads out over the telephone.
func TestCaptureDumpsHexadecimalAndText(t *testing.T) {
	clock := fake.NewClock(captureStart)
	screen, _, _ := runCaptureOnScript(t, emitting(clock, 1), clock, 5*time.Second, false)

	const wantHex = "53 54 2C 47 53 2C 2B 20 20 31 2E 32 33 36 4B 47 "
	if !strings.Contains(screen, wantHex) {
		t.Errorf("le dump hexadécimal est absent :\n%s", screen)
	}
	if !strings.Contains(screen, "|ST,GS,+  1.236KG|") {
		t.Errorf("la colonne texte est absente :\n%s", screen)
	}
	// A CR and an LF are not printable and must not be shown as though they were.
	if !strings.Contains(screen, "0D 0A") || !strings.Contains(screen, "|..|") {
		t.Errorf("les octets non imprimables ne sont pas rendus par un point :\n%s", screen)
	}
	requireLine(t, screen, "1 +0,412 s 1,236 kg stable")
}

// TestCaptureFileIsSelfDescribing: a capture outlives the session that produced it.
// It lands in the living corpus months later, or inside a diagnostic.zip with no
// context at all, and it has to say which port, which link settings and which day it
// came from.
func TestCaptureFileIsSelfDescribing(t *testing.T) {
	clock := fake.NewClock(captureStart)
	_, file, _ := runCaptureOnScript(t, emitting(clock, 3), clock, 5*time.Second, true)

	const wantHeader = "# openscale capture — COM8 · 9600 bauds 8N1 · 2026-07-25T09:30:00Z · durée demandée 5s"
	if !strings.HasPrefix(file, wantHeader) {
		t.Errorf("en-tête inattendu :\n%s", file)
	}
	for _, want := range []string{
		"# Corpus vivant (§15.4)",
		"# Toute ligne commençant par # est un commentaire.",
	} {
		if !strings.Contains(file, want) {
			t.Errorf("le fichier ne se décrit pas : %q absent de\n%s", want, file)
		}
	}
}

// TestCaptureKeepsAnUnterminatedFrameAsAComment: a fragment is not a frame. Writing
// "ST,GS,+  1.2" as a line of the corpus would add a frame no scale ever sent to the
// permanent tests, and turning a truncated frame into a mass is the one thing
// frame.Parse exists to refuse. It is kept, quoted, because it is evidence.
func TestCaptureKeepsAnUnterminatedFrameAsAComment(t *testing.T) {
	clock := fake.NewClock(captureStart)
	stream := newScriptedStream(clock,
		scriptedRead{after: cadence, data: nominalFrame},
		scriptedRead{after: cadence, data: "ST,GS,+  1.2"},
	)
	_, file, path := runCaptureOnScript(t, stream, clock, 5*time.Second, true)

	if !strings.Contains(file, `# fin de capture, trame incomplète et donc NON rejouée : "ST,GS,+  1.2"`) {
		t.Errorf("le reliquat n'a pas été conservé en commentaire :\n%s", file)
	}
	if strings.Contains(file, "@412 ST,GS,+  1.2\n") {
		t.Errorf("le reliquat a été écrit comme une trame :\n%s", file)
	}
	// And replaying it back decodes the one frame that was whole, and only that one.
	var out bytes.Buffer
	if err := runReplay([]string{path, "--quiet"}, &out); err != nil {
		t.Fatalf("runReplay : %v", err)
	}
	requireLine(t, out.String(), "1 trame décodée sur 1 ligne, 0 resynchronisation")
}

// TestCaptureNeverReconnects is the one place capture departs from the production
// loop of internal/scale/serial, and it is deliberate: a cadence measured across an
// outage describes the outage. The link that drops ends the capture, is NAMED, and
// what was read stays in the file.
func TestCaptureNeverReconnects(t *testing.T) {
	clock := fake.NewClock(captureStart)
	stream := emitting(clock, 3)
	stream.endErr = errors.New("le port a disparu")

	path := filepath.Join(t.TempDir(), "frames.txt")
	var out bytes.Buffer
	err := capture(captureRequest{
		link:     serial.Options{Port: "COM8", Baud: 9600, Clock: clock, Open: stream.opener()},
		duration: 30 * time.Minute,
		path:     path,
		quiet:    true,
	}, &out)
	if err != nil {
		t.Fatalf("capture : %v", err)
	}
	if !strings.Contains(out.String(), "la liaison a été perdue avant la fin de la capture") {
		t.Errorf("la cause de l'arrêt n'est pas nommée :\n%s", out.String())
	}
	requireLine(t, out.String(), "3 trames décodées sur 3 lignes, 0 resynchronisation")
	if stream.closes != 1 {
		t.Errorf("le port a été refermé %d fois, une seule est attendue", stream.closes)
	}
	// Thirty minutes were asked for and four seconds elapsed: the capture stopped at the
	// failure instead of retrying for half an hour on a cable nobody plugged back in.
	if elapsed := clock.Now().Sub(captureStart); elapsed > 10*time.Second {
		t.Errorf("la capture a insisté pendant %s après la perte de la liaison", elapsed)
	}
}

// TestCaptureSaysSoWhenNothingCameOutOfTheCable. A silent capture is the commonest
// outcome of a first attempt on the bench, and « 0 trames » alone tells nobody what to
// do next.
func TestCaptureSaysSoWhenNothingCameOutOfTheCable(t *testing.T) {
	clock := fake.NewClock(captureStart)
	screen, file, _ := runCaptureOnScript(t, newScriptedStream(clock), clock, 30*time.Second, true)

	if !strings.Contains(screen, "ATTENTION : aucun octet reçu sur COM8") {
		t.Errorf("le silence n'est pas signalé :\n%s", screen)
	}
	if !strings.Contains(screen, "La balance est-elle allumée") {
		t.Errorf("le silence n'est pas accompagné de ce qu'il faut vérifier :\n%s", screen)
	}
	// The file still exists and still says what was attempted: it is the trace of the
	// attempt, which is what a support archive needs.
	if !strings.HasPrefix(file, "# openscale capture") {
		t.Errorf("le fichier de trames ne porte pas la trace de la tentative :\n%s", file)
	}
	requireLine(t, screen, "0 trame décodée sur 0 ligne, 0 resynchronisation")
}

// TestCaptureRefusesWhatItCannotDo covers the paths where the measurement cannot
// happen at all. Every message is French and names what to fix.
func TestCaptureRefusesWhatItCannotDo(t *testing.T) {
	t.Run("port introuvable", func(t *testing.T) {
		clock := fake.NewClock(captureStart)
		var out bytes.Buffer
		err := capture(captureRequest{
			link: serial.Options{
				Port: "COM8", Baud: 9600, Clock: clock,
				Open: refusingOpener(errors.New("le fichier spécifié est introuvable")),
			},
			duration: 30 * time.Second,
			path:     filepath.Join(t.TempDir(), "frames.txt"),
		}, &out)
		if err == nil {
			t.Fatal("un port introuvable a été accepté")
		}
		if !strings.Contains(err.Error(), "COM8") ||
			!strings.Contains(err.Error(), "ne peut pas être ouvert") {
			t.Errorf("message inexploitable : %v", err)
		}
	})

	t.Run("fichier de trames impossible à écrire", func(t *testing.T) {
		clock := fake.NewClock(captureStart)
		var out bytes.Buffer
		err := capture(captureRequest{
			link:     serial.Options{Port: "COM8", Baud: 9600, Clock: clock, Open: emitting(clock, 1).opener()},
			duration: 30 * time.Second,
			path:     filepath.Join(t.TempDir(), "repertoire-absent", "frames.txt"),
		}, &out)
		if err == nil {
			t.Fatal("un chemin d'écriture impossible a été accepté")
		}
		if !strings.Contains(err.Error(), "ne peut pas être écrit") {
			t.Errorf("message inexploitable : %v", err)
		}
	})

	t.Run("capture précédente jamais écrasée", func(t *testing.T) {
		clock := fake.NewClock(captureStart)
		path := filepath.Join(t.TempDir(), "frames.txt")
		if err := os.WriteFile(path, []byte("# une capture de trente minutes\n"), 0o600); err != nil {
			t.Fatalf("préparation : %v", err)
		}
		var out bytes.Buffer
		err := capture(captureRequest{
			link:     serial.Options{Port: "COM8", Baud: 9600, Clock: clock, Open: emitting(clock, 1).opener()},
			duration: 30 * time.Second,
			path:     path,
		}, &out)
		if err == nil {
			t.Fatal("la capture précédente a été écrasée")
		}
		if !strings.Contains(err.Error(), "existe déjà") || !strings.Contains(err.Error(), "--out") {
			t.Errorf("message inexploitable : %v", err)
		}
		kept, readErr := os.ReadFile(path)
		if readErr != nil || string(kept) != "# une capture de trente minutes\n" {
			t.Errorf("le fichier précédent a été touché : %q, %v", kept, readErr)
		}
	})

	t.Run("durée nulle", func(t *testing.T) {
		clock := fake.NewClock(captureStart)
		var out bytes.Buffer
		err := capture(captureRequest{
			link:     serial.Options{Port: "COM8", Baud: 9600, Clock: clock, Open: emitting(clock, 1).opener()},
			path:     filepath.Join(t.TempDir(), "frames.txt"),
			duration: 0,
		}, &out)
		if err == nil {
			t.Fatal("une durée nulle a été acceptée")
		}
	})

	// A capture with no clock is a composition mistake, not an operator one: it would
	// read the real one and put a thirty-minute measurement out of reach of any test.
	t.Run("aucune horloge", func(t *testing.T) {
		var out bytes.Buffer
		err := capture(captureRequest{
			link:     serial.Options{Port: "COM8", Baud: 9600},
			path:     filepath.Join(t.TempDir(), "frames.txt"),
			duration: 30 * time.Second,
		}, &out)
		if err == nil {
			t.Fatal("une capture sans horloge a été acceptée")
		}
	})
}

// TestCaptureNamesAStreamThatEnded: an EOF is not the same event as a device error,
// and « EOF » on its own screen means nothing to a volunteer.
func TestCaptureNamesAStreamThatEnded(t *testing.T) {
	clock := fake.NewClock(captureStart)
	stream := emitting(clock, 2)
	stream.endErr = io.EOF
	screen, _, _ := runCaptureOnScript(t, stream, clock, 30*time.Minute, true)

	if !strings.Contains(screen, "le flux de COM8 s'est terminé avant la fin de la capture") {
		t.Errorf("la fin du flux n'est pas nommée en français :\n%s", screen)
	}
	requireLine(t, screen, "2 trames décodées sur 2 lignes, 0 resynchronisation")
}

// TestRunCaptureRefusesBeforeTouchingTheHardware: every one of these has to fail on
// the arguments alone. A test suite that opened a real serial port would fail on a
// machine that has one and pass on a machine that does not.
func TestRunCaptureRefusesBeforeTouchingTheHardware(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"aucun port", []string{"--duration", "30s"}, "--port"},
		{"durée illisible", []string{"--port", "COM8", "--duration", "trente minutes"}, "illisible"},
		{"durée nulle", []string{"--port", "COM8", "--duration", "0s"}, "positive"},
		{"argument en trop", []string{"--port", "COM8", "frames.txt"}, "inattendu"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			err := runCapture(c.args, &out)
			if err == nil {
				t.Fatalf("runCapture(%v) a été accepté", c.args)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("message %q : %q attendu", err.Error(), c.want)
			}
		})
	}
}

// TestFrenchNumbersUseIntegerArithmetic: no float ever reaches this application, and a
// percentage on a diagnostic screen is no reason to introduce the first one.
func TestFrenchNumbersUseIntegerArithmetic(t *testing.T) {
	percents := []struct {
		part, whole int
		want        string
	}{
		{10, 12, "83,3 %"},
		{100, 100, "100,0 %"},
		{0, 7, "0,0 %"},
		{1, 3, "33,3 %"},
		{1, 0, "—"},
	}
	for _, c := range percents {
		if got := percent(c.part, c.whole); got != c.want {
			t.Errorf("percent(%d, %d) = %q, want %q", c.part, c.whole, got, c.want)
		}
	}

	durations := []struct {
		d              time.Duration
		milli, seconds string
	}{
		{412 * time.Millisecond, "412 ms", "0,4 s"},
		{2400 * time.Millisecond, "2400 ms", "2,4 s"},
		{5 * time.Second, "5000 ms", "5 s"},
		{0, "0 ms", "0 s"},
	}
	for _, c := range durations {
		if got := millis(c.d); got != c.milli {
			t.Errorf("millis(%s) = %q, want %q", c.d, got, c.milli)
		}
		if got := secondsLabel(c.d); got != c.seconds {
			t.Errorf("secondsLabel(%s) = %q, want %q", c.d, got, c.seconds)
		}
	}

	offsets := []struct {
		d    time.Duration
		want string
	}{
		{0, "+0,000 s"},
		{412 * time.Millisecond, "+0,412 s"},
		{75 * time.Second, "+75,000 s"},
		{-2 * time.Millisecond, "-0,002 s"},
	}
	for _, c := range offsets {
		if got := offsetLabel(c.d); got != c.want {
			t.Errorf("offsetLabel(%s) = %q, want %q", c.d, got, c.want)
		}
	}
}

// TestObservationsLabelSaysWhichIntervalsTheMedianRestsOn. RateMeter remembers the
// LAST 64 intervals, so on a thirty-minute capture the median is a recent one and not
// the median of the session. Saying « sur 64 intervalles » flat would be a quiet lie.
func TestObservationsLabelSaysWhichIntervalsTheMedianRestsOn(t *testing.T) {
	if got := observationsLabel(11); got != "sur 11 intervalles" {
		t.Errorf("observationsLabel(11) = %q", got)
	}
	if got := observationsLabel(64); got != "sur les 64 derniers intervalles" {
		t.Errorf("observationsLabel(64) = %q", got)
	}
}

// TestCaptureUsageIsFrenchAndNamesThePeakHour. The usage is read by whoever runs the
// binary, and the audience of this project is a cooperative, not a Go developer --
// and the one instruction that decides whether the measurement is worth anything is
// that it be taken at peak hour.
func TestCaptureUsageIsFrenchAndNamesThePeakHour(t *testing.T) {
	var out bytes.Buffer
	if err := runCapture([]string{"--help"}, &out); err == nil {
		t.Fatal("--help doit s'arrêter là")
	}
	usage := out.String()
	for _, want := range []string{"EN HEURE DE POINTE", "--duration", "--port", "openscale replay"} {
		if !strings.Contains(usage, want) {
			t.Errorf("usage sans %q :\n%s", want, usage)
		}
	}
}

// TestCaptureDoesNotSplitACRLFAcrossTwoLines: a terminator delivered in two reads is
// still ONE terminator, exactly as frame.Accumulator treats it. The opposite would
// double the line count of every capture taken on a busy machine.
func TestCaptureDoesNotSplitACRLFAcrossTwoLines(t *testing.T) {
	clock := fake.NewClock(captureStart)
	stream := newScriptedStream(clock,
		scriptedRead{after: cadence, data: "ST,GS,+  1.236KG\r"},
		scriptedRead{after: 2 * time.Millisecond, data: "\nST,GS,+  0.850KG\r\n"},
	)
	screen, file, _ := runCaptureOnScript(t, stream, clock, 5*time.Second, true)

	requireLine(t, screen, "2 trames décodées sur 2 lignes, 0 resynchronisation")
	if got := strings.Count(file, "\n"); got != 8 {
		t.Errorf("%d lignes dans le fichier, 6 de commentaire + 2 de trame attendues :\n%s", got, file)
	}
	if !strings.Contains(file, "@0 ST,GS,+  1.236KG\r\n") {
		t.Errorf("la trame coupée n'a pas été recollée :\n%q", file)
	}
}

// failingWriter is a disk that fills up in the middle of a capture.
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

// TestCorpusWriterGivesUpLoudlyWhenItCannotWrite. A capture that lost frames to a
// full disk and said nothing would produce a corpus file that LOOKS complete, and the
// cadence measured from it would be a fiction -- the exact failure the living corpus
// exists to make impossible.
func TestCorpusWriterGivesUpLoudlyWhenItCannotWrite(t *testing.T) {
	writer := &corpusWriter{to: failingWriter{err: errors.New("disque plein")}}
	if err := writer.feed([]byte(nominalFrame), captureStart); err == nil {
		t.Error("une trame perdue n'a pas été signalée")
	}
	// A fragment waits for its terminator, so feeding it writes nothing; it is finish
	// that has to fail on it.
	if err := writer.feed([]byte("ST,GS,+  1.2"), captureStart); err != nil {
		t.Errorf("une trame incomplète a été écrite avant son terminateur : %v", err)
	}
	if err := writer.finish(); err == nil {
		t.Error("un reliquat perdu n'a pas été signalé")
	}
	if err := writer.header(captureRequest{link: serial.Options{Port: "COM8"}}, captureStart); err == nil {
		t.Error("un en-tête perdu n'a pas été signalé")
	}
}

// Compile-time proof that the scripted port satisfies what an Opener has to return.
var _ io.ReadCloser = (*scriptedStream)(nil)

// TestScriptedStreamNeverNeedsTheRealClock guards the guard: if this double ever
// stopped driving the injected clock, every temporal assertion above would silently
// become a test of nothing.
func TestScriptedStreamNeverNeedsTheRealClock(t *testing.T) {
	clock := fake.NewClock(captureStart)
	stream := emitting(clock, 3)
	buffer := make([]byte, 64)
	for i := 1; i <= 3; i++ {
		n, err := stream.Read(buffer)
		if err != nil || n == 0 {
			t.Fatalf("lecture %d : %d octets, %v", i, n, err)
		}
		if want := captureStart.Add(time.Duration(i) * cadence); !clock.Now().Equal(want) {
			t.Fatalf("lecture %d : horloge à %s, %s attendu", i, clock.Now(), want)
		}
	}
}
