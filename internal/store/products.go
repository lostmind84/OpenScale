package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"openscale/internal/domain"
)

// This file reads a catalog back out: the snapshot a station starts on, the rows the
// administration screens page through, and the photos addressed by their content.
//
// A withdrawn product is still HERE — it left the file, it did not leave the history —
// and it is the grid predicate, not a deletion, that keeps it off the customer screen.

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
