package sbpl_test

import (
	"errors"
	"strings"
	"testing"

	"openscale/internal/printing/sbpl"
	"openscale/internal/station/ports"
)

// What happens when it does not go through: a refused job writes NOTHING AT ALL to the
// transport — not one byte, not the beginning of a frame the printer would then sit
// waiting to see finished — and a transport that refuses is reported as TRANSIENT,
// because that is a cable or a queue, never a wrong label.

// --- 5. A refused job leaves the transport untouched ------------------------

// countingWriter accepts everything and remembers how much.
type countingWriter struct{ written int }

func (w *countingWriter) Write(p []byte) (int, error) {
	w.written += len(p)
	return len(p), nil
}

// TestARefusedJobWritesNothingAtAll is property 3 of the package documentation, and
// it is the one departure from the sketch of §8.3 that has teeth.
//
// That sketch validates each command as it writes it, so a job whose <G> is too wide
// puts <A>, <A1>, <A3>, <#E>, <CS> and <%> on the wire and then stops: the printer is
// left mid-job, with every parameter reset and nothing to print, and the next job
// starts on top of it. Validating the whole job first costs one traversal and removes
// the state entirely.
//
// The invalid jobs come out of NewJob itself, which returns the job it refused —
// that is the only way an external caller can hold one, and it is exactly the value
// this test needs.
func TestARefusedJobWritesNothingAtAll(t *testing.T) {
	valid := smallJob(t)
	setup := mustSetup(t, 24, 16)
	graphic := mustGraphic(t, 0, 0, smallBitmap(), sbpl.InkIsOne)
	copies, err := sbpl.NewCopies(shippedCopies)
	if err != nil {
		t.Fatalf("NewCopies : %v", err)
	}

	tooWide, _ := sbpl.NewGraphic(sbpl.WS408(), 0, 0, checkerboard(105*8, 1), sbpl.InkIsOne)
	forgedSetup, _ := sbpl.NewJob(sbpl.Setup{}, graphic, copies)
	forgedGraphic, _ := sbpl.NewJob(setup, sbpl.Graphic{}, copies)
	forgedCopies, _ := sbpl.NewJob(setup, graphic, sbpl.Copies{})
	oversized, _ := sbpl.NewJob(setup, tooWide, copies)

	for _, c := range []struct {
		name string
		job  sbpl.Job
		op   string
		kind ports.Kind
	}{
		{"travail vide", sbpl.Job{}, "sbpl.media", ports.KindConfig},
		{"réglages forgés", forgedSetup, "sbpl.media", ports.KindConfig},
		{"graphique forgé", forgedGraphic, "sbpl.model", ports.KindConfig},
		{"exemplaires forgés", forgedCopies, "sbpl.copies", ports.KindConfig},
		{"bloc trop large", oversized, "sbpl.graphic", ports.KindTemplate},
	} {
		t.Run(c.name, func(t *testing.T) {
			transport := &countingWriter{}
			err := sbpl.Encode(transport, c.job)
			if err == nil {
				t.Fatal("Encode a accepté un travail invalide")
			}
			if transport.written != 0 {
				t.Errorf("%d octets sont partis sur le transport avant le refus : "+
					"l'imprimante reste en plein travail", transport.written)
			}
			assertPrintError(t, err, c.kind, c.op)
		})
	}

	// And the valid job of the same shape does reach the transport, so the test
	// above is not passing because nothing ever gets written.
	transport := &countingWriter{}
	if err := sbpl.Encode(transport, valid); err != nil {
		t.Fatalf("Encode d'un travail valide : %v", err)
	}
	if transport.written == 0 {
		t.Error("un travail valide n'a rien écrit : le test des refus ne prouve rien")
	}
}

// --- 9. What the transport says, and what the driver announces --------------

// errRefused is what a device that stops taking bytes looks like from here.
var errRefused = errors.New("le périphérique a refusé l'écriture")

// failingWriter accepts a fixed number of bytes and then refuses everything.
type failingWriter struct {
	accept  int
	written int
}

func (w *failingWriter) Write(p []byte) (int, error) {
	if w.written+len(p) > w.accept {
		return 0, errRefused
	}
	w.written += len(p)
	return len(p), nil
}

// TestATransportThatRefusesIsTransient checks the one failure this package can meet
// at write time, and the policy it carries.
//
// A device that stops taking bytes is exactly what the two retries of §8.2 exist
// for. Reporting it as anything but KindTransient would make the print service give
// up on a printer that was merely busy.
//
// 60 is in the table by measurement, not by taste: the ten commands around the bitmap
// weigh exactly that on this job, so a device that accepts 60 bytes and no more is one
// that dies on the FIRST BYTE OF THE PAYLOAD — the one write of the encoder that is
// not a formatted command, and the one carrying 16 kB behind it.
func TestATransportThatRefusesIsTransient(t *testing.T) {
	for _, accept := range []int{0, 4, 30, 60} {
		transport := &failingWriter{accept: accept}
		err := sbpl.Encode(transport, smallJob(t))
		if err == nil {
			t.Fatalf("un transport qui refuse après %d octets n'a produit aucune erreur", accept)
		}
		assertPrintError(t, err, ports.KindTransient, "sbpl.encode")
		var refusal *ports.PrintError
		errors.As(err, &refusal)
		if !refusal.Retryable() {
			t.Error("une panne de transport doit être réessayable (§8.5)")
		}
		if !errors.Is(err, errRefused) {
			t.Errorf("l'erreur du transport n'est pas enveloppée : %v", err)
		}
		if !strings.Contains(err.Error(), "sbpl.encode") {
			t.Errorf("le message ne nomme pas l'opération : %v", err)
		}
	}
}
