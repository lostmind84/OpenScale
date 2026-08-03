package printing

// This file is the printer of this station as the station OPERATES it: one job at a
// time, two retries, the three status levels of §8.5 and the roll behind them. WHICH
// printer a label comes out of is the other half, and it is in routing.go.

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
