package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	catalogpkg "openscale/internal/catalog"
	"openscale/internal/catalog/csvodoo"
	"openscale/internal/domain"
	"openscale/internal/station/ports"
	"openscale/internal/store"
)

// adminCatalog is the catalog source as the administration screen acts on it.
//
// THERE IS NO SECOND IMPORT PATH, and that is the point of this file. « Recharger le
// catalogue » makes the ordinary watch poll now, and the drag-and-drop of a CSV writes the
// file where that same watch will find it: same parser, same qualification, same
// transaction, same acknowledgement (A4, ADR-011, §10.1). A route that parsed and applied
// a file by itself would be a second code path with its own guards, its own archive and
// its own bugs, for the same job.
type adminCatalog struct {
	source *liveCatalog
	db     *store.DB
	clock  ports.Clock
	log    ports.TechnicalLog
	// config reports the configuration IN FORCE, because the parse limits and the
	// fallback category are read from it and both are hot-reloadable (§11.4).
	config func() domain.Config
}

// waker is a catalog source that can be asked to poll NOW.
//
// Both shipped sources are one, and the interface is declared here — on the consumer's
// side — so that a source which cannot be woken is refused with a sentence rather than
// answered with a silent success.
type waker interface{ Wake() }

// watchedFile is a source that watches a named file of this machine.
//
// Only a local drop has one: a share is watched over the network, and a CSV dropped on
// the screen of a station configured that way has nowhere local to go.
type watchedFile interface{ Path() string }

// maxDroppedBytes bounds a catalog dropped on the screen: eight megabytes, the ceiling of
// §10.1. The real file weighs 527 kB.
//
// internal/web already bounds the multipart form at the same figure; this one bounds what
// is READ from it, because a Reader handed to this package is a Reader and its size is a
// promise somebody else made.
const maxDroppedBytes = 8 << 20

// Reload asks the source for a fresh batch NOW rather than at the next poll (§14.4).
//
// It reads no file itself. What it does is wake the watch, which then applies the very
// rules it always applies — including « a file still growing is not read » (§10.5), so a
// producer's export caught mid-copy is not turned into an amputated catalog by somebody
// pressing a button.
func (c adminCatalog) Reload(_ context.Context) error {
	source := c.source.current()
	if source == nil {
		// The refusal names a PAGE and not two configuration keys: the volunteer reading it
		// has an administration screen in front of them, not a file, and the « réglages
		// avancés » this sentence used to send them to were removed on 27/07/2026.
		return errors.New("aucune source de catalogue n'a pu être ouverte sur ce poste : " +
			"choisissez où le poste va chercher le catalogue, sur la page Catalogue")
	}
	wake, ok := source.(waker)
	if !ok {
		return fmt.Errorf("la source %q ne sait pas relire à la demande : le prochain "+
			"balayage la relira", source.Name())
	}
	wake.Wake()
	return nil
}

// Import takes a CSV dropped on the screen and writes it where the ordinary watcher will
// find it (A4).
//
// # It parses BEFORE it writes, and gives the inventory back
//
// Two reasons, in order. A file that is not a catalog at all — the wrong export, a PDF
// renamed — must be refused while the volunteer is still looking at the screen, and not
// five seconds later in a journal they would have to go and open. And the answer they get
// is the inventory of §14.4, measured on the very bytes they dropped: « 355 produits reçus,
// 331 pesables ». A 202 carrying nothing but « accepté » would leave them wondering
// whether the file was the right one.
//
// The parse keeps NO image: the watcher is about to decode them again and write them by
// content address, so doing it twice would write the same 165 files twice. What the report
// counts is unaffected — the photos are decoded either way, only the sink differs.
//
// # Why the record it returns carries no result
//
// The four results of an import say what became of it: applied, unchanged, rejected,
// failed (§10.5). This file has not been applied yet — the watch will, in the seconds that
// follow, and THAT import is the one that lands in the history. Inventing a fifth value
// would put a word in a column whose CHECK constraint does not allow it, and would make
// the history show two rows for one file.
func (c adminCatalog) Import(ctx context.Context, name string, r io.Reader) (domain.Import, error) {
	source := c.source.current()
	if source == nil {
		return domain.Import{}, errors.New("aucune source de catalogue n'a pu être ouverte " +
			"sur ce poste : le fichier n'a nulle part où être déposé")
	}
	watched, ok := source.(watchedFile)
	if !ok {
		return domain.Import{}, fmt.Errorf("ce poste lit son catalogue par %q : déposez le "+
			"fichier sur le partage, ou passez catalog.type à %q pour utiliser le dépôt local",
			source.Name(), domain.CatalogSourceLocalDrop)
	}

	raw, err := io.ReadAll(io.LimitReader(r, maxDroppedBytes+1))
	if err != nil {
		return domain.Import{}, fmt.Errorf("le fichier déposé n'a pas pu être lu : %w", err)
	}
	if int64(len(raw)) > maxDroppedBytes {
		return domain.Import{}, fmt.Errorf("le fichier déposé dépasse %d Mo : "+
			"ce n'est plus un catalogue", maxDroppedBytes>>20)
	}

	cfg := c.config()
	options := csvodoo.OptionsFrom(cfg.Catalog)
	// The provenance is an OBSERVATION and never a branch of code: the row will say
	// « manual », and everything that happens to the file afterwards is what happens to
	// any other file (§10.9).
	options.Source, options.FileName, options.Now = domain.CatalogSourceManual, name, c.clock.Now()
	batch, err := csvodoo.Parse(bytes.NewReader(raw), options)
	if err != nil {
		return domain.Import{}, err
	}

	if err := dropFile(watched.Path(), raw); err != nil {
		return domain.Import{}, err
	}
	if wake, ok := source.(waker); ok {
		wake.Wake()
	}
	c.log.Technical(domain.LevelInfo, "catalog", "",
		"Catalogue déposé depuis l'écran d'administration.",
		fmt.Sprintf("%s → %s", name, watched.Path()))

	return receivedImport(batch, c.clock.Now()), nil
}

// receivedImport is the inventory of a file that has been ACCEPTED for reading.
//
// Every figure comes from catalog.Summarize, which counted the batch: none is re-derived
// here, so this answer and the history row the watch is about to write cannot disagree.
func receivedImport(batch *ports.Batch, now time.Time) domain.Import {
	report := catalogpkg.Summarize(batch)
	return domain.Import{
		OccurredAt: now,
		Source:     domain.CatalogSourceManual,
		FileName:   batch.FileName,
		SHA256:     batch.ID,
		ByteCount:  batch.Bytes,

		RowsRead:       report.RowsRead,
		UnreadableRows: report.UnreadableRows,
		Weighable:      report.Weighable,
		NotWeighable:   report.NotWeighable,
		Anomalies:      report.Anomalies,
		UnitMismatches: report.UnitMismatches,
		ImagesDecoded:  report.ImagesDecoded,
		ImagesRejected: report.ImagesRejected,

		Reason: "Fichier déposé dans le répertoire surveillé : il prend service dans quelques " +
			"secondes, et son inventaire définitif s'inscrira dans l'historique des imports.",
	}
}

// dropFile writes the catalog into the watched directory ATOMICALLY.
//
// tmp then rename, in the SAME directory, for the reason the whole watch is built around:
// a file still growing is not read (§10.5), and a partial write that the stability rule
// happened to sample at the wrong instant would be an amputated catalog offered to a
// station. The rename makes the file appear whole, at its final size, in one step.
func dropFile(path string, content []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("le répertoire de dépôt %s ne peut pas être créé : %w", directory, err)
	}
	file, err := os.CreateTemp(directory, "dropped-*.csv")
	if err != nil {
		return fmt.Errorf("fichier temporaire dans %s : %w", directory, err)
	}
	tmp := file.Name()
	if _, err := file.Write(content); err != nil {
		file.Close()
		os.Remove(tmp)
		return fmt.Errorf("écriture de %s : %w", tmp, err)
	}
	if err := file.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("fermeture de %s : %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("le fichier déposé n'a pas pu être mis en place dans %s : %w", path, err)
	}
	return nil
}

// ForgetQuarantine clears the memory of the files that were refused (§10.5).
//
// ALL of them, and that is what the button says: a producer who corrected an export and
// re-dropped a byte-identical file would otherwise find a station that has already given
// up on that content. Forgetting is cheap — the next refusal counts again from one.
func (c adminCatalog) ForgetQuarantine(ctx context.Context) error {
	forgotten, err := c.db.ForgetQuarantine(ctx, "")
	if err != nil {
		return err
	}
	c.log.Technical(domain.LevelInfo, "catalog", "",
		"Quarantaine oubliée depuis l'écran d'administration.",
		fmt.Sprintf("%d contenu(s) oublié(s)", forgotten))
	return nil
}
