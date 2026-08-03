package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"openscale/internal/domain"
)

// This file is the ONE transaction of §10.9: a whole catalog replaces the one in the
// base, or none of it does. The import row, the categories, the images, the products
// and the withdrawal of what left the file are written together — so the history and
// the grid can never disagree about which import produced which prices.
//
// What READS a catalog back out is in products.go.

// Batch is a whole catalog, as one import produced it.
//
// It carries the import row on purpose: products.last_import_id is NOT NULL, so the
// import and the catalog it produced are written by the same transaction or not at
// all. Two calls would leave a window where the products of an import that failed to
// be recorded are already in the grid.
type Batch struct {
	// Import is written first, inside the transaction, and its assigned id becomes the
	// last_import_id of every product. Its Result must be domain.ImportApplied: an
	// import that changes nothing goes through RecordImport, which touches no product.
	// ProductsWithdrawn is computed here and overwrites whatever the caller put in it.
	Import domain.Import
	// Categories are upserted first: products.category_code references them.
	Categories []domain.Category
	// Images are the rows of the photos this import decoded. The FILES were written
	// before this call and are addressed by their sha, which makes writing them
	// idempotent (§10.7); the ROWS belong inside the transaction, because
	// products.image_sha256 references them.
	Images []domain.Image
	// Products is the complete catalog of the file, weighable or not: a prepackaged
	// product is a row, it simply gets no tile (§10.3).
	Products []domain.Product
	// Findings are what the import has to say about the rows. A finding blocks nothing.
	Findings []domain.Finding
}

// ImportOutcome reports what one import did to the catalog.
type ImportOutcome struct {
	ImportID int64
	// Inserted and Updated exist to make the difference between an UPSERT and the
	// legacy DELETE-then-INSERT observable: re-importing the same file must report 355
	// updates and 0 insertions, because the 355 Odoo ids are the producer's key and
	// were never ours to destroy (§10.9).
	Inserted int
	Updated  int
	// Withdrawn counts the products this import did not see. They are marked at a date,
	// never deleted: they keep their weighing history, their local decision and their
	// image, and "4 products withdrawn since the import of 12/03" becomes a fact the
	// admin can show instead of a silence.
	Withdrawn int
}

// ReplaceCatalog applies one import in a SINGLE transaction.
//
// Either the N-1 catalog stays intact or the new one is completely in place; there is
// no visible intermediate state (§12.5). The write is an UPSERT BY id followed by one
// UPDATE that marks the unseen products withdrawn -- never a DELETE followed by
// re-INSERTs, which destroyed 355 identities in order to recreate 355 identical ones
// (§10.9).
//
// A product that reappears in the file loses its withdrawn_at and returns to the grid.
// Errors leave the database exactly as it was: a CHECK on a price out of range or a
// category the configuration does not declare aborts everything, on purpose.
func (d *DB) ReplaceCatalog(ctx context.Context, b Batch) (ImportOutcome, error) {
	if b.Import.Result != domain.ImportApplied {
		return ImportOutcome{}, fmt.Errorf(
			"store.ReplaceCatalog: import result is %q, want %q; an import that changes no product goes through RecordImport",
			b.Import.Result, domain.ImportApplied)
	}
	// Last line of defence, and the reason it is worth a line: an applied batch with no
	// product would withdraw the ENTIRE catalog, leaving a station with an empty grid and
	// a green light. The guards of §10.4 catch that upstream -- a file whose rows are
	// unreadable is a content failure, ERR-CAT-03 -- and this makes the outcome
	// unreachable even if one of them is ever loosened.
	if len(b.Products) == 0 {
		return ImportOutcome{}, errors.New(
			"store.ReplaceCatalog: empty batch; an import that read no product is a content failure, not an applied catalog")
	}

	now := d.clock.Now()
	var out ImportOutcome
	err := d.tx(ctx, func(tx *sql.Tx) error {
		known, err := existingProductIDs(ctx, tx)
		if err != nil {
			return err
		}

		importID, err := insertImport(ctx, tx, b.Import)
		if err != nil {
			return err
		}
		out.ImportID = importID

		if err := upsertCategories(ctx, tx, b.Categories); err != nil {
			return err
		}
		if err := upsertImages(ctx, tx, b.Images); err != nil {
			return err
		}
		if err := upsertProducts(ctx, tx, b.Products, importID, now); err != nil {
			return err
		}
		for _, p := range b.Products {
			if known[p.ID] {
				out.Updated++
			} else {
				out.Inserted++
			}
		}

		withdrawn, err := withdrawUnseen(ctx, tx, importID, now)
		if err != nil {
			return err
		}
		out.Withdrawn = withdrawn

		if err := insertFindings(ctx, tx, importID, b.Findings); err != nil {
			return err
		}
		// The counter is known only now, and it belongs to the same transaction as the
		// products it describes.
		_, err = tx.ExecContext(ctx,
			`UPDATE imports SET products_withdrawn = ? WHERE id = ?`, withdrawn, importID)
		return err
	})
	if err != nil {
		return ImportOutcome{}, err
	}
	return out, nil
}

// existingProductIDs reads the ids already present, so that Inserted and Updated can
// be told apart: an ON CONFLICT upsert reports one affected row either way.
func existingProductIDs(ctx context.Context, tx *sql.Tx) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM products`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	known := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		known[id] = true
	}
	return known, rows.Err()
}

func upsertCategories(ctx context.Context, tx *sql.Tx, categories []domain.Category) error {
	if len(categories) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO categories (code, label, rank, color, visible)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(code) DO UPDATE SET
			label = excluded.label, rank = excluded.rank,
			color = excluded.color, visible = excluded.visible`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, c := range categories {
		if _, err := stmt.ExecContext(ctx, c.Code, c.Label, c.Rank, c.Color, boolToInt(c.Visible)); err != nil {
			return fmt.Errorf("catégorie %q : %w", c.Code, err)
		}
	}
	return nil
}

func upsertImages(ctx context.Context, tx *sql.Tx, images []domain.Image) error {
	if len(images) == 0 {
		return nil
	}
	// seen_at is refreshed but the identity is the sha: the same photo re-imported is
	// the same row, which is what makes an import of 181 images write 0 new files.
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO images (sha256, byte_count, format, width, height, seen_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(sha256) DO UPDATE SET seen_at = excluded.seen_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, img := range images {
		if _, err := stmt.ExecContext(ctx, img.SHA256, img.ByteCount, img.Format,
			img.Width, img.Height, formatTime(img.SeenAt)); err != nil {
			return fmt.Errorf("image %s : %w", img.SHA256, err)
		}
	}
	return nil
}

func upsertProducts(ctx context.Context, tx *sql.Tx, products []domain.Product, importID int64, now time.Time) error {
	if len(products) == 0 {
		return nil
	}
	// One prepared statement for the whole batch: 355 rows is the measured size, and
	// re-parsing this statement 355 times is the kind of cost nobody ever comes back to
	// remove.
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO products (
			id, name, reference, mode, price_suffix, unit_price_cents, category_code,
			qualification, reason, csv_line, image_sha256, seen_at, withdrawn_at, last_import_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name, reference = excluded.reference, mode = excluded.mode,
			price_suffix = excluded.price_suffix, unit_price_cents = excluded.unit_price_cents,
			category_code = excluded.category_code, qualification = excluded.qualification,
			reason = excluded.reason, csv_line = excluded.csv_line,
			image_sha256 = excluded.image_sha256, seen_at = excluded.seen_at,
			last_import_id = excluded.last_import_id,
			-- A product that comes back in the file returns to the grid. Without this
			-- line a supplier gap of one night would retire an article for good.
			withdrawn_at = NULL`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	seenAt := formatTime(now)
	for _, p := range products {
		if _, err := stmt.ExecContext(ctx,
			p.ID, p.Name, string(p.Reference), p.Mode.String(), p.PriceSuffix, int64(p.UnitPrice),
			p.CategoryCode, p.Qualification.String(), p.Reason, p.CSVLine,
			nullString(p.ImageSHA), seenAt, importID,
		); err != nil {
			return fmt.Errorf("produit %s (ligne CSV %d) : %w", p.ID, p.CSVLine, err)
		}
	}
	return nil
}

// withdrawUnseen marks the products this import did not see.
//
// DEVIATION FROM §12.5, deliberate: the document writes "seen_at < :now", and that
// works only as long as two consecutive imports carry two different instants. The
// injected clock is FROZEN in the tests that exist to prove this very rule -- failure
// test 12 ter, and §16.3 -- so "seen_at < :now" would then withdraw nothing and the
// rule would silently stop working exactly where it is checked. last_import_id is
// guaranteed distinct by AUTOINCREMENT and says the same thing without depending on
// clock resolution: "not seen by THIS import".
func withdrawUnseen(ctx context.Context, tx *sql.Tx, importID int64, now time.Time) (int, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE products SET withdrawn_at = ?
		 WHERE last_import_id <> ? AND withdrawn_at IS NULL`, formatTime(now), importID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}
