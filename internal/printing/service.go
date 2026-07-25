package printing

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// retryDelays is a TABLE, not a formula (§8.2). The 300*(n+1) formula produced 300 ms
// then 600 ms, while both the taxonomy of §8.5 and failure test 4 announce « 300 ms
// then 1 s ». The wait happens BEFORE a retry and never after the last failure.
var retryDelays = []time.Duration{300 * time.Millisecond, 1 * time.Second}

// printBudget is how long one label may take, retries included (§8.2). It is spent on
// the INJECTED clock, never on context.WithTimeout, which reads the real one: that is
// what makes failure test 6 — a printer hanging for 60 s — instantaneous instead of
// burning eight seconds of a suite budgeted at ten (§16.4).
const printBudget = 8 * time.Second

// The two ERR-PRN codes §15.4 names for the printer, allocated here because this is
// where they are emitted. A number cited in a comment is not an allocated number; it
// becomes one the day a constant carries it.
const (
	// codePrinterUnreachable is ERR-PRN-01 — « Imprimante injoignable », the row whose
	// remedy is « Imprimer sur l'imprimante du poste N » (§15.4). The Hub emits the same
	// code when a print fails; it is the same fact seen from two sides, and one code per
	// fact is the point of the table.
	codePrinterUnreachable = "ERR-PRN-01"
	// codeMediaEmpty is ERR-PRN-06 — « Imprimante sans papier », journalled « media-empty
	// », amber Rouleau light, and NEVER a red screen for the customer (§15.4,
	// important-9).
	codeMediaEmpty = "ERR-PRN-06"
)

// Retryable reports whether trying a print again could change the outcome.
//
// It asks the ERROR, and there is now exactly one type to ask: ports.PrintError, the
// §8.5 taxonomy declared where the driver↔station contracts are. It used to exist
// twice, once per driver, and this function reached both through a structural
// interface — which worked, and hid the fact that two identical taxonomies were being
// maintained side by side.
//
// errors.As and not a type assertion: a driver wrapping its refusal in context must
// still be classified, and KindTransient is what the two retries of §8.2 hang on.
//
// An error that classifies nothing is NOT retried, and neither is one whose Kind
// nobody set — KindInternal is the zero value for exactly that reason. A taxonomy
// whose default were « transient » would try a programming mistake three times.
func Retryable(err error) bool {
	var classified *ports.PrintError
	if errors.As(err, &classified) {
		return classified.Retryable()
	}
	return false
}

// ServiceOptions is everything the print service is given.
type ServiceOptions struct {
	// Main is the printer of this station, built from printer.type over
	// printer.options.transport.
	Main ports.Printer
	// MainName is what the screen calls it, in French — typically the print queue,
	// « SATO WS408_2 ». Empty falls back on the driver's own label.
	MainName string

	// Fallback is the printer of the neighbouring station, built from
	// printer.options.fallback (§8.4, bloquant-8). Nil when none is configured, which is
	// the shipped state.
	Fallback ports.Printer
	// FallbackName is what the banner calls it, in French. It is REQUIRED as soon as
	// Fallback is set: a permanent banner that cannot name where the labels are coming
	// out sends a volunteer looking at four printers.
	FallbackName string

	// Clock times the retries and the budget. Required (§5.3).
	Clock ports.Clock
	// Queue is the level N2 probe. Nil is legitimate and honest — see QueueProbe.
	Queue QueueProbe
	// Roll is the roll counter. Nil builds one with no persistence, so that no code path
	// of this file has to check.
	Roll *RollCounter
	// Log is the technical journal. Nil discards.
	Log TechnicalLog
}

// Service is the printer of this station as the station OPERATES it: one driver, an
// optional fallback printer, a roll counter, the three self-tests of §8.6 and the three
// status levels of §8.5.
//
// It satisfies ports.Printer, and that is the whole shape of it: the Hub prints, and
// everything this type adds — counting the roll, routing to the neighbour, telling N1
// from N2 from N3 — is invisible from up there. Nothing above this line has to learn
// that a fallback exists.
//
// What it does NOT do is decide anything about a label. The job arrives complete,
// amounts and barcode included (§8.2), and this file never looks inside it.
type Service struct {
	main         ports.Printer
	mainName     string
	fallback     ports.Printer
	fallbackName string

	clock ports.Clock
	queue QueueProbe
	roll  *RollCounter
	log   TechnicalLog

	// printMu serialises the DEVICE: one label at a time, never interleaved (§8.2).
	// What the legacy application had instead was an
	// `If AllReports("EtataImprimer").IsLoaded Then Exit Sub` that silently ABANDONED
	// the weighing.
	printMu sync.Mutex

	// stateMu guards what is READ by the screens, and it is deliberately not printMu: a
	// dashboard asking which printer is in use must not queue behind a printer that is
	// hanging for its full eight seconds.
	stateMu    sync.Mutex
	onFallback bool
	closed     bool
	seen       Observations
	report     StatusReport
}

var _ ports.Printer = (*Service)(nil)

// NewService wires the service to its printers, its clock and its counters.
//
// It refuses at CONSTRUCTION what would otherwise fail with a customer standing at the
// scale, and the messages about a missing collaborator are English: no configuration
// file can produce a nil printer, so only a developer can ever read them. The one
// refusal an operator can cause — a fallback with no name — is French, because
// printer.options.fallback is a block they edit.
func NewService(o ServiceOptions) (*Service, error) {
	if o.Main == nil {
		return nil, fmt.Errorf("printing: NewService: no main printer")
	}
	if o.Clock == nil {
		return nil, fmt.Errorf("printing: NewService: no clock; the print budget is spent on the INJECTED clock (§5.3)")
	}
	if o.Fallback != nil && o.FallbackName == "" {
		return nil, fmt.Errorf("l'imprimante de secours n'a pas de nom : le bandeau permanent doit " +
			"pouvoir dire sur quelle imprimante les étiquettes sortent (printer.options.fallback.queue)")
	}
	log := o.Log
	if log == nil {
		log = nopLog{}
	}
	name := o.MainName
	if name == "" {
		name = o.Main.Descriptor().Label
	}
	roll := o.Roll
	if roll == nil {
		roll = NewRollCounter(nil, 0, log)
	}
	return &Service{
		main:         o.Main,
		mainName:     name,
		fallback:     o.Fallback,
		fallbackName: o.FallbackName,
		clock:        o.Clock,
		queue:        o.Queue,
		roll:         roll,
		log:          log,
		report:       Assess(Observations{}),
	}, nil
}

// Descriptor reports the driver in use. It changes with the routing, because the
// administration screen showing « rendu image » while the labels leave through the
// neighbour's driver would be showing the wrong machine.
func (s *Service) Descriptor() domain.PrinterDescriptor { return s.target().Descriptor() }

// Print sends one label and retries only transient failures (§8.2).
//
// It returns when the BYTES HAVE BEEN ACCEPTED, not when a label comes out: no
// transport guarantees the latter, which is why the customer screen says « Étiquette
// envoyée à l'imprimante » and why the reprint bar is permanent (important-7).
//
// # What this method never does
//
// It never asks for a status once a print has succeeded. That is important-9 made
// structural rather than remembered: « on ne transforme jamais un succès en erreur ».
// The end of a roll is the case that costs — the last label comes out, THEN the printer
// says media-empty — and the customer used to get a valid label and a red screen, so
// they stuck two on or weighed again, and the till counted twice. Here the receipt is
// returned and the consumable state reaches the screen through Observe, as an amber
// maintenance light, with the weighing still a success.
//
// It never asks the roll counter for permission either. See RollCounter.
func (s *Service) Print(ctx context.Context, job ports.PrintJob) (ports.PrintReceipt, error) {
	s.printMu.Lock()
	defer s.printMu.Unlock()
	if s.isClosed() {
		return ports.PrintReceipt{}, errors.New("l'imprimante a été fermée : ce poste ne peut plus " +
			"imprimer sans redémarrer")
	}

	ctx, cancel := ports.WithBudget(ctx, s.clock, printBudget)
	defer cancel()

	target, name := s.target(), s.routedName()
	var lastErr error
	for attempt := 0; ; attempt++ {
		receipt, err := target.Print(ctx, job)
		if err == nil {
			s.observeWrite(&WriteOutcome{OK: true, Detail: name})
			s.roll.Printed(ctx, labelsOf(job))
			return receipt, nil
		}
		lastErr = err
		if !Retryable(err) || attempt >= len(retryDelays) {
			break // permanent failure, or third and last attempt: leave WITHOUT waiting
		}
		select {
		case <-s.clock.After(retryDelays[attempt]): // 300 ms, then 1 s
		case <-ctx.Done():
			s.observeWrite(&WriteOutcome{Detail: fmt.Sprintf("%s : %v", name, ctx.Err())})
			return ports.PrintReceipt{}, ctx.Err()
		}
	}
	s.observeWrite(&WriteOutcome{Detail: fmt.Sprintf("%s : %v", name, lastErr)})
	return ports.PrintReceipt{}, lastErr
}

// labelsOf reports how many labels a job puts on the roll.
//
// A job that names NO count gets one. The driver would substitute
// printer.options.copies, but that figure is not on the receipt and this service never
// reads another package's configuration — on the shipped file it is 1 (§11.2), so the
// count is exact today. The counter is built to be recalibrated by hand anyway, which
// is what makes an approximation here acceptable and a veto unacceptable.
func labelsOf(job ports.PrintJob) int64 {
	if job.Copies > 1 {
		return int64(job.Copies)
	}
	return 1
}

// SelfTest prints one of the three built-in patterns of §8.6.
//
// The name is checked against the catalogue rather than against a switch, so that the
// administration screen, the HTTP route and this method cannot drift apart — and so
// that the two patterns A2 deleted get an answer that says why they are gone.
//
// A self-test burns a label like any other print, so it counts against the roll.
func (s *Service) SelfTest(ctx context.Context, what string) error {
	test, err := LookupSelfTest(what)
	if err != nil {
		return err
	}
	s.printMu.Lock()
	defer s.printMu.Unlock()
	if s.isClosed() {
		return errors.New("l'imprimante a été fermée : ce poste ne peut plus imprimer sans redémarrer")
	}

	target, name := s.target(), s.routedName()
	if err := target.SelfTest(ctx, string(test.ID)); err != nil {
		s.observeWrite(&WriteOutcome{Detail: fmt.Sprintf("%s : %v", name, err)})
		return err
	}
	s.observeWrite(&WriteOutcome{OK: true, Detail: name})
	s.roll.Printed(ctx, 1)
	return nil
}

// Status reports what the station may say about its printer, in the shape the Hub
// consumes. It is Observe, converted.
func (s *Service) Status(ctx context.Context) ports.PrinterStatus { return s.Observe(ctx).Status() }

// Observe asks levels N2 and N3 what they can see, combines them with what level N1
// last recorded, and reports the conclusion (§8.5).
//
// It journals on a CHANGE of health and never on a repetition: §8.5 has the status
// re-read every ten seconds while a consumable is out, and six identical lines a minute
// is how a journal stops being read.
func (s *Service) Observe(ctx context.Context) StatusReport {
	// The two probes run OUTSIDE the lock: a driver whose transport is hanging must not
	// hold the dashboard, and ports.Printer.Status is already serialised by the driver.
	var queue *Queue
	if s.queue != nil {
		if q, err := s.queue.Queue(ctx); err == nil {
			queue = &q
		}
	}
	native := nativeFrom(s.target().Status(ctx))

	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.seen.Queue, s.seen.Native = queue, native
	return s.conclude()
}

// Report returns the last conclusion without touching a device. It is what the
// dashboard renders on every refresh and what every state broadcast carries.
func (s *Service) Report() StatusReport {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.report
}

// Roll gives the roll counter, so that the troubleshooting screen can reach « J'ai
// changé le rouleau » and the recalibration behind it.
func (s *Service) Roll() *RollCounter { return s.roll }

// Routing is which printer the labels are coming out of, and what the screen says about
// it.
type Routing struct {
	// Fallback reports that the station is on the neighbour's printer.
	Fallback bool
	// Name is the FRENCH name of the printer in use.
	Name string
	// Banner is the PERMANENT banner of §8.4, in French. Empty on the main printer:
	// there is nothing to warn about when everything is where it belongs.
	Banner string
	// Available reports that a fallback is configured at all, which is what decides
	// whether the button « Imprimer sur l'imprimante du poste N » is offered (§14.4).
	Available bool
}

// Routing reports which printer is in use.
func (s *Service) Routing() Routing {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	r := Routing{Fallback: s.onFallback, Name: s.mainName, Available: s.fallback != nil}
	if s.onFallback {
		r.Name = s.fallbackName
		r.Banner = fmt.Sprintf("Les étiquettes sortent sur l'imprimante de secours (%s).", s.fallbackName)
	}
	return r
}

// UseFallback routes printing to the fallback printer FOR THE CURRENT SESSION (§8.4,
// bloquant-8).
//
// # Asked for, never automatic — and §8.4 is the one that decides
//
// The document describes an explicit button on the troubleshooting screen, « Imprimer
// sur l'imprimante du poste N », and a permanent banner. It is worth saying why that is
// the right call rather than a timid one, because « switch automatically when the main
// printer fails » sounds like a service.
//
// Nothing observable would trigger it honestly. What the station can see is a write
// that failed, and a write fails on a cable knocked loose for two seconds as readily as
// on a dead printer (important-7 is the same lesson from the other end: we do not
// confirm a physical event with a probe that does not observe it). An automatic switch
// would therefore move a customer's label two metres away, silently, on a transient —
// and the customer is standing at THIS station, watching a slot that stays empty.
//
// And it does not scale down the way it must. The four printers of the parc are two
// metres apart and each is the fallback of its neighbour; a network hiccup that touches
// all four would pile all four stations onto one printer, which is how a bad afternoon
// becomes a closed shop.
//
// So the switch is a human decision, taken by someone who has looked at the printer,
// and the banner is permanent because the same human has to remember to come back.
func (s *Service) UseFallback(ctx context.Context) error {
	s.stateMu.Lock()
	if s.fallback == nil {
		s.stateMu.Unlock()
		return errors.New("aucune imprimante de secours n'est configurée sur ce poste : " +
			"renseignez printer.options.fallback (transport et file de l'imprimante voisine)")
	}
	if s.onFallback {
		s.stateMu.Unlock()
		return nil
	}
	s.onFallback = true
	s.forget()
	s.stateMu.Unlock()

	s.log.Technical(domain.LevelWarn, "printer", "",
		fmt.Sprintf("Les étiquettes sont basculées sur l'imprimante de secours (%s).", s.fallbackName),
		"bascule demandée depuis l'écran de dépannage ; elle dure jusqu'au retour explicite ou "+
			"jusqu'au redémarrage du service")
	s.observeQueueAfterSwitch(ctx)
	return nil
}

// UseMain routes printing back to the main printer.
//
// Also asked for, and for the mirror reason: NOTHING tells this station that the main
// printer has been fixed. Level N1 cannot — it has not written to it since the switch —
// and the person who changed the roll or plugged the cable back in is the only one who
// knows. An automatic return would put the banner out while the labels were still
// coming out of the neighbour's printer, which is the one sentence a volunteer relies
// on to know where to walk.
func (s *Service) UseMain(ctx context.Context) error {
	s.stateMu.Lock()
	if !s.onFallback {
		s.stateMu.Unlock()
		return nil
	}
	s.onFallback = false
	s.forget()
	s.stateMu.Unlock()

	s.log.Technical(domain.LevelInfo, "printer", "",
		fmt.Sprintf("Les étiquettes repassent sur l'imprimante du poste (%s).", s.mainName),
		"retour demandé depuis l'écran de dépannage")
	s.observeQueueAfterSwitch(ctx)
	return nil
}

// Close releases both printers. It is idempotent: the Hub closes on a configuration
// reload and again on shutdown (§11.4, §13.4), and a handle already released is not
// news.
func (s *Service) Close() error {
	s.stateMu.Lock()
	if s.closed {
		s.stateMu.Unlock()
		return nil
	}
	s.closed = true
	s.stateMu.Unlock()

	err := s.main.Close()
	if s.fallback != nil {
		if fallbackErr := s.fallback.Close(); err == nil {
			err = fallbackErr
		}
	}
	return err
}

// target is the printer the labels are going to right now.
func (s *Service) target() ports.Printer {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.onFallback {
		return s.fallback
	}
	return s.main
}

// routedName is the French name of that printer.
func (s *Service) routedName() string {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.onFallback {
		return s.fallbackName
	}
	return s.mainName
}

// isClosed reports whether Close has run.
func (s *Service) isClosed() bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.closed
}

// observeWrite records what level N1 just saw and re-concludes.
func (s *Service) observeWrite(w *WriteOutcome) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.seen.Write = w
	s.conclude()
}

// observeQueueAfterSwitch re-reads the levels that can answer immediately, so that the
// screen does not keep showing LevelNone until the next label.
func (s *Service) observeQueueAfterSwitch(ctx context.Context) { s.Observe(ctx) }

// forget drops every observation. It is called on both switches, and it is the honest
// half of the routing: what this station knew about one printer says NOTHING about
// another one, and carrying a green light across the switch would be inventing a
// measurement. The report goes back to LevelNone until something is observed.
//
// The caller holds stateMu.
func (s *Service) forget() {
	s.seen = Observations{}
	s.conclude()
}

// conclude re-runs the assessment and journals a CHANGE of health. The caller holds
// stateMu.
func (s *Service) conclude() StatusReport {
	previous := s.report
	s.report = Assess(s.seen)
	if s.report.Health != previous.Health {
		s.journal(s.report)
	}
	return s.report
}

// journal writes one line for a health that has just changed.
//
// Two codes, both named by §15.4, and nothing invented for the rest: a printer that
// comes back is an `info` line with no code, because there is no failure to name.
//
// It runs while stateMu is held, which puts ONE requirement on a TechnicalLog: it must
// not call back into this service. The shipped one cannot — it is a bounded channel
// served by a worker, so that a saturated journal degrades the JOURNAL and never the
// service (ADR-013) — and that is the property being relied on here.
func (s *Service) journal(r StatusReport) {
	detail := fmt.Sprintf("niveau %s", r.Level)
	switch r.Health {
	case ports.PrinterFaulted:
		s.log.Technical(domain.LevelError, "printer", codePrinterUnreachable, r.Detail, detail)
	case ports.PrinterConsumable:
		s.log.Technical(domain.LevelWarn, "printer", codeMediaEmpty, r.Detail, detail)
	case ports.PrinterReady:
		s.log.Technical(domain.LevelInfo, "printer", "", r.Detail, detail)
	}
}
