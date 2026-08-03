package station

import (
	"context"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// This file is what a station is GIVEN: the factories that build a driver out of a
// configuration, the collaborators the shutdown releases, and the Options that carry
// them all. Nothing here decides anything — station.go turns it into a Station.

// ScaleFactory builds the scale driver a configuration names.
//
// It is INJECTED because internal/station knows no concrete driver: adding a scale
// is one package and one line in cmd/openscale/drivers.go, with zero modification
// here (cut 2 of §5.2).
type ScaleFactory func(cfg domain.Config) (ports.Scale, error)

// PrinterFactory builds the printer a configuration names.
type PrinterFactory func(cfg domain.Config) (ports.Printer, error)

// CatalogSourceFactory builds the catalog source a configuration names.
type CatalogSourceFactory func(cfg domain.Config) (ports.CatalogSource, error)

// CatalogApplier turns the batch a source produced into the snapshot that will
// take service, and says what to acknowledge.
//
// It is a hook and not a hard-coded step because the qualification of §10.3 and
// the guards of §10.4 — an amputated catalog must not replace a healthy one —
// belong to internal/catalog, which this package does not import. The default
// builds the snapshot and nothing else.
type CatalogApplier func(ctx context.Context, cfg domain.Config, b *ports.Batch) (*domain.Catalog, ports.BatchResult, error)

// CatalogBatch is a whole catalog waiting to take service.
//
// It carries what produced it so that the dashboard can say « Catalogue du
// 24/07/2026 » without asking the store.
type CatalogBatch struct {
	Catalog  *domain.Catalog
	Source   string
	FileName string
	// ImportedAt is the instant of the import that PRODUCED this catalog — the
	// occurred_at of its row in the imports table, and never the instant of the swap.
	//
	// The two differ by up to MaxSwitchIdle, because a catalog waits for a station
	// nobody is touching (§10.8). Stamping the swap made the same catalog carry one
	// date in service and another after the next restart, which reads it back from the
	// base. One catalog, one instant.
	ImportedAt time.Time
}

// Server is the part of an HTTP server the shutdown needs. Declared here, on the
// consumer's side, so that internal/station imports no net/http.
type Server interface {
	// Shutdown stops accepting and waits for the active requests, up to ctx.
	Shutdown(ctx context.Context) error
}

// Closer is the part of the store, and of anything else with a handle, that the
// shutdown needs.
type Closer interface {
	Close() error
}

// Waiter is something the shutdown waits for before closing what it writes to —
// an import transaction that has to roll back, typically.
type Waiter interface {
	Wait()
}

// Options is everything a station is given. Clock, Config, Printer and Journal
// are required; the rest has an honest default.
type Options struct {
	Clock  ports.Clock
	Config domain.Config
	// Catalog is the snapshot already in the store, or nil on a virgin station. Nil
	// starts the machine in Initializing, which is what makes the grid say
	// « Catalogue vide » instead of showing nothing.
	Catalog *domain.Catalog
	// CatalogAt is when that snapshot was IMPORTED — the instant of the last import
	// the store applied, read back from the base by the composition root.
	//
	// It is handed in rather than taken from the clock, and that is the defect this
	// field was added for: a station stamps the catalog it starts with, so reading the
	// clock here dated every catalog from the last reboot. §14.3 shows this instant
	// permanently to answer « ces prix datent de quand ? », and a date that a service
	// restart moves answers a question nobody asked.
	CatalogAt time.Time
	// OutOfService starts the station in the one terminal state, which is what an
	// unusable configuration does (§11.3, ERR-CFG-01).
	OutOfService bool
	// Registries carries the driver descriptors, and it is here for ONE question: is
	// the configuration that just arrived still unusable?
	//
	// A station started out of service is repaired from the administration screen, block
	// by block, and it comes back into service the moment the last fault goes — which is
	// what §11.4 promises when it says no configuration block requires a restart. Left at
	// its zero value, no driver is known, every configuration carries faults, and the
	// station never returns: that is the safe default for a caller that never had a reason
	// to be out of service in the first place.
	Registries domain.Registries
	// Poller is the daily check for a newer version of this binary. Nil starts no
	// worker at all, which is what a binary that cannot update itself honestly is
	// -- a development build, or a platform with no swap.
	Poller Poller
	// Templates resolves printer.template. It defaults to the shipped ones.
	Templates map[string]domain.Template
	// NominalRate is the cadence the scale driver DECLARES, used until the rate
	// meter has eight intervals of its own.
	NominalRate time.Duration
	Counters    *Counters

	Scale         ports.Scale
	Printer       ports.Printer
	CatalogSource ports.CatalogSource
	Journal       Journal
	TechnicalSink TechnicalSink

	NewScale         ScaleFactory
	NewPrinter       PrinterFactory
	NewCatalogSource CatalogSourceFactory
	ApplyCatalog     CatalogApplier

	Server Server
	Store  Closer
	// CatalogWait rolls an import transaction back before the database closes.
	CatalogWait Waiter
	// OnRevert is called when the 60 s window of §11.4 closed without a confirmation, and it
	// receives THE FILE AS IT WAS BEFORE THE SAVE — never the configuration the station was
	// running.
	//
	// It exists because the countdown protects the RUNNING station and the file is written
	// before it starts: without this hook, a station that rolled back would come back, at
	// the next restart, on the very configuration nobody confirmed — which is exactly the
	// branch the countdown was cutting. What it does is the caller's business; internal/
	// station knows no file.
	//
	// The two documents are distinct on the one station this matters for. A station whose
	// configuration is unusable RUNS the neutral profile (§11.3) while its file keeps the
	// cooperative's tariffs, safeguards and categories: handing the running configuration
	// over here wrote the factory profile onto that file, on the very save that repaired it.
	OnRevert func(fileBefore domain.Config)
}
