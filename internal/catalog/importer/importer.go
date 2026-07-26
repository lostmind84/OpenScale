package importer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"openscale/internal/catalog"
	"openscale/internal/domain"
	"openscale/internal/station/ports"
	"openscale/internal/store"
)

// Records is the part of the station's database an import reads and writes, declared
// on the CONSUMER's side (cut 3 of §5.2).
//
// Six calls, named one by one, so that a test can drive the applier against the REAL
// store — the only kind of test that proves a transaction is a transaction — while the
// package still names what it depends on rather than a whole database.
type Records interface {
	catalog.FailureLedger
	// LastAppliedImport answers the only question §10.5 asks of the history.
	LastAppliedImport(ctx context.Context) (domain.Import, error)
	// RecordImport appends one import that changed no product.
	RecordImport(ctx context.Context, imp domain.Import, findings []domain.Finding) (int64, error)
	// ReplaceCatalog applies one import in a single transaction.
	ReplaceCatalog(ctx context.Context, batch store.Batch) (store.ImportOutcome, error)
	// LoadCatalog builds the snapshot the Hub publishes.
	LoadCatalog(ctx context.Context) (*domain.Catalog, error)
}

// defaultMaxWeighableDrop is the shipped catalog.options.max_weighable_drop (§11.2).
//
// A missing key must never mean "no guard": a station whose configuration lost that
// line keeps the one the specification ships with.
const defaultMaxWeighableDrop = 0.1

// Options is everything an applier is handed, and nothing it could invent.
type Options struct {
	Records Records
	// Clock stamps the import row and measures its duration. No import reads time.Now.
	Clock ports.Clock
	// Log is where the outcome of an import goes. Nil discards, so no caller checks.
	Log ports.TechnicalLog
}

// Applier turns a batch into the catalog in service, or refuses it.
//
// It satisfies station.CatalogApplier without importing internal/station: the hook is a
// function type, and Apply has its shape.
type Applier struct {
	records Records
	clock   ports.Clock
	log     ports.TechnicalLog
}

// New builds an applier.
//
// Both refusals are composition mistakes with no operator input in them, and both are
// worth a sentence rather than a nil pointer at the first import.
func New(o Options) (*Applier, error) {
	switch {
	case o.Records == nil:
		return nil, errors.New("importer : un applicateur reçoit l'historique des imports du poste")
	case o.Clock == nil:
		return nil, errors.New("importer : un applicateur reçoit une horloge, jamais time.Now")
	}
	log := o.Log
	if log == nil {
		log = ports.NopTechnicalLog{}
	}
	return &Applier{records: o.Records, clock: o.Clock, log: log}, nil
}

// Apply decides what becomes of one batch and says what to acknowledge.
//
// The three answers are distinct on purpose. A snapshot plus `applied` replaces the
// grid; a nil snapshot plus `unchanged` leaves it alone and is a NOMINAL outcome; a nil
// snapshot plus an error leaves the catalog N−1 in service and lights the red light —
// there is no fourth shape, and in particular no half catalog.
func (a *Applier) Apply(ctx context.Context, cfg domain.Config, batch *ports.Batch) (
	*domain.Catalog, ports.BatchResult, error) {
	if batch == nil {
		return nil, ports.BatchResult{}, errors.New("importer : aucun lot à appliquer")
	}
	started := a.clock.Now()
	report := catalog.Summarize(batch)
	quarantine := catalog.NewQuarantine(a.records, whole(cfg, "failures_before_reject",
		catalog.DefaultFailuresBeforeReject))

	if entry, banned := quarantine.Banned(ctx, batch.ID); banned {
		return a.refuse(ctx, batch, report, started, bannedReason(entry), notCounted)
	}
	if a.alreadyApplied(ctx, batch.ID) {
		return a.unchanged(ctx, batch, report, started)
	}
	if len(batch.Products) == 0 {
		return a.refuse(ctx, batch, report, started,
			"le fichier ne porte aucun produit : un catalogue vide ne remplace pas un catalogue en service",
			counted)
	}
	if reason, amputated := a.amputated(ctx, cfg, report); amputated {
		return a.refuse(ctx, batch, report, started, reason, counted)
	}
	return a.replace(ctx, cfg, batch, report, started)
}

// alreadyApplied reports a content this station already put in service.
//
// A producer may drop a byte-identical export every night, and that is a NOMINAL
// event: an earlier design turned it into a constraint violation, an aborted
// transaction, an unacknowledged file, a retry, and finally a permanent ban behind a
// red light (ADR-015, §10.5).
func (a *Applier) alreadyApplied(ctx context.Context, sha string) bool {
	if sha == "" {
		return false
	}
	last, err := a.records.LastAppliedImport(ctx)
	return err == nil && last.SHA256 == sha
}

// unchanged records the second sighting of a catalog and touches nothing else.
//
// The findings are NOT written again: they belong to the import that produced the
// catalog in service, and re-recording seventeen rows every night would grow a table
// whose whole content is already there, unchanged, one row above.
func (a *Applier) unchanged(ctx context.Context, batch *ports.Batch, report catalog.Report,
	started time.Time) (*domain.Catalog, ports.BatchResult, error) {
	row := a.importRow(batch, report, started, domain.ImportUnchanged)
	if _, err := a.records.RecordImport(ctx, row, nil); err != nil {
		return nil, ports.BatchResult{Result: domain.ImportFailed, Code: codeDatabase,
			Reason: err.Error()}, err
	}
	a.log.Technical(domain.LevelInfo, "catalog", "",
		"Catalogue inchangé : le fichier déposé est celui qui est déjà en service.", batch.FileName)
	// A nil snapshot is the point: nothing is swapped, so nothing moves under the
	// finger of a customer browsing the grid (§10.8).
	return nil, ports.BatchResult{Result: domain.ImportUnchanged}, nil
}

// counted and notCounted say whether a refusal is one more failure against a CONTENT.
//
// Named rather than a bare bool at the call site: « refuse(..., true) » would be a
// command flag, and this one decides whether a red light is on its way.
const (
	counted    = true
	notCounted = false
)

// refuse leaves the catalog N−1 in service and says why, in French, with what to do.
//
// The error it returns is what the station journals as ERR-CAT-03, and the reason is
// what the source writes in the .reason.txt next to the archived copy: the file is gone
// from the drop directory, so that sentence is all somebody has to work from (§10.5).
func (a *Applier) refuse(ctx context.Context, batch *ports.Batch, report catalog.Report,
	started time.Time, reason string, count bool) (*domain.Catalog, ports.BatchResult, error) {
	if count {
		if _, err := a.records.RecordContentFailure(ctx, batch.ID, codeContent, reason); err != nil {
			a.log.Technical(domain.LevelWarn, "catalog", codeContent,
				"Échec de contenu non compté en quarantaine.", err.Error())
		}
	}
	row := a.importRow(batch, report, started, domain.ImportRejected)
	row.Code, row.Reason = codeContent, reason
	if _, err := a.records.RecordImport(ctx, row, batch.Findings); err != nil {
		a.log.Technical(domain.LevelWarn, "catalog", codeContent,
			"Refus d'import non journalisé.", err.Error())
	}
	return nil, ports.BatchResult{Result: domain.ImportRejected, Code: codeContent, Reason: reason},
		fmt.Errorf("%w : %s", catalog.ErrContent, reason)
}

// replace applies the batch in ONE transaction and returns the grid that came out of
// it.
//
// The snapshot is READ BACK from the database rather than built from the batch, and
// that is the line that makes three separate rules true at once: a transaction that
// rolled back cannot produce a grid, a product a human stopped offering stays out of
// it (§10.6), and a product that left the file stays out of it too (§10.9) — none of
// which the batch knows anything about.
func (a *Applier) replace(ctx context.Context, cfg domain.Config, batch *ports.Batch,
	report catalog.Report, started time.Time) (*domain.Catalog, ports.BatchResult, error) {
	written := store.Batch{
		Import:     a.importRow(batch, report, started, domain.ImportApplied),
		Categories: cfg.Catalog.Categories,
		Images:     batch.Images,
		Products:   batch.Products,
		Findings:   batch.Findings,
	}
	if _, err := a.records.ReplaceCatalog(ctx, written); err != nil {
		// NOT a content failure, and therefore NOT a quarantine: the file is fine, the
		// database is not. Banning a producer's catalog because a disk was full would be
		// the false alarm §10.5 exists to prevent.
		reason := fmt.Sprintf("le catalogue n'a pas pu être écrit : %v ; "+
			"le catalogue précédent reste en service", err)
		// The history line is written OUTSIDE the transaction that rolled back, so that
		// an administration screen can say what happened rather than show an import that
		// left no trace at all. A database too broken to take it says so in its turn, and
		// there is nothing left to do about that.
		failed := a.importRow(batch, report, started, domain.ImportFailed)
		failed.Code, failed.Reason = codeDatabase, reason
		if _, recordErr := a.records.RecordImport(ctx, failed, nil); recordErr != nil {
			a.log.Technical(domain.LevelWarn, "catalog", codeDatabase,
				"Échec d'import non journalisé.", recordErr.Error())
		}
		return nil, ports.BatchResult{Result: domain.ImportFailed, Code: codeDatabase,
			Reason: reason}, fmt.Errorf("%s : %w", codeDatabase, err)
	}

	snapshot, err := a.records.LoadCatalog(ctx)
	if err != nil {
		// The import IS applied — the transaction committed — so the file is acknowledged
		// all the same. What failed is the reading back, and the grid in service simply
		// stays the previous one until the next reload.
		a.log.Technical(domain.LevelWarn, "catalog", codeDatabase,
			"Catalogue appliqué mais non relu : la grille en service reste la précédente.",
			err.Error())
		return nil, ports.BatchResult{Result: domain.ImportApplied}, nil
	}
	a.log.Technical(domain.LevelInfo, "catalog", "",
		fmt.Sprintf("Catalogue appliqué : %d tuiles sur %d lignes reçues.",
			snapshot.WeighableCount(), report.RowsRead), batch.FileName)
	return snapshot, ports.BatchResult{Result: domain.ImportApplied}, nil
}

// The two codes an import ends on.
//
// ERR-CAT-03 is about a CONTENT and it counts towards the quarantine; a database that
// refuses to write is neither, and giving it a code of its own is what keeps the
// quarantine counting the thing it is named after (§10.5).
const (
	codeContent  = "ERR-CAT-03"
	codeDatabase = "ERR-DB-01"
)

// importRow is the history line of one import, whatever became of it.
//
// Every figure in it comes from the report, which counted the batch: none of them is
// re-derived here, so the row and the screen can never disagree.
func (a *Applier) importRow(batch *ports.Batch, report catalog.Report, started time.Time,
	result string) domain.Import {
	now := a.clock.Now()
	return domain.Import{
		OccurredAt:     now,
		Source:         batch.Source,
		FileName:       batch.FileName,
		SHA256:         batch.ID,
		ByteCount:      batch.Bytes,
		RowsRead:       report.RowsRead,
		UnreadableRows: report.UnreadableRows,
		Weighable:      report.Weighable,
		NotWeighable:   report.NotWeighable,
		Anomalies:      report.Anomalies,
		UnitMismatches: report.UnitMismatches,
		ImagesDecoded:  report.ImagesDecoded,
		ImagesRejected: report.ImagesRejected,
		Result:         result,
		DurationMS:     int(now.Sub(started).Milliseconds()),
	}
}

// whole reads a whole-number option, or the value the specification ships.
func whole(cfg domain.Config, key string, fallback int) int {
	if value, ok := cfg.Catalog.Options.Int(key); ok && value > 0 {
		return int(value)
	}
	return fallback
}
