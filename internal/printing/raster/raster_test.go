package raster

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/station/ports"
)

// The tests of the driver itself: what it declares, what it refuses, and what it
// hands to the transport.
//
// No printer, no queue, no device node and no time.Sleep. The clock is the injected
// fake one, and the transport is the recorder below — which is the whole point of
// ports.Transport being an interface declared on the consumer's side (§5.3).

// t0 is the instant every test clock starts from. Its value carries no meaning; that
// it never moves on its own does.
var t0 = time.Date(2026, 7, 25, 14, 32, 5, 0, time.UTC)

// writeDelay is how long the recorder pretends a write takes. It is charged to the
// INJECTED clock, so the receipt carries a duration a test can assert and the suite
// still runs in microseconds (§16.4).
const writeDelay = 40 * time.Millisecond

// recorder is a ports.Transport that keeps what it was given.
type recorder struct {
	clock *fake.Clock
	// frames holds every frame handed over, in order.
	frames [][]byte
	// writeErr, when set, is what Write answers instead of accepting the bytes.
	writeErr error
	// shortBy makes Write report fewer bytes than it took, with NO error: the failure
	// mode that prints a truncated label while the station reports a success.
	shortBy int
	// answer and queryErr are what Query gives back. The zero value of queryErr is
	// ports.ErrUnsupported, because that is what a one-way transport answers.
	answer   []byte
	queryErr error
	queries  [][]byte
	closed   int
}

func newRecorder(clk *fake.Clock) *recorder {
	return &recorder{clock: clk, queryErr: ports.ErrUnsupported}
}

func (r *recorder) Name() string { return "recorder" }

func (r *recorder) Write(ctx context.Context, p []byte) (int, error) {
	r.clock.Advance(writeDelay)
	if r.writeErr != nil {
		return 0, r.writeErr
	}
	r.frames = append(r.frames, append([]byte(nil), p...))
	return len(p) - r.shortBy, nil
}

func (r *recorder) Query(ctx context.Context, request []byte, budget time.Duration) ([]byte, error) {
	r.queries = append(r.queries, append([]byte(nil), request...))
	return r.answer, r.queryErr
}

func (r *recorder) Describe() string { return "enregistreur de trames (test)" }

func (r *recorder) Close() error {
	r.closed++
	return nil
}

// last returns the frame the driver handed over most recently.
func (r *recorder) last(t *testing.T) []byte {
	t.Helper()
	if len(r.frames) == 0 {
		t.Fatal("aucune trame remise au transport")
	}
	return r.frames[len(r.frames)-1]
}

// newPrinter builds a driver on a fake clock and a recording transport.
func newPrinter(t *testing.T, tune func(*Options)) (*Printer, *recorder, *fake.Clock) {
	t.Helper()
	clk := fake.NewClock(t0)
	transport := newRecorder(clk)
	o := Options{
		Transport: transport,
		Clock:     clk,
		Template:  domain.IdenticalTemplate(),
		Settings:  DefaultSettings(),
	}
	if tune != nil {
		tune(&o)
	}
	printer, err := New(o)
	if err != nil {
		t.Fatalf("New : %v", err)
	}
	t.Cleanup(func() { printer.Close() })
	return printer, transport, clk
}

// job builds the print job of the reference weighing.
func job(t *testing.T) ports.PrintJob {
	t.Helper()
	template, _ := productionLabel(t)
	label, err := domain.Price(celeryRow, domain.Measurement{Gross: referenceMass}, domain.LaCagetteRules())
	if err != nil {
		t.Fatalf("Price : %v", err)
	}
	plan, err := domain.PlanFor(celeryRow.Reference)
	if err != nil {
		t.Fatalf("plan : %v", err)
	}
	code, err := domain.Generate(celeryRow.Reference, int64(referenceMass), plan.PayloadWidth)
	if err != nil {
		t.Fatalf("Generate : %v", err)
	}
	label.Barcode = code
	label.JobID = "01J9F2ABC"
	return ports.PrintJob{Label: label, Template: template, Locale: string(domain.LocaleFrench)}
}

// --- Identity ---------------------------------------------------------------

// TestTheDescriptorIsWhatTheAdminScreenReads holds the driver to the identity a
// configuration names it by and a volunteer picks it by.
func TestTheDescriptorIsWhatTheAdminScreenReads(t *testing.T) {
	printer, _, _ := newPrinter(t, nil)
	d := printer.Descriptor()

	if d.ID != "raster" {
		t.Errorf("ID = %q, attendu \"raster\" : c'est la valeur de printer.type et le DÉFAUT de §8.1", d.ID)
	}
	if d.Label != "Imprimante d'étiquettes (rendu image)" {
		t.Errorf("Label = %q : c'est ce qu'un bénévole lit dans la liste déroulante, en français (§8.2)", d.Label)
	}
	if !d.Capabilities.Raster {
		t.Error("Capabilities.Raster est faux sur le driver qui rastérise")
	}
	if d.Capabilities.Cutter {
		t.Error("Capabilities.Cutter est vrai : aucune des onze commandes de §8.3 ne coupe, " +
			"et un interrupteur qui ne fait rien est pire que pas d'interrupteur")
	}
	if d.Capabilities.MaxCopies != MaxCopies {
		t.Errorf("MaxCopies = %d, attendu %d (champ <Q>, six chiffres)", d.Capabilities.MaxCopies, MaxCopies)
	}
	if d.Capabilities.DotsPerMM != 8 {
		t.Errorf("DotsPerMM = %g, attendu 8 sur une WS408 : c'est ce qui est comparé à "+
			"template.media.dots_per_mm", d.Capabilities.DotsPerMM)
	}
	if again := printer.Descriptor(); again != d {
		t.Error("Descriptor() ne rend pas deux fois la même chose : une identité qui bouge n'est pas une identité")
	}
}

// --- The nominal path -------------------------------------------------------

// TestPrintHandsTheWholeFrameOverAndTimesItOnTheInjectedClock is the nominal path,
// end to end: the label is rendered, encapsulated, and the receipt describes what
// really left.
func TestPrintHandsTheWholeFrameOverAndTimesItOnTheInjectedClock(t *testing.T) {
	printer, transport, _ := newPrinter(t, nil)
	j := job(t)

	receipt, err := printer.Print(context.Background(), j)
	if err != nil {
		t.Fatalf("Print : %v", err)
	}
	frame := transport.last(t)

	if receipt.JobID != j.Label.JobID {
		t.Errorf("JobID = %q, attendu %q : c'est ce qui relie l'accusé de l'écran à la ligne du journal",
			receipt.JobID, j.Label.JobID)
	}
	if receipt.Bytes != len(frame) {
		t.Errorf("Bytes = %d pour une trame de %d octets", receipt.Bytes, len(frame))
	}
	if receipt.Duration != writeDelay {
		t.Errorf("Duration = %s, attendu %s : elle est mesurée par l'horloge INJECTÉE (§5.3)",
			receipt.Duration, writeDelay)
	}
	// And the frame really carries the label: same dots as the render.
	_, rendered := productionLabel(t)
	compareDots(t, rendered, readFrame(t, frame).graphic)
}

// TestTheCopyCounterFollowsTheJobThenTheConfiguration covers <Q> from the caller's
// side: a job that says nothing gets what the configuration says, and a job that
// asks for a count out of range is refused rather than rounded into printing.
func TestTheCopyCounterFollowsTheJobThenTheConfiguration(t *testing.T) {
	printer, transport, _ := newPrinter(t, func(o *Options) {
		s := DefaultSettings()
		s.Copies = 2
		o.Settings = s
	})
	j := job(t)

	if _, err := printer.Print(context.Background(), j); err != nil {
		t.Fatalf("Print : %v", err)
	}
	if got := commandArg(readFrame(t, transport.last(t)), "Q"); got != "000002" {
		t.Errorf("<Q>%s : un travail qui ne dit rien prend printer.options.copies, ici 2", got)
	}

	j.Copies = 3
	if _, err := printer.Print(context.Background(), j); err != nil {
		t.Fatalf("Print 3 exemplaires : %v", err)
	}
	if got := commandArg(readFrame(t, transport.last(t)), "Q"); got != "000003" {
		t.Errorf("<Q>%s : le travail demandait 3 exemplaires", got)
	}

	before := len(transport.frames)
	for _, copies := range []int{-1, MaxCopies + 1} {
		j.Copies = copies
		_, err := printer.Print(context.Background(), j)
		printError(t, err, ports.KindConfig, "exemplaires")
	}
	if len(transport.frames) != before {
		t.Errorf("%d trame(s) partie(s) alors que le nombre d'exemplaires est refusé",
			len(transport.frames)-before)
	}
}

// --- What it refuses --------------------------------------------------------

// TestAnUnusableBarcodeIsARefusalAboutThePRODUCT is the taxonomy doing its job: the
// remedy is in Odoo, not in the template, and the screen says so.
func TestAnUnusableBarcodeIsARefusalAboutTheProduct(t *testing.T) {
	printer, transport, _ := newPrinter(t, nil)
	j := job(t)
	j.Label.Barcode = "0493021012366" // dernier chiffre faux

	_, err := printer.Print(context.Background(), j)
	printError(t, err, ports.KindData, "code-barres")
	if len(transport.frames) != 0 {
		t.Error("une trame est partie avec un code-barres invalide : la caisse lirait autre chose que l'étiquette")
	}
}

// TestATemplateOfAnotherHeadIsRefused covers the mistake no byte of the frame would
// betray: the label simply comes out at another scale.
func TestATemplateOfAnotherHeadIsRefused(t *testing.T) {
	printer, _, _ := newPrinter(t, nil)
	j := job(t)
	j.Template.Media.DotsPerMM = 12 // WS412

	_, err := printer.Print(context.Background(), j)
	printError(t, err, ports.KindTemplate, "12 dots/mm")
}

// TestATransportFailureIsTransientAndSaysSo is what the print service reads to
// decide about its two retries (§8.2, §8.5).
func TestATransportFailureIsTransientAndSaysSo(t *testing.T) {
	fault := errors.New("file d'impression injoignable")
	printer, transport, _ := newPrinter(t, nil)
	transport.writeErr = fault

	_, err := printer.Print(context.Background(), job(t))
	printError(t, err, ports.KindTransient, "ne répond pas")

	var printErr *ports.PrintError
	if !errors.As(err, &printErr) || !printErr.Retryable() {
		t.Fatal("une panne de transport n'est pas déclarée réessayable : les deux réessais de §8.5 ne partiront jamais")
	}
	if !errors.Is(err, fault) {
		t.Error("la cause du transport n'est pas enveloppée : le journal technique perd la seule ligne qui dit pourquoi")
	}
}

// TestAShortWriteIsAFailureAndNotASuccess is the failure mode that costs the most: a
// truncated frame, a label that comes out wrong, and a station announcing a success.
func TestAShortWriteIsAFailureAndNotASuccess(t *testing.T) {
	printer, transport, _ := newPrinter(t, nil)
	transport.shortBy = 1

	_, err := printer.Print(context.Background(), job(t))
	printError(t, err, ports.KindTransient, "tronquée")
}

// --- Status -----------------------------------------------------------------

// TestStatusIsHonestAboutWhatItCannotKnow covers the three answers of §8.5, and the
// one it must never give: a failure invented out of a silence.
func TestStatusIsHonestAboutWhatItCannotKnow(t *testing.T) {
	for _, c := range []struct {
		name     string
		arrange  func(*recorder)
		health   ports.PrinterHealth
		says     string
		wantsRaw bool
	}{
		{
			name:    "transport unidirectionnel",
			arrange: func(r *recorder) { r.queryErr = ports.ErrUnsupported },
			health:  ports.PrinterUnknown,
			says:    "état inconnu",
		},
		{
			name:    "imprimante muette",
			arrange: func(r *recorder) { r.queryErr = nil; r.answer = nil },
			health:  ports.PrinterUnknown,
			says:    "n'a rien renvoyé",
		},
		{
			// Vivante, et pas prête : le décodage fin est désactivé (§8.5), donc la trame
			// revenue peut très bien dire PAPER OUT. PrinterReady signifie « a répondu et
			// n'a rien à signaler » ; un poste qui ne sait pas lire le rapport ne peut pas
			// affirmer qu'il est vide.
			name:     "imprimante vivante",
			arrange:  func(r *recorder) { r.queryErr = nil; r.answer = []byte{0x30, 0x41} },
			health:   ports.PrinterUnknown,
			says:     "elle est vivante",
			wantsRaw: true,
		},
		{
			name:    "transport en panne",
			arrange: func(r *recorder) { r.queryErr = errors.New("connexion refusée") },
			health:  ports.PrinterFaulted,
			says:    "n'a pas répondu",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			printer, transport, _ := newPrinter(t, nil)
			c.arrange(transport)

			status := printer.Status(context.Background())
			if status.Health != c.health {
				t.Errorf("Health = %d, attendu %d", status.Health, c.health)
			}
			if !strings.Contains(status.Detail, c.says) {
				t.Errorf("Detail « %s » : il devait contenir « %s ». Il est lu par un bénévole sur "+
					"l'écran de dépannage", status.Detail, c.says)
			}
			if c.wantsRaw && len(status.Raw) == 0 {
				t.Error("Raw est vide : c'est la trame brute affichée en hexa qui permettra de " +
					"compléter le décodage sans se déplacer (§8.5)")
			}
			if len(transport.queries) != 1 || transport.queries[0][0] != 0x05 {
				t.Errorf("interrogations émises : %v, attendu un seul ENQ (0x05)", transport.queries)
			}
		})
	}
}

// --- Life cycle -------------------------------------------------------------

// TestCloseIsIdempotentAndPrintingAfterwardsIsRefused: the Hub closes on a reload and
// again on shutdown (§11.4, §13.4).
func TestCloseIsIdempotentAndPrintingAfterwardsIsRefused(t *testing.T) {
	printer, transport, _ := newPrinter(t, nil)

	for call := 1; call <= 3; call++ {
		if err := printer.Close(); err != nil {
			t.Fatalf("Close, appel %d : %v", call, err)
		}
	}
	if transport.closed != 1 {
		t.Errorf("le transport a été fermé %d fois : Close relâche ce qui a été pris, une fois", transport.closed)
	}
	if _, err := printer.Print(context.Background(), job(t)); err == nil {
		t.Error("une impression a été acceptée après Close")
	}
	if status := printer.Status(context.Background()); status.Health != ports.PrinterUnknown {
		t.Errorf("Health = %d après Close : on ne sait plus rien de l'imprimante", status.Health)
	}
}

// --- Construction -----------------------------------------------------------

// TestNewRefusesAtStartUpWhatWouldFailInFrontOfACustomer: an inconsistency stops the
// process at start-up, never at print time (§11.3).
func TestNewRefusesAtStartUpWhatWouldFailInFrontOfACustomer(t *testing.T) {
	valid := func() Options {
		clk := fake.NewClock(t0)
		return Options{
			Transport: newRecorder(clk),
			Clock:     clk,
			Template:  domain.IdenticalTemplate(),
			Settings:  DefaultSettings(),
		}
	}

	for _, c := range []struct {
		name    string
		spoil   func(*Options)
		says    string
		typed   bool
		partial []string
	}{
		{name: "sans transport", spoil: func(o *Options) { o.Transport = nil }, says: "no transport"},
		{name: "sans horloge", spoil: func(o *Options) { o.Clock = nil }, says: "no clock"},
		{
			name:    "réglages tous faux",
			spoil:   func(o *Options) { o.Settings = Settings{} },
			typed:   true,
			partial: []string{"printer.options.darkness", "printer.options.speed", "printer.options.copies"},
		},
		{
			name:  "gabarit d'une autre tête",
			spoil: func(o *Options) { o.Template.Media.DotsPerMM = 12 },
			typed: true,
			says:  "printer.template",
		},
		{
			name:  "aucun gabarit",
			spoil: func(o *Options) { o.Template = domain.Template{} },
			typed: true,
			says:  "aucune résolution",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			o := valid()
			c.spoil(&o)
			printer, err := New(o)
			if err == nil {
				printer.Close()
				t.Fatal("driver construit : la faute serait tombée devant un client")
			}
			if c.says != "" && !strings.Contains(err.Error(), c.says) {
				t.Errorf("erreur « %v » : elle devait nommer « %s »", err, c.says)
			}
			// Every fault at once: a volunteer who came to fix one file leaves having
			// fixed it (§11.3).
			for _, field := range c.partial {
				if !strings.Contains(err.Error(), field) {
					t.Errorf("erreur « %v » : %s manque, les fautes sont remontées d'un coup", err, field)
				}
			}
			if c.typed {
				var printErr *ports.PrintError
				if !errors.As(err, &printErr) || printErr.Kind != ports.KindConfig {
					t.Errorf("erreur %T : une faute de configuration est une *PrintError{config}", err)
				}
			}
		})
	}
}

// TestAHeadWithoutAWidthIsRefused covers the day somebody adds a model by hand.
func TestAHeadWithoutAWidthIsRefused(t *testing.T) {
	clk := fake.NewClock(t0)
	_, err := New(Options{
		Transport: newRecorder(clk),
		Clock:     clk,
		Template:  domain.IdenticalTemplate(),
		Settings:  DefaultSettings(),
		Head:      Head{DotsPerMM: 8},
	})
	if err == nil {
		t.Fatal("une tête sans largeur a été acceptée : le bloc <G> n'aurait aucune borne")
	}
	if !strings.Contains(err.Error(), "max_width_bytes") {
		t.Errorf("erreur « %v » : elle devait nommer la largeur manquante", err)
	}
}
