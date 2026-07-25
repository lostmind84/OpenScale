package store

import (
	"context"
	"database/sql"
	"fmt"

	"openscale/internal/domain"
)

// SaveDecision records the human judgement about one product.
//
// It is neither the qualification nor a correction of an anomaly, and that is why it
// has a table of its own (§10.6, ADR-017). The qualification answers a question of
// FACT -- can this product be weighed? -- and is computed. This answers a question of
// JUDGEMENT -- do we want to offer it today? -- and belongs to a human. The case it
// exists for is the one no import rule can detect: a reference that is irreproachable
// and wrong at heart, which prints a label the till refuses or charges wrongly, in
// front of a customer, until Odoo is fixed.
//
// The product must exist: the foreign key is ordinary, because since §10.9 a product
// no longer disappears from one import to the next. Both columns are written together,
// as one route writes them (§14.5): "stop offering this product" and "this product may
// weigh less than 10 g" are two columns of one decision, not two mechanisms.
func (d *DB) SaveDecision(ctx context.Context, dec domain.LocalDecision) error {
	var minWeight any
	if dec.MinWeightG != nil {
		minWeight = int64(*dec.MinWeightG)
	}
	_, err := d.writer.ExecContext(ctx, `
		INSERT INTO local_decisions (product_id, offered, min_weight_g, reason, decided_at, decided_by)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(product_id) DO UPDATE SET
			offered = excluded.offered, min_weight_g = excluded.min_weight_g,
			reason = excluded.reason, decided_at = excluded.decided_at,
			decided_by = excluded.decided_by`,
		dec.ProductID, boolToInt(dec.Offered), minWeight, dec.Reason,
		formatTime(dec.DecidedAt), dec.DecidedBy)
	if err != nil {
		return fmt.Errorf("décision locale sur le produit %s impossible : %w", dec.ProductID, err)
	}
	return nil
}

// ClearDecision removes the decision about one product, putting it back under the
// general rules. Removing an absent decision is not an error: the caller asked for a
// state, and that state is reached.
func (d *DB) ClearDecision(ctx context.Context, productID string) error {
	if _, err := d.writer.ExecContext(ctx,
		`DELETE FROM local_decisions WHERE product_id = ?`, productID); err != nil {
		return fmt.Errorf("suppression de la décision locale du produit %s impossible : %w", productID, err)
	}
	return nil
}

// Decision returns the decision about one product, or ErrNotFound when a human never
// said anything about it -- which is the answer for almost every product.
func (d *DB) Decision(ctx context.Context, productID string) (domain.LocalDecision, error) {
	row := d.reader.QueryRowContext(ctx, `
		SELECT product_id, offered, min_weight_g, reason, decided_at, decided_by
		  FROM local_decisions WHERE product_id = ?`, productID)
	dec, err := scanDecision(row)
	if err != nil {
		return domain.LocalDecision{}, notFound(err)
	}
	return dec, nil
}

// LocalDecisions returns every recorded decision, most recent first.
//
// The Hub needs the whole set at once, not one row per weighing: min_weight_g feeds
// domain.CheckInput.ProductMinWeight on the weighing path, and that path touches no
// disk (§4). The dashboard reads the same list to show how many decisions are active
// WITH their reason and their date -- which is what stops a product from sitting there
// for six months because nobody remembers why it went in.
func (d *DB) LocalDecisions(ctx context.Context) ([]domain.LocalDecision, error) {
	rows, err := d.reader.QueryContext(ctx, `
		SELECT product_id, offered, min_weight_g, reason, decided_at, decided_by
		  FROM local_decisions ORDER BY decided_at DESC, product_id`)
	if err != nil {
		return nil, fmt.Errorf("lecture des décisions locales impossible : %w", err)
	}
	defer rows.Close()

	var out []domain.LocalDecision
	for rows.Next() {
		dec, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, dec)
	}
	return out, rows.Err()
}

func scanDecision(s rowScanner) (domain.LocalDecision, error) {
	var (
		dec       domain.LocalDecision
		offered   int
		minWeight sql.NullInt64
		decidedAt string
	)
	if err := s.Scan(&dec.ProductID, &offered, &minWeight, &dec.Reason, &decidedAt, &dec.DecidedBy); err != nil {
		return domain.LocalDecision{}, err
	}
	dec.Offered = offered == 1
	if minWeight.Valid {
		// NULL means "the general limit applies" and must stay distinguishable from a
		// stored 0, which the CHECK forbids anyway (§10.6).
		g := domain.Grams(minWeight.Int64)
		dec.MinWeightG = &g
	}
	var err error
	if dec.DecidedAt, err = parseTime(decidedAt); err != nil {
		return domain.LocalDecision{}, err
	}
	return dec, nil
}
