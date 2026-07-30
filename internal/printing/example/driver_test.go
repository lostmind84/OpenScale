package example

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/printing/conformance"
	"openscale/internal/station/ports"
)

// t0 is where the injected clock of these tests starts, and it sits deliberately far in the
// PAST: any instant a driver took from the wall clock lands years away from the window this
// clock ever covered.
var t0 = time.Date(2020, 1, 1, 8, 0, 0, 0, time.UTC)

// TestTheDefaultDestinationIsTheDriversOwnBuffer covers the one path the conformance suite
// never takes: the suite always hands a destination of its own, so nothing else exercises
// the buffer a driver built without an Options.Sink writes into.
func TestTheDefaultDestinationIsTheDriversOwnBuffer(t *testing.T) {
	printer := newPrinter(t, Options{})
	defer printer.Close()

	if _, err := printer.Print(context.Background(), referenceJob(t, "01J9EXAMPLE1")); err != nil {
		t.Fatalf("Print : %v", err)
	}
	if frames := printer.Frames(); frames != 1 {
		t.Fatalf("%d trame(s) dans le tampon après une impression, attendu 1", frames)
	}
	if !strings.Contains(string(printer.Buffered()), "01J9EXAMPLE1") {
		t.Error("la trame écrite ne porte pas l'identifiant du travail : c'est lui qui relie " +
			"l'accusé de réception à l'écran, la ligne du journal et la barre de réimpression")
	}
}

// TestParseOptionsRefusesWhatAVolunteerHasToBeToldAbout holds the parser to the three
// answers §11.2 distinguishes: a key that is ABSENT, a key present with the WRONG TYPE, and
// a value OUT OF RANGE. None of the three may become a silent default.
//
// The third is the one that costs: a copy count quietly turned into one is a volunteer
// pressing a button that no longer does what it says.
func TestParseOptionsRefusesWhatAVolunteerHasToBeToldAbout(t *testing.T) {
	for _, c := range []struct {
		name    string
		options domain.DriverOptions
		names   string
	}{
		{"absente", options(t, map[string]any{}), optionCopies},
		{"mauvais type", options(t, map[string]any{optionCopies: "1"}), optionCopies},
		{"hors bornes", options(t, map[string]any{optionCopies: MaxCopies + 1}), optionCopies},
		{"booleen mal ecrit", options(t, map[string]any{optionCopies: 1, optionHeader: "oui"}), optionHeader},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseOptions(c.options)
			if err == nil {
				t.Fatalf("les options %v ont été acceptées", c.options)
			}
			if !strings.Contains(err.Error(), c.names) {
				t.Errorf("le refus « %v » ne nomme pas la clé %q : un bénévole doit savoir "+
					"quel champ corriger", err, c.names)
			}
		})
	}
}

// TestParseOptionsReadsWhatTheSchemaDeclares is the other half of the same contract: every
// key OptionSchema offers is a key ParseOptions really reads, or the generated form carries
// a field the driver ignores.
func TestParseOptionsReadsWhatTheSchemaDeclares(t *testing.T) {
	settings, err := ParseOptions(options(t, map[string]any{
		optionCopies: 3,
		optionHeader: false,
	}))
	if err != nil {
		t.Fatalf("ParseOptions : %v", err)
	}
	if settings.Copies != 3 || settings.Header {
		t.Fatalf("options lues %+v, attendu {Copies:3 Header:false}", settings)
	}
	for _, declared := range OptionSchema() {
		if declared.Key != optionCopies && declared.Key != optionHeader {
			t.Errorf("le schéma déclare la clé %q que ParseOptions ne lit pas : le formulaire "+
				"généré porterait un champ dont le driver ne fait rien", declared.Key)
		}
	}
}

// TestAJobThatNamesNoCopyCountTakesTheConfiguredOne is §8.2 read from the driver's side: the
// print service builds its PrintJob WITHOUT a Copies field, so zero means « unspecified ».
func TestAJobThatNamesNoCopyCountTakesTheConfiguredOne(t *testing.T) {
	printer := newPrinter(t, Options{Settings: Settings{Copies: 4, Header: true}})
	defer printer.Close()

	if _, err := printer.Print(context.Background(), referenceJob(t, "01J9EXAMPLE2")); err != nil {
		t.Fatalf("Print : %v", err)
	}
	if !strings.Contains(string(printer.Buffered()), "4 copie(s)") {
		t.Errorf("la trame ne demande pas les 4 exemplaires de printer.options.copies : "+
			"un driver qui lit job.Copies au pied de la lettre n'imprime rien du tout.\n%s",
			firstLine(printer.Buffered()))
	}
}

// newPrinter builds one driver, filling in the collaborators the caller did not name.
func newPrinter(t *testing.T, o Options) *Printer {
	t.Helper()
	if o.Clock == nil {
		o.Clock = fake.NewClock(t0)
	}
	if o.Template.Media.DotsPerMM == 0 {
		o.Template = domain.IdenticalTemplate()
	}
	if o.Settings == (Settings{}) {
		o.Settings = DefaultSettings()
	}
	if o.DemoLabel == nil {
		o.DemoLabel = conformance.DemoLabel
	}
	printer, err := New(o)
	if err != nil {
		t.Fatalf("New : %v", err)
	}
	return printer
}

// referenceJob is the demonstration weighing of §8.6, which is what the conformance suite
// prints too: ail, 1,236 kg.
func referenceJob(t *testing.T, jobID string) ports.PrintJob {
	t.Helper()
	label, err := conformance.DemoLabel()
	if err != nil {
		t.Fatalf("étiquette de démonstration : %v", err)
	}
	label.JobID = jobID
	return ports.PrintJob{
		Label:    label,
		Template: domain.IdenticalTemplate(),
		Locale:   string(domain.LocaleFrench),
	}
}

// options renders a printer.options block the way config.json carries it.
//
// Through encoding/json and not by wrapping values in quotation marks: what is under test is
// exactly the difference between the number 1 and the string "1".
func options(t *testing.T, values map[string]any) domain.DriverOptions {
	t.Helper()
	out := make(domain.DriverOptions, len(values))
	for key, value := range values {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("encodage de l'option %q : %v", key, err)
		}
		out[key] = encoded
	}
	return out
}

// firstLine is the readable head of a frame, for a failure message that shows what was
// written instead of dumping a bitmap.
func firstLine(frame []byte) string {
	if cut := strings.IndexByte(string(frame), '\n'); cut >= 0 {
		return string(frame[:cut])
	}
	return string(frame)
}
