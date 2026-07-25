package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"openscale/internal/domain"
)

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

// LoadCatalog builds the immutable snapshot the Hub publishes.
//
// The grid predicate of §12.3 is evaluated HERE, in SQL, once: present in the catalog
// (withdrawn_at IS NULL) and not refused by a human (local_decisions.offered). There
// is no products.visible column to forget to filter on, and no consumer can forget a
// clause it never sees. Products that are not weighable are still returned -- they are
// part of the catalog, they simply get no tile, and WeighableCount is what the
// dashboard shows.
//
// Ordering is alphabetical by name, as §12.3 requires; the presentation order of the
// grid remains the front's business.
func (d *DB) LoadCatalog(ctx context.Context) (*domain.Catalog, error) {
	categories, err := d.categories(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := d.reader.QueryContext(ctx, `
		SELECT p.id, p.name, p.reference, p.mode, p.price_suffix, p.unit_price_cents,
		       p.category_code, p.qualification, p.reason, p.csv_line, p.image_sha256
		  FROM products p
		  LEFT JOIN local_decisions d ON d.product_id = p.id
		 WHERE p.withdrawn_at IS NULL
		   AND COALESCE(d.offered, 1) = 1
		 ORDER BY p.name`)
	if err != nil {
		return nil, fmt.Errorf("lecture du catalogue impossible : %w", err)
	}
	defer rows.Close()

	var products []domain.Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		products = append(products, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return domain.NewCatalog(products, categories), nil
}

// ProductRow is a product plus the three storage facts the domain type does not carry.
//
// The domain has no place for them and must not grow one: "when was this row last
// seen" and "which import saw it" are observations about a row, not properties of a
// product (§10.9). Only the admin screens read them.
type ProductRow struct {
	Product domain.Product
	SeenAt  time.Time
	// WithdrawnAt is the zero instant while the product is in the catalog.
	WithdrawnAt  time.Time
	LastImportID int64
}

// AllProducts returns every row, withdrawn ones included, for the admin catalog screen.
//
// One grid with filters derived from the data, not four screens (ADR-024): the caller
// gets everything and narrows it itself, which is also what lets it say "4 products
// withdrawn since the import of 12/03" without a second query.
func (d *DB) AllProducts(ctx context.Context) ([]ProductRow, error) {
	rows, err := d.reader.QueryContext(ctx, `
		SELECT id, name, reference, mode, price_suffix, unit_price_cents, category_code,
		       qualification, reason, csv_line, image_sha256, seen_at, withdrawn_at, last_import_id
		  FROM products
		 ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("lecture des produits impossible : %w", err)
	}
	defer rows.Close()

	var out []ProductRow
	for rows.Next() {
		var (
			r         ProductRow
			reference string
			mode      string
			qual      string
			imageSHA  sql.NullString
			seenAt    string
			withdrawn sql.NullString
		)
		if err := rows.Scan(&r.Product.ID, &r.Product.Name, &reference, &mode, &r.Product.PriceSuffix,
			&r.Product.UnitPrice, &r.Product.CategoryCode, &qual, &r.Product.Reason,
			&r.Product.CSVLine, &imageSHA, &seenAt, &withdrawn, &r.LastImportID); err != nil {
			return nil, err
		}
		if err := fillProduct(&r.Product, reference, mode, qual, imageSHA); err != nil {
			return nil, err
		}
		if r.SeenAt, err = parseTime(seenAt); err != nil {
			return nil, err
		}
		if r.WithdrawnAt, err = timeFromNull(withdrawn); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Product returns one product by its Odoo id, withdrawn or not.
//
// Returns ErrNotFound when the id is unknown. It never returns "absent" for a
// withdrawn product: the row survives its disappearance from the CSV, and a journal
// entry that names it must still be readable (§10.9).
func (d *DB) Product(ctx context.Context, id string) (ProductRow, error) {
	var (
		r         ProductRow
		reference string
		mode      string
		qual      string
		imageSHA  sql.NullString
		seenAt    string
		withdrawn sql.NullString
	)
	err := d.reader.QueryRowContext(ctx, `
		SELECT id, name, reference, mode, price_suffix, unit_price_cents, category_code,
		       qualification, reason, csv_line, image_sha256, seen_at, withdrawn_at, last_import_id
		  FROM products WHERE id = ?`, id).
		Scan(&r.Product.ID, &r.Product.Name, &reference, &mode, &r.Product.PriceSuffix,
			&r.Product.UnitPrice, &r.Product.CategoryCode, &qual, &r.Product.Reason,
			&r.Product.CSVLine, &imageSHA, &seenAt, &withdrawn, &r.LastImportID)
	if err != nil {
		return ProductRow{}, notFound(err)
	}
	if err := fillProduct(&r.Product, reference, mode, qual, imageSHA); err != nil {
		return ProductRow{}, err
	}
	if r.SeenAt, err = parseTime(seenAt); err != nil {
		return ProductRow{}, err
	}
	if r.WithdrawnAt, err = timeFromNull(withdrawn); err != nil {
		return ProductRow{}, err
	}
	return r, nil
}

// Image returns the metadata of one photo, addressed by its content.
//
// It is what GET /images/{sha}.{ext} consults before serving a file: the extension and
// the Content-Type derive from the stored FORMAT, never from the requested extension,
// so a request for .jpg on a row that says png is a 404 and not a mislabelled body
// (§10.7). Returns ErrNotFound for an unknown sha.
func (d *DB) Image(ctx context.Context, sha string) (domain.Image, error) {
	var (
		img    domain.Image
		seenAt string
	)
	err := d.reader.QueryRowContext(ctx,
		`SELECT sha256, byte_count, format, width, height, seen_at FROM images WHERE sha256 = ?`, sha).
		Scan(&img.SHA256, &img.ByteCount, &img.Format, &img.Width, &img.Height, &seenAt)
	if err != nil {
		return domain.Image{}, notFound(err)
	}
	if img.SeenAt, err = parseTime(seenAt); err != nil {
		return domain.Image{}, err
	}
	return img, nil
}

// categories reads the shelves of the grid, in display order.
func (d *DB) categories(ctx context.Context) ([]domain.Category, error) {
	rows, err := d.reader.QueryContext(ctx,
		`SELECT code, label, rank, color, visible FROM categories ORDER BY rank, code`)
	if err != nil {
		return nil, fmt.Errorf("lecture des catégories impossible : %w", err)
	}
	defer rows.Close()

	var out []domain.Category
	for rows.Next() {
		var (
			c       domain.Category
			visible int
		)
		if err := rows.Scan(&c.Code, &c.Label, &c.Rank, &c.Color, &visible); err != nil {
			return nil, err
		}
		c.Visible = visible == 1
		out = append(out, c)
	}
	return out, rows.Err()
}

// scanProduct reads the eleven catalog columns of a grid query.
func scanProduct(rows *sql.Rows) (domain.Product, error) {
	var (
		p         domain.Product
		reference string
		mode      string
		qual      string
		imageSHA  sql.NullString
	)
	if err := rows.Scan(&p.ID, &p.Name, &reference, &mode, &p.PriceSuffix, &p.UnitPrice,
		&p.CategoryCode, &qual, &p.Reason, &p.CSVLine, &imageSHA); err != nil {
		return domain.Product{}, err
	}
	err := fillProduct(&p, reference, mode, qual, imageSHA)
	return p, err
}

// fillProduct converts the four columns that are not plain scalars.
func fillProduct(p *domain.Product, reference, mode, qual string, imageSHA sql.NullString) error {
	p.Reference = domain.EAN13(reference)
	m, err := parseSaleMode(mode)
	if err != nil {
		return fmt.Errorf("produit %s : %w", p.ID, err)
	}
	p.Mode = m
	q, err := parseQualification(qual)
	if err != nil {
		return fmt.Errorf("produit %s : %w", p.ID, err)
	}
	p.Qualification = q
	if imageSHA.Valid {
		p.ImageSHA = imageSHA.String
	}
	return nil
}
