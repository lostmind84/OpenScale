package station

import (
	"context"
	"errors"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// This file is goroutine n° 5 of §13.1: the watch that reads whole catalogs from the
// source in service, offers them to the loop and acknowledges the file. The swap
// itself is the Hub's business and is DEFERRED (§10.8).

// currentCatalogSource reports the source in service.
//
// It exists because a reload replaces that source while watchCatalog is reading from
// it: -race caught the write of restartCatalog against the read of the watch loop.
func (s *Station) currentCatalogSource() ports.CatalogSource {
	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	return s.catalogSource
}

// swapCatalogSource puts next in service and returns the one it replaced, so the
// caller closes the old source OUTSIDE the lock — Close talks to a file system or to
// a WebDAV server and has no business being held against the watch loop.
//
// It also ENDS the read in flight. Without that, the swap changes what a getter
// answers and nothing else: the watch stays parked in the source it just replaced,
// for as long as the process lives, and a station pointed at a share goes on watching
// an empty drop folder until somebody restarts the service.
func (s *Station) swapCatalogSource(next ports.CatalogSource) ports.CatalogSource {
	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	previous := s.catalogSource
	s.catalogSource = next
	if s.cancelCatalogRead != nil {
		s.cancelCatalogRead()
		s.cancelCatalogRead = nil
	}
	return previous
}

// beginCatalogRead hands back the source in service and the context to read it with.
//
// The context ends with the parent or with the next swap, whichever comes first, and
// the returned func ends it once the read is over. It is handed out EVEN WHEN THERE IS
// NO SOURCE: a station whose share was unreachable at boot starts without one, and
// waiting on that context is how it notices the one a reload puts in service.
func (s *Station) beginCatalogRead(parent context.Context) (ports.CatalogSource, context.Context, context.CancelFunc) {
	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	ctx, cancel := context.WithCancel(parent)
	s.cancelCatalogRead = cancel
	return s.catalogSource, ctx, cancel
}

// watchCatalog reads whole catalogs from the source and hands them to the loop.
//
// The swap itself is the Hub's business and it is DEFERRED: this goroutine never
// changes what is on screen, it only offers.
func (s *Station) watchCatalog(ctx context.Context) {
	defer close(s.catalogDone)
	for {
		source, readCtx, endRead := s.beginCatalogRead(ctx)
		if source == nil {
			// Wait for one to arrive rather than for the end of the process: the
			// station was started with an unbuildable source, and the volunteer is
			// about to repair it on the screen.
			<-readCtx.Done()
			endRead()
			if ctx.Err() != nil {
				return
			}
			continue
		}
		batch, err := source.Next(readCtx)
		// READ BEFORE ENDING IT: endRead cancels this very context, so asking
		// afterwards answers « replaced » for every batch the source ever yields.
		replaced := readCtx.Err() != nil
		endRead()
		if ctx.Err() != nil {
			return
		}
		// A read ended by the swap and not by the source: the station has a new source
		// and this loop reads it now. It is not a failure and it says nothing in the
		// journal — the reload already wrote what changed.
		if replaced {
			continue
		}
		if err != nil {
			s.hub.logTechnical(domain.LevelWarn, "catalog", "ERR-CAT-03",
				"Lecture du catalogue impossible.", err.Error())
			continue
		}
		if batch == nil {
			continue
		}
		s.offer(ctx, source, batch)
	}
}

// offer qualifies one batch, hands it to the loop and acknowledges the file.
//
// Acknowledgement is EXPLICIT and comes LAST: deleting at read time would let a
// crash between reading and applying lose an update for good, and without a trace.
func (s *Station) offer(ctx context.Context, source ports.CatalogSource, batch *ports.Batch) {
	cfg := *s.hub.cfg.Load()
	catalog, result, err := s.applyCatalog(ctx, cfg, batch)
	if err != nil {
		s.hub.logTechnical(domain.LevelError, "catalog", "ERR-CAT-03",
			"Catalogue refusé.", err.Error())
	} else if catalog != nil {
		s.logIfCatalogErr(s.hub.PushCatalog(ctx, &CatalogBatch{
			Catalog: catalog, Source: batch.Source,
			FileName: batch.FileName, ImportedAt: importedAt(result, s.clock),
		}))
	}
	if err := source.Acknowledge(ctx, batch, result); err != nil {
		s.hub.logTechnical(domain.LevelWarn, "catalog", "ERR-CAT-05",
			"Fichier de catalogue non supprimé.", err.Error())
	}
}

// importedAt is the instant the applier recorded, or the clock when it recorded none.
//
// The fallback is for the DEFAULT applier — plainCatalog, which writes no history row
// because it has no store to write it to — and for any plug-in one somebody adds later.
// A station whose applier keeps no history still has to answer « ces prix datent de
// quand ? », and the moment its catalog was offered is the truest thing left to say.
func importedAt(result ports.BatchResult, clock ports.Clock) time.Time {
	if result.AppliedAt.IsZero() {
		return clock.Now()
	}
	return result.AppliedAt
}

// logIfCatalogErr reports a catalog that never reached the loop.
func (s *Station) logIfCatalogErr(err error) {
	if err == nil || errors.Is(err, ErrStopped) || errors.Is(err, context.Canceled) {
		return
	}
	s.hub.logTechnical(domain.LevelWarn, "catalog", "",
		"Catalogue non remis au Hub.", err.Error())
}

// plainCatalog is the default applier: it freezes the rows the source produced
// with the categories this station is configured for, and acknowledges 'applied'.
func plainCatalog(_ context.Context, cfg domain.Config, b *ports.Batch) (*domain.Catalog, ports.BatchResult, error) {
	return domain.NewCatalog(b.Products, cfg.Catalog.Categories),
		ports.BatchResult{Result: domain.ImportApplied}, nil
}
