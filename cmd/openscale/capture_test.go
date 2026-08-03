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

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/scale/serial"
)

// The `openscale capture` subcommand: what it measures, what it prints, and what it
// refuses. It is the instrument of unknown n° 3 of §21 — the real emission cadence of
// the scale — so the assertions are about the OBSERVED median and the stable ratio.
//
// The serial port it listens to is a double, in scriptedstream_test.go; the file it
// writes is asserted in corpuswriter_test.go, and the French units of its summary in
// report_test.go.

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

// --- the bench --------------------------------------------------------------------

// benchProtocol is the grammar these tests capture with, resolved THROUGH THE REGISTRY
// exactly as the command line resolves it.
//
// Naming a literal here would make the bench decode with a grammar the binary might not
// even carry; going through decoderOf is what makes these tests exercise the path
// `openscale capture --type` takes, default included.
func benchProtocol(t *testing.T) (string, domain.Decoder) {
	t.Helper()
	protocol, decoder, err := decoderOf(scaleRegistry(), "")
	if err != nil {
		t.Fatalf("protocole de banc : %v", err)
	}
	return protocol, decoder
}

// runCaptureOnScript captures a scripted port into a temporary file and returns what
// the operator saw and what the file holds.
func runCaptureOnScript(t *testing.T, stream *scriptedStream, clock *fake.Clock,
	duration time.Duration, quiet bool) (screen string, file string, path string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "frames.txt")
	protocol, decoder := benchProtocol(t)
	var out bytes.Buffer
	err := capture(captureRequest{
		link: serial.Options{
			Port: "COM8", Baud: 9600, Clock: clock, Open: stream.opener(),
		},
		duration: duration,
		path:     path,
		protocol: protocol,
		decoder:  decoder,
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

// TestCaptureNeverReconnects is the one place capture departs from the production
// loop of internal/scale/serial, and it is deliberate: a cadence measured across an
// outage describes the outage. The link that drops ends the capture, is NAMED, and
// what was read stays in the file.
func TestCaptureNeverReconnects(t *testing.T) {
	clock := fake.NewClock(captureStart)
	stream := emitting(clock, 3)
	stream.endErr = errors.New("le port a disparu")

	path := filepath.Join(t.TempDir(), "frames.txt")
	protocol, decoder := benchProtocol(t)
	var out bytes.Buffer
	err := capture(captureRequest{
		link:     serial.Options{Port: "COM8", Baud: 9600, Clock: clock, Open: stream.opener()},
		duration: 30 * time.Minute,
		path:     path,
		protocol: protocol,
		decoder:  decoder,
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
		protocol, decoder := benchProtocol(t)
		var out bytes.Buffer
		err := capture(captureRequest{
			link: serial.Options{
				Port: "COM8", Baud: 9600, Clock: clock,
				Open: refusingOpener(errors.New("le fichier spécifié est introuvable")),
			},
			duration: 30 * time.Second,
			path:     filepath.Join(t.TempDir(), "frames.txt"),
			protocol: protocol,
			decoder:  decoder,
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
		protocol, decoder := benchProtocol(t)
		var out bytes.Buffer
		err := capture(captureRequest{
			link:     serial.Options{Port: "COM8", Baud: 9600, Clock: clock, Open: emitting(clock, 1).opener()},
			duration: 30 * time.Second,
			path:     filepath.Join(t.TempDir(), "repertoire-absent", "frames.txt"),
			protocol: protocol,
			decoder:  decoder,
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
		protocol, decoder := benchProtocol(t)
		var out bytes.Buffer
		err := capture(captureRequest{
			link:     serial.Options{Port: "COM8", Baud: 9600, Clock: clock, Open: emitting(clock, 1).opener()},
			duration: 30 * time.Second,
			path:     path,
			protocol: protocol,
			decoder:  decoder,
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
