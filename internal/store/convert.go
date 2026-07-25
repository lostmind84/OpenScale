package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"openscale/internal/domain"
)

// This file is the only place where a domain value becomes a column and back.
//
// The domain types spell themselves with String() -- SaleMode, Qualification,
// Stability all report exactly the value the CHECK constraint of §12.3 accepts -- but
// none of them parses. The parsers live here rather than in the domain because
// "which strings may a column hold" is a storage question, and because a bad value
// read from a row is a corrupted file, not a business case.

// formatTime writes an instant the way every TEXT timestamp column expects it.
func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

// parseTime reads back what formatTime wrote.
//
// RFC3339Nano is accepted as a fallback so that a row written by hand, by a repair
// script or by sqlite3 on someone's laptop still loads: refusing to display a journal
// because one timestamp lost its milliseconds would be a self-inflicted outage.
func parseTime(s string) (time.Time, error) {
	if t, err := time.Parse(timeLayout, s); err == nil {
		return t, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("horodate illisible en base (%q) : %w", s, err)
	}
	return t, nil
}

// timeFromNull reads a nullable timestamp; SQL NULL becomes the zero instant.
//
// It is how "not withdrawn" is read back from products.withdrawn_at (§10.9). There is
// no symmetric writer: the only two writes of that column are a literal NULL in the
// upsert and a real instant in the withdrawal, and a helper for a case that does not
// occur is a helper nobody maintains.
func timeFromNull(s sql.NullString) (time.Time, error) {
	if !s.Valid || s.String == "" {
		return time.Time{}, nil
	}
	return parseTime(s.String)
}

// nullString renders the empty string as SQL NULL.
//
// It matters for two columns and for a precise reason: products.image_sha256 and
// weighings.product_id are foreign keys, and ” is a VALUE that satisfies no parent
// row, whereas NULL is the absence the schema means.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// boolToInt spells a Go bool the way an INTEGER CHECK (x IN (0,1)) column wants it.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// parseSaleMode reads products.mode and weighings.mode.
func parseSaleMode(s string) (domain.SaleMode, error) {
	switch s {
	case "by_weight":
		return domain.ByWeight, nil
	case "by_unit":
		return domain.ByUnit, nil
	}
	return 0, fmt.Errorf("mode de vente inconnu en base : %q", s)
}

// parseQualification reads products.qualification.
func parseQualification(s string) (domain.Qualification, error) {
	switch s {
	case "weighable":
		return domain.Weighable, nil
	case "not_weighable":
		return domain.NotWeighable, nil
	case "anomaly":
		return domain.Anomaly, nil
	}
	return 0, fmt.Errorf("qualification inconnue en base : %q", s)
}

// parseStability reads weighings.stability.
func parseStability(s string) (domain.Stability, error) {
	switch s {
	case "stable":
		return domain.Stable, nil
	case "unstable":
		return domain.Unstable, nil
	case "unknown":
		return domain.StabilityUnknown, nil
	case "not_applicable":
		return domain.StabilityNotApplicable, nil
	}
	return 0, fmt.Errorf("stabilité inconnue en base : %q", s)
}

// notFound turns the driver's "no rows" into ErrNotFound and leaves anything else
// alone, so that a caller can tell an absent product from a broken file.
func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
