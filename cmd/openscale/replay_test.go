package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"openscale/internal/fake"
)

// corpusDir is the GRAM XFOC RS drawer of the LIVING CORPUS of §15.4, seen from
// cmd/openscale. Replaying the real files matters more than any synthetic one: they are
// what a station actually sent, and they were written before this file format existed —
// before the corpus was filed by protocol, so they carry no « # protocole : » header and
// the command falls back on the protocol it announces.
const corpusDir = "../../internal/scale/testdata/frames/gram-xfoc-rs"

// writeFrames drops a capture file in a temporary directory and returns its path.
func writeFrames(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "frames.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("écriture du fichier de trames : %v", err)
	}
	return path
}

// replayed runs the command and returns what a volunteer would see.
func replayed(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	if err := runReplay(args, &out); err != nil {
		t.Fatalf("runReplay(%v) : %v", args, err)
	}
	return out.String()
}

// TestACaptureSaysWhichGrammarCutItAndAReplayReadsItBack closes the round trip of the two
// commands.
//
// `openscale capture` writes the protocol into the header of the file, and `openscale
// replay` reads it from there rather than from the memory of whoever ran the capture.
// Without that line the two commands would agree only by accident: a capture of one
// protocol replayed through another decodes to nothing AND reports no error, which is the
// answer of an unplugged scale.
func TestACaptureSaysWhichGrammarCutItAndAReplayReadsItBack(t *testing.T) {
	clock := fake.NewClock(captureStart)
	_, file, path := runCaptureOnScript(t, emitting(clock, 3), clock, 5*time.Second, true)

	protocol, _ := benchProtocol(t)
	if !strings.Contains(file, "# protocole : "+protocol) {
		t.Fatalf("le fichier ne dit pas avec quelle grammaire il a été découpé :\n%s", file)
	}

	screen := replayed(t, path)
	if !strings.Contains(screen, "décodé en "+protocol) {
		t.Fatalf("le rejeu n'annonce pas la grammaire lue dans l'en-tête :\n%s", screen)
	}

	// And the flag still wins over the file: a capture cut by one protocol may have to be
	// offered to another, which is exactly what a support call does when the header is
	// wrong.
	forced := replayed(t, path, "--type", "gram-xfoc-plus", "--quiet")
	if !strings.Contains(forced, "décodé en gram-xfoc-plus") {
		t.Fatalf("--type n'a pas primé sur l'en-tête :\n%s", forced)
	}
}

// TestAReplayRefusesAProtocolThisBinaryDoesNotCarry: a tool asked for a grammar that does
// not exist must name the ones that do, never fall back on another and answer « 0 trame ».
func TestAReplayRefusesAProtocolThisBinaryDoesNotCarry(t *testing.T) {
	path := writeFrames(t, "ST,GS,+  1.236KG\r\n")
	var out bytes.Buffer
	err := runReplay([]string{path, "--type", "balance-de-cuisine"}, &out)
	if err == nil {
		t.Fatal("un protocole inexistant a été accepté")
	}
	if !strings.Contains(err.Error(), "gram-xfoc-rs") {
		t.Fatalf("le refus ne nomme pas les protocoles disponibles : %v", err)
	}
}

// TestReplayReadsTheCorpusWrittenBeforeThisFormatExisted is the compatibility claim of
// the capture format, checked against the two files that were already in the
// repository. Neither carries a timestamp, and neither had to be touched.
func TestReplayReadsTheCorpusWrittenBeforeThisFormatExisted(t *testing.T) {
	t.Run("nominal-gram-xfoc.txt", func(t *testing.T) {
		screen := replayed(t, filepath.Join(corpusDir, "nominal-gram-xfoc.txt"))
		// A nominal capture loses nothing: seven lines, seven frames.
		requireLine(t, screen, "7 trames décodées sur 7 lignes, 0 resynchronisation")
		requireLine(t, screen, "trames stables : 5 sur 7 (71,4 %) · instables : 1 · sans indication : 0")
		requireLine(t, screen, "trames en surcharge (OL) : 1 — la balance se déclare hors capacité")
	})

	t.Run("degraded-18-byte-read.txt", func(t *testing.T) {
		screen := replayed(t, filepath.Join(corpusDir, "degraded-18-byte-read.txt"))
		// The degraded file is the artefact of the 18-byte read. Two of its five lines are
		// legal frames; the other three are truncations, and REFUSING them is the right
		// answer -- ".996kg" could have been 1.996 or 10.996 kg.
		requireLine(t, screen, "2 trames décodées sur 5 lignes, 0 resynchronisation")
		if strings.Contains(screen, "996,000 kg") {
			t.Errorf("une troncature a été prise pour une masse :\n%s", screen)
		}
	})
}

// TestReplayNeverPassesOffAFabricatedCadenceAsAMeasurement. A file with no timestamp
// cannot measure a cadence: spacing its frames at the nominal rate and printing the
// median would hand back 400 ms as though it had been observed -- which is exactly
// the confusion unknown No 3 exists to end.
func TestReplayNeverPassesOffAFabricatedCadenceAsAMeasurement(t *testing.T) {
	screen := replayed(t, filepath.Join(corpusDir, "nominal-gram-xfoc.txt"))

	if !strings.Contains(screen, "SANS horodate : instants reconstitués à 400 ms") {
		t.Errorf("l'absence d'horodates n'est pas annoncée :\n%s", screen)
	}
	if !strings.Contains(screen, "cadence : NON MESURABLE") {
		t.Errorf("une cadence a été annoncée sur un fichier sans horodate :\n%s", screen)
	}
	if strings.Contains(screen, "cadence observée") || strings.Contains(screen, "péremption dérivée") {
		t.Errorf("une médiane a été présentée comme une mesure :\n%s", screen)
	}
}

// TestReplayShowsTheLatchState freezes the second half of the L3 criterion. The latch
// anchors the GROSS weight and reports how long the anchor has held; what it names is
// the ANCHOR and never the last frame, which is why the label spells both out.
func TestReplayShowsTheLatchState(t *testing.T) {
	// Four frames 200 ms apart: the anchor needs min_duration_ms = 300 ms to latch, so
	// it takes two frames -- and the third moves by 5 g, past tolerance_g = 2, which
	// resets it.
	path := writeFrames(t, "@0 ST,GS,+  1.236KG\r\n"+
		"@200 ST,GS,+  1.236KG\r\n"+
		"@400 ST,GS,+  1.241KG\r\n"+
		"@600 ST,GS,+  1.241KG\r\n")
	screen := replayed(t, path)

	requireLine(t, screen, "1 +0,000 s 1,236 kg stable non figé (1,236 kg depuis 0 ms)")
	requireLine(t, screen, "2 +0,200 s 1,236 kg stable non figé (1,236 kg depuis 200 ms)")
	requireLine(t, screen, "3 +0,400 s 1,241 kg stable non figé (1,241 kg depuis 0 ms)")
	requireLine(t, screen, "4 +0,600 s 1,241 kg stable non figé (1,241 kg depuis 200 ms)")
}

// TestReplayLatchesOnceTheAnchorHasHeldLongEnough is the other half of the same rule:
// past min_duration_ms the anchor is frozen, and the weight it reports is the anchor,
// not the latest fluctuation.
func TestReplayLatchesOnceTheAnchorHasHeldLongEnough(t *testing.T) {
	// The second frame drifts by 1 g, inside tolerance_g = 2: the anchor does NOT move,
	// and 412 ms later it is latched -- at 1,236 kg, the anchor, not at 1,237.
	path := writeFrames(t, "@0 ST,GS,+  1.236KG\r\n@412 ST,GS,+  1.237KG\r\n")
	screen := replayed(t, path)

	requireLine(t, screen, "2 +0,412 s 1,237 kg stable FIGÉ à 1,236 kg (tenu 412 ms)")
}

// TestEighteenByteReadDecodesEveryFrameWhateverTheAlignment is the §18 demonstration,
// and the reason it is worth making: the accumulator decodes 100 out of 100 BECAUSE
// it does not care where a read ends. The legacy application read 18 fixed bytes for
// 18-byte frames, and one byte of drift cut every following frame in half.
func TestEighteenByteReadDecodesEveryFrameWhateverTheAlignment(t *testing.T) {
	path := filepath.Join(corpusDir, "nominal-gram-xfoc.txt")
	for _, size := range []string{"1", "5", "17", "18", "19", "512"} {
		t.Run("lectures de "+size+" octets", func(t *testing.T) {
			screen := replayed(t, path, "--read-size", size, "--quiet")
			requireLine(t, screen, "7 trames décodées sur 7 lignes, 0 resynchronisation")
		})
	}
}

// TestReadSizeVerdictNamesTheDefectItReproduces: a figure with no sentence beside it
// is a figure nobody acts on, and « 7 sur 7 » means nothing to whoever has not read
// §18.
func TestReadSizeVerdictNamesTheDefectItReproduces(t *testing.T) {
	screen := replayed(t, filepath.Join(corpusDir, "nominal-gram-xfoc.txt"),
		"--read-size", "18", "--quiet")

	requireLine(t, screen, "Lectures de 18 octets FIXES : 7 trames décodées sur 7 lignes (100,0 %).")
	if !strings.Contains(screen, "en perdait une sur deux") {
		t.Errorf("le verdict ne nomme pas le défaut qu'il reproduit :\n%s", screen)
	}
}

// TestX10CompressesTheInstants. In a command that has nothing to wait on, "ten times
// faster" means ten times closer together: the cadence and the derived expiry that
// come out are those a scale ten times faster would produce.
func TestX10CompressesTheInstants(t *testing.T) {
	var lines strings.Builder
	for i := 0; i < 12; i++ {
		lines.WriteString("@" + strconv.Itoa(i*412) + " ST,GS,+  1.236KG\r\n")
	}
	path := writeFrames(t, lines.String())

	requireLine(t, replayed(t, path), "cadence observée : médiane 412 ms sur 11 intervalles")

	fast := replayed(t, path, "--x10")
	requireLine(t, fast, "cadence observée : médiane 41 ms sur 11 intervalles")
	if !strings.Contains(fast, "horodates présentes, rejouées ×10") {
		t.Errorf("le facteur de vitesse n'est pas annoncé :\n%s", fast)
	}
	// The derived expiry falls back on its FLOOR: 3 x 41 ms is far under 1 200 ms, and
	// a station must never consider a weight expired 123 ms after it was measured.
	requireLine(t, fast, "péremption dérivée : 1200 ms (facteur 3, plancher 1200 ms, plafond 5000 ms)")
}

// TestCorpusFormatNeverMistakesAFrameForAMarker. The grammar of §9.2 lets a frame
// begin with a status letter, a sign, a blank OR A DIGIT, and pads its number with
// blanks that are part of the protocol. The '@' marker can be none of those, which is
// why it is in FRONT and why no rule is needed to tell the two apart -- these three
// lines would each have needed one.
func TestCorpusFormatNeverMistakesAFrameForAMarker(t *testing.T) {
	path := writeFrames(t, "ST,GS,+\t1.236KG\r\n"+ // a tab as padding
		"412 1.236KG\r\n"+ // a frame that opens on digits
		"@0  0.996kg\r\n") // a marker, then a frame whose first byte is a blank
	screen := replayed(t, path)

	requireLine(t, screen, "3 trames décodées sur 3 lignes, 0 resynchronisation")
	for _, want := range []string{"1,236 kg", "0,996 kg"} {
		if !strings.Contains(screen, want) {
			t.Errorf("%s absent — une trame a été décapitée :\n%s", want, screen)
		}
	}
}

// TestReplayCarriesTheParserRefusalThroughToTheOperator. The format is defined by
// internal/scale/replay, and so are its refusals: a malformed marker and an offset
// that goes backwards both name the LINE NUMBER of the file as an editor shows it.
// This command must hand that sentence through, not flatten it into "unreadable file".
func TestReplayCarriesTheParserRefusalThroughToTheOperator(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"marqueur sans nombre", "@ ST,GS,+  1.236KG\r\n", "ligne 1"},
		{"horodatage qui recule",
			"@0 ST,GS,+  1.236KG\r\n@800 ST,GS,+  1.236KG\r\n@400 ST,GS,+  1.236KG\r\n",
			"ligne 3"},
		{"fichier sans trame", "# rien que des commentaires\n", "aucune trame"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			err := runReplay([]string{writeFrames(t, c.content)}, &out)
			if err == nil {
				t.Fatalf("capture invalide acceptée : %q", c.content)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("message %q : %q attendu", err.Error(), c.want)
			}
		})
	}
}

// TestReplayIgnoresCommentsAndBlankLines: a capture file carries its own header, and a
// contributor adding a note to a corpus file must not have to worry about breaking a
// test.
func TestReplayIgnoresCommentsAndBlankLines(t *testing.T) {
	path := writeFrames(t, "# openscale capture — COM8\n"+
		"# une note ajoutée à la main\n"+
		"\r\n"+
		"@0 ST,GS,+  1.236KG\r\n"+
		"\n"+
		"@412 ST,GS,+  0.850KG\r\n")
	screen := replayed(t, path)

	requireLine(t, screen, "2 trames décodées sur 2 lignes, 0 resynchronisation")
	// Two comments and two blank lines, and neither is counted as a frame nor mistaken
	// for a line that declares no instant.
	if !strings.Contains(screen, "2 lignes de trame, horodates présentes") {
		t.Errorf("les commentaires ont été comptés comme des trames :\n%s", screen)
	}
}

// TestReplayReportsNoiseWithoutInventingAMass. A file of pure noise replays as zero
// frames out of n, which is a diagnostic and NOT an error: it is the answer to « la
// balance babille », and refusing to name a mass is the whole point of the grammar.
func TestReplayReportsNoiseWithoutInventingAMass(t *testing.T) {
	path := writeFrames(t, ".996kg\r\nxyzzy\r\nST,GS,+  1.\r\n")
	screen := replayed(t, path)

	requireLine(t, screen, "0 trame décodée sur 3 lignes, 0 resynchronisation")
	if strings.Contains(screen, "kg  ") {
		t.Errorf("du bruit a produit une masse :\n%s", screen)
	}
}

// TestReplayRefusesWhatItCannotRead. Every message is French and names what to fix.
func TestReplayRefusesWhatItCannotRead(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"fichier absent", []string{filepath.Join(t.TempDir(), "jamais-ecrit.txt")}, "illisible"},
		{"aucun fichier", nil, "exactement un fichier"},
		{"deux fichiers", []string{"a.txt", "b.txt"}, "exactement un fichier"},
		{"taille de lecture négative", []string{"a.txt", "--read-size", "-3"}, "positive"},
		{"taille de lecture illisible", []string{"a.txt", "--read-size", "dix-huit"}, "read-size"},
		{"option inconnue", []string{"a.txt", "--vitesse", "10"}, "vitesse"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			err := runReplay(c.args, &out)
			if err == nil {
				t.Fatalf("runReplay(%v) a été accepté", c.args)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("message %q : %q attendu", err.Error(), c.want)
			}
		})
	}
}

// TestReplayIsDeterministic: the command reads NO clock of its own. Every instant
// comes from the file, so two runs on two machines print the same thing -- which is
// what makes a capture comparable with the one taken last month.
func TestReplayIsDeterministic(t *testing.T) {
	path := writeFrames(t, "@0 ST,GS,+  1.236KG\r\n@412 US,GS,+  1.240KG\r\n")
	first := replayed(t, path)
	if second := replayed(t, path); first != second {
		t.Errorf("deux exécutions divergent :\n%s\n--- vs ---\n%s", first, second)
	}
}

// TestCaptureAndReplayAgreeOnTheSameStream is the round trip that makes the file
// format worth having: what capture measured live and what replay measures from the
// file it wrote have to be the same figures. Otherwise the corpus would be a story
// about a capture rather than the capture.
func TestCaptureAndReplayAgreeOnTheSameStream(t *testing.T) {
	clock := fake.NewClock(captureStart)
	live, _, path := runCaptureOnScript(t, emitting(clock, 20, 3, 9), clock, time.Minute, true)
	fromFile := replayed(t, path, "--quiet")

	for _, want := range []string{
		"20 trames décodées sur 20 lignes, 0 resynchronisation",
		"cadence observée : médiane 412 ms sur 19 intervalles",
		"péremption dérivée : 1236 ms (facteur 3, plancher 1200 ms, plafond 5000 ms)",
		"trames stables : 18 sur 20 (90,0 %) · instables : 2 · sans indication : 0",
	} {
		requireLine(t, live, want)
		requireLine(t, fromFile, want)
	}
}
