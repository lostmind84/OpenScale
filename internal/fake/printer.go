package fake

import (
	"context"
	"sync"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// Printer is a label printer a test reads the jobs out of.
//
// It accepts everything by default, because that is the nominal case and a double
// tap that produces two jobs must fail on the COUNT of jobs and not on a setup
// step somebody forgot.
type Printer struct {
	mu       sync.Mutex
	jobs     []ports.PrintJob
	selfTest []string
	err      error
	status   ports.PrinterStatus
	closed   bool
	// holds is closed by Release. While it is open, Print waits on it or on its
	// context, which is how « imprimante qui pend 60 s » is reproduced without
	// waiting sixty seconds.
	holds chan struct{}
	// printed is signalled after every accepted job, so that a test can wait for
	// the worker without a sleep.
	printed chan struct{}
}

var _ ports.Printer = (*Printer)(nil)

// NewPrinter returns a printer that accepts every job.
func NewPrinter() *Printer {
	return &Printer{
		printed: make(chan struct{}, 1024),
		status:  ports.PrinterStatus{Health: ports.PrinterReady, Detail: "Imprimante prête."},
	}
}

// Fail makes every subsequent Print return err.
func (p *Printer) Fail(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.err = err
}

// Hang makes Print block until Release is called or its context is done.
func (p *Printer) Hang() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.holds = make(chan struct{})
}

// Release lets a hanging Print return.
func (p *Printer) Release() {
	p.mu.Lock()
	holds := p.holds
	p.holds = nil
	p.mu.Unlock()
	if holds != nil {
		close(holds)
	}
}

// SetStatus is what the device will say about itself next time it is asked.
func (p *Printer) SetStatus(s ports.PrinterStatus) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status = s
}

// Descriptor reports the driver identity and what it can do.
func (p *Printer) Descriptor() domain.PrinterDescriptor {
	return domain.PrinterDescriptor{
		ID: "preview", Label: "Imprimante factice",
		Capabilities: domain.PrinterCapabilities{Raster: true, MaxCopies: 9, DotsPerMM: 8},
	}
}

// Print records the job, or fails the way the test asked it to.
func (p *Printer) Print(ctx context.Context, job ports.PrintJob) (ports.PrintReceipt, error) {
	p.mu.Lock()
	holds, err := p.holds, p.err
	p.mu.Unlock()

	if holds != nil {
		select {
		case <-holds:
		case <-ctx.Done():
			return ports.PrintReceipt{}, ctx.Err()
		}
	}
	if err != nil {
		return ports.PrintReceipt{}, err
	}

	p.mu.Lock()
	p.jobs = append(p.jobs, job)
	p.mu.Unlock()

	select {
	case p.printed <- struct{}{}:
	default:
	}
	return ports.PrintReceipt{JobID: job.Label.JobID, Bytes: 16384}, nil
}

// Jobs returns a COPY of what was printed, oldest first.
func (p *Printer) Jobs() []ports.PrintJob {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]ports.PrintJob, len(p.jobs))
	copy(out, p.jobs)
	return out
}

// Printed is signalled once per accepted job.
func (p *Printer) Printed() <-chan struct{} { return p.printed }

// Status reports what the device says about itself.
func (p *Printer) Status(context.Context) ports.PrinterStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

// SelfTest records the pattern it was asked for.
func (p *Printer) SelfTest(_ context.Context, what string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.selfTest = append(p.selfTest, what)
	return p.err
}

// SelfTests returns the patterns that were asked for.
func (p *Printer) SelfTests() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.selfTest))
	copy(out, p.selfTest)
	return out
}

// Close releases the device. It is idempotent.
func (p *Printer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

// Closed reports whether Close has run.
func (p *Printer) Closed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}
