package station

import (
	"context"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// printBudget is how long one label may take.
//
// It is spent on the INJECTED clock and not on context.WithTimeout, which reads
// the real one: that is what makes failure test 6 — a printer hanging for sixty
// seconds — instantaneous instead of burning eight seconds of a suite budgeted at
// ten (§16.4).
const printBudget = 8 * time.Second

// purgeEvery is how many weighings pass between two journal purges (§4, step 16).
//
// Once in fifty, and not on a timer: a purge that happens on the clock happens
// while a customer is weighing, whereas one that happens every fiftieth row
// happens exactly as often as the journal grows.
const purgeEvery = 50

// TechnicalEntry is one line of the technical journal, on its way to the store.
//
// It carries its own instant, read from the injected clock AT THE MOMENT OF THE
// EVENT: the worker may write it much later, and a journal that dates an event by
// when it was persisted is a journal nobody can correlate with anything.
type TechnicalEntry struct {
	At      time.Time
	Level   string
	Source  string
	Code    string
	Message string
	Detail  string
}

// Journal is where the station's records go. It is declared HERE, on the
// consumer's side (cut 3 of §5.2), and *store.DB satisfies it as it stands.
type Journal interface {
	// RecordWeighing persists one weighing and its price lines.
	RecordWeighing(ctx context.Context, w *domain.Weighing) error
	// PurgeWeighings trims the journal to its retention and reports how many rows
	// went.
	PurgeWeighings(ctx context.Context) (int64, error)
}

// TechnicalSink is where a technical line is persisted. Declared HERE, on the
// consumer's side, so that internal/station names no storage type.
//
// It takes the whole entry and not the five strings of ports.TechnicalLog for one
// reason: the INSTANT. A line is stamped when the event happened, by the injected
// clock, and written whenever the worker gets to it; a journal that dates an event
// by when it was persisted cannot be correlated with anything (ADR-013).
type TechnicalSink interface {
	// RecordTechnical appends one line to the technical journal.
	RecordTechnical(ctx context.Context, e TechnicalEntry) error
}

// printWorker turns one label at a time into bytes on a device.
//
// It DIES BY THE CLOSURE OF ITS CHANNEL (§13.1), never by a cancelled context, and
// the difference is the whole of the shutdown: the root context is cancelled first
// and the workers are drained afterwards, so a job in flight must not be killed by
// the cancellation that started the shutdown.
type printWorker struct {
	printer  ports.Printer
	clock    ports.Clock
	results  chan<- PrintResult
	hubDone  <-chan struct{}
	counters *Counters
	finished chan struct{}
}

// run serves jobs until the channel is closed.
//
// # What it deliberately does NOT do
//
// It does not retry. The retry policy of §8.5 — KindTransient only, twice, at
// 300 ms then 1 s — lives in printing.Service.Print, which is the ports.Printer
// this worker is handed on a real station, and which already spends it on the
// injected clock. A second retry loop here would turn three attempts into nine and
// a 1.3 s failure into a four-second one, for a customer standing at the scale.
// The worker's job is to keep the Hub off the device, and that is all it does.
func (w *printWorker) run(jobs <-chan job) {
	defer close(w.finished)
	for j := range jobs {
		w.send(w.print(j))
	}
}

// print sends one job and measures it on the injected clock.
func (w *printWorker) print(j job) PrintResult {
	// context.Background and NOT the root context: the shutdown cancels the root
	// first and drains the workers afterwards, so a job in flight has to survive
	// the cancellation long enough to finish (§13.4).
	ctx, cancel := ports.WithBudget(context.Background(), w.clock, printBudget)
	defer cancel()

	started := w.clock.Now()
	w.counters.PrintJobs.Add(1)
	_, err := w.printer.Print(ctx, ports.PrintJob{
		Label:    j.Label,
		Template: j.Template,
		Locale:   j.Locale,
		Copies:   j.Copies,
	})
	if err != nil {
		w.counters.PrintFailures.Add(1)
	}
	return PrintResult{
		JobID:    j.Label.JobID,
		Err:      err,
		Duration: w.clock.Now().Sub(started),
	}
}

// send hands the result back without ever blocking for good.
//
// The Hub reads printResults in its select; if the loop has already returned,
// nobody will, and a worker stuck on that send would make the drain of §13.4 hang
// for its whole budget.
func (w *printWorker) send(r PrintResult) {
	select {
	case w.results <- r:
	case <-w.hubDone:
	}
}

// journalWorker writes the records, OUT of the weighing path.
//
// It serves two channels and dies when the WEIGHINGS one is closed: that channel
// has a single writer — the Hub loop, which has already returned by the time the
// shutdown closes it — whereas the technical channel is written by every driver
// goroutine and is therefore never closed at all. A late technical line finds a
// worker that is gone, falls into the non-blocking default of logTechnical and is
// counted; it can never be a send on a closed channel.
type journalWorker struct {
	journal   Journal
	technical TechnicalSink
	counters  *Counters
	// spare is the RAM safety net of ADR-013, and it is written HERE as well as on
	// the saturated channel, because a full disk is the case failure test 7 names
	// and it never saturates anything: the store answers « no space left » in
	// microseconds, so the weighing would be counted and then forgotten. The
	// counter says HOW MANY were lost; only the ring says WHICH ONES.
	spare    *ring
	log      func(level, source, code, message, detail string)
	finished chan struct{}
	written  int
}

// run serves both channels until the weighing channel is closed and drained.
func (w *journalWorker) run(weighings <-chan domain.Weighing, technical <-chan TechnicalEntry) {
	defer close(w.finished)
	for {
		select {
		case entry, ok := <-weighings:
			if !ok {
				w.drainTechnical(technical)
				return
			}
			w.record(entry)
		case entry := <-technical:
			w.write(entry)
		}
	}
}

// drainTechnical empties what is already in the technical channel before leaving.
//
// The lines that are there were produced while the station was still running, and
// losing them is losing exactly the ones that explain a shutdown.
func (w *journalWorker) drainTechnical(technical <-chan TechnicalEntry) {
	for {
		select {
		case entry := <-technical:
			w.write(entry)
		default:
			return
		}
	}
}

// record persists one weighing, and keeps it when it cannot.
//
// A failure here NEVER reaches a customer: the label came out before this line ran.
// What is left to decide is what becomes of the row, and the answer is the same as
// for a saturated channel — the RAM ring and the counter the dashboard shows in
// red. A disk that is full, a database locked past its busy_timeout and a channel
// nobody drains lose the same row for the same customer; making only one of the
// three recoverable would be a distinction the dashboard cannot explain.
func (w *journalWorker) record(entry domain.Weighing) {
	if err := w.journal.RecordWeighing(context.Background(), &entry); err != nil {
		w.counters.JournalFailures.Add(1)
		w.counters.UnloggedWeighings.Add(1)
		if w.spare != nil {
			w.spare.Add(entry)
		}
		w.log(domain.LevelError, "system", "ERR-SYS-05",
			"Pesée non journalisée.", err.Error())
		return
	}
	w.counters.JournalWrites.Add(1)
	w.written++
	if w.written%purgeEvery == 0 {
		if _, err := w.journal.PurgeWeighings(context.Background()); err != nil {
			w.log(domain.LevelWarn, "system", "ERR-SYS-05",
				"Purge du journal impossible.", err.Error())
		}
	}
}

// write persists one technical line.
//
// It cannot journal its own failure — that would be a loop — so it counts it and
// says nothing. The RAM ring of §12.1 is what survives a database that has stopped
// accepting writes.
func (w *journalWorker) write(entry TechnicalEntry) {
	if w.technical == nil {
		return
	}
	if err := w.technical.RecordTechnical(context.Background(), entry); err != nil {
		w.counters.JournalFailures.Add(1)
	}
}

// --- Le sondage des versions publiées ------------------------------------------

// updateGracePeriod is how long a station waits after starting before it looks for
// a newer version of itself.
//
// Five minutes and not zero: a station that has just booted is opening a serial
// port, reading a catalogue and drawing a screen, and none of that is helped by an
// outbound request starting at the same instant.
const updateGracePeriod = 5 * time.Minute

// updatePeriod is how often it looks afterwards.
const updatePeriod = 24 * time.Hour

// Poller is what the station asks, once a day, whether a newer version exists.
//
// It is declared HERE, on the consumer's side, and it names no type of the update
// package: internal/station has no business knowing what a release is. The version
// comes back as a plain string, and the adapter that turns an update.Service into
// this lives in the composition root.
type Poller interface {
	// Poll asks the repository whether a newer version exists and records the
	// answer, returning the newest version published or the empty string.
	Poll(ctx context.Context, repository string) (string, error)
}

// runUpdateWorker asks the repository, once a day, whether something newer exists.
//
// IT DOWNLOADS NOTHING. It reads a few kilobytes of JSON; the archive comes down
// only when somebody touches the button. Four stations polling once a day sit far
// below the sixty-requests-an-hour anonymous limit.
//
// A FAILED POLL LIGHTS NOTHING. A shop whose line is down is not a station in
// breakdown, and an amber light there would teach volunteers to ignore amber
// lights. It is written down at warn, because a station that has silently stopped
// seeing new versions for six months is exactly what this feature exists to
// prevent -- but it is written down, not shown.
// THE TWO TIMERS ARE REGISTERED BY Start, in the calling goroutine, and handed
// down -- the same discipline the hub ticker and the supervisor ticker follow, and
// for the same reason. Registering them here would make the first poll depend on
// when the scheduler got to this goroutine: on a fake clock that is the difference
// between a deterministic test and a flaky one, and it was measured, not guessed.
func (s *Station) runUpdateWorker(ctx context.Context, grace <-chan time.Time,
	ticks <-chan time.Time, stop func(), poller Poller) {

	defer stop()
	select {
	case <-ctx.Done():
		return
	case <-grace:
	}
	s.pollForUpdate(ctx, poller)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			s.pollForUpdate(ctx, poller)
		}
	}
}

// pollForUpdate asks once, and never lets a refusal escape.
func (s *Station) pollForUpdate(ctx context.Context, poller Poller) {
	repository := s.hub.Config().Update.Repository
	version, err := poller.Poll(ctx, repository)
	if err != nil {
		// No ERR code: this is not a fault of the station, and giving it one would
		// file it beside a printer that has stopped.
		s.hub.logTechnical(domain.LevelWarn, "update", "",
			"Impossible de joindre le serveur des versions.", err.Error())
		return
	}
	s.hub.logTechnical(domain.LevelInfo, "update", "",
		"Version publiée la plus récente : "+version, repository)
}
