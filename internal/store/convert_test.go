package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
)

// The parsers below have error branches no CHECK constraint lets a caller reach: a
// value outside the vocabulary means the FILE is corrupted, not that a caller made a
// mistake. They are tested directly, which is the only honest way to reach them.

func TestTimeRoundTripIsLexicographicallyChronological(t *testing.T) {
	instants := []time.Time{
		time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 12, 9, 0, 0, 5_000_000, time.UTC),
		time.Date(2026, 3, 12, 9, 0, 1, 0, time.UTC),
		time.Date(2026, 12, 31, 23, 59, 59, 999_000_000, time.UTC),
	}
	var previous string
	for _, instant := range instants {
		text := formatTime(instant)
		// Fixed width is the property the indexes and the purge of §12.4 rely on.
		if len(text) != len("2026-03-12T09:00:00.000Z") {
			t.Fatalf("formatTime(%s) = %q : largeur variable", instant, text)
		}
		if text <= previous {
			t.Fatalf("%q n'est pas après %q en ordre lexicographique", text, previous)
		}
		previous = text
		back, err := parseTime(text)
		if err != nil {
			t.Fatalf("parseTime(%q): %v", text, err)
		}
		// Millisecond truncation is deliberate: nothing in this database decides anything
		// on a microsecond.
		if !back.Equal(instant.Truncate(time.Millisecond)) {
			t.Fatalf("aller-retour : %s -> %q -> %s", instant, text, back)
		}
	}
}

// TestParseTimeAcceptsRFC3339Nano: a row written by hand or by a repair script must not
// make a journal page unreadable.
func TestParseTimeAcceptsRFC3339Nano(t *testing.T) {
	got, err := parseTime("2026-03-12T09:00:00Z")
	if err != nil {
		t.Fatalf("parseTime: %v", err)
	}
	if !got.Equal(TestEpoch) {
		t.Fatalf("parseTime = %s, want %s", got, TestEpoch)
	}
	if _, err := parseTime("hier matin"); err == nil {
		t.Fatal("parseTime a accepté une horodate illisible")
	}
}

func TestTimeFromNull(t *testing.T) {
	for _, c := range []struct {
		in   sql.NullString
		zero bool
	}{
		{sql.NullString{}, true},
		{sql.NullString{String: "", Valid: true}, true},
		{sql.NullString{String: "2026-03-12T09:00:00.000Z", Valid: true}, false},
	} {
		got, err := timeFromNull(c.in)
		if err != nil {
			t.Fatalf("timeFromNull(%+v): %v", c.in, err)
		}
		if got.IsZero() != c.zero {
			t.Errorf("timeFromNull(%+v).IsZero() = %v, want %v", c.in, got.IsZero(), c.zero)
		}
	}
	if _, err := timeFromNull(sql.NullString{String: "n'importe quoi", Valid: true}); err == nil {
		t.Fatal("timeFromNull a accepté une horodate illisible")
	}
}

func TestNullStringIsNULLAndNotTheEmptyString(t *testing.T) {
	// It matters for two foreign keys: '' is a VALUE that satisfies no parent row.
	if nullString("") != nil {
		t.Error("nullString(\"\") doit rendre NULL")
	}
	if nullString("abc") != "abc" {
		t.Error("nullString a modifié une valeur non vide")
	}
}

func TestBoolToInt(t *testing.T) {
	if boolToInt(true) != 1 || boolToInt(false) != 0 {
		t.Fatal("boolToInt ne rend pas 1 et 0")
	}
}

func TestParseSaleModeMatchesTheDomainSpelling(t *testing.T) {
	// Round trip against the domain: the CHECK of §12.3 accepts exactly what String
	// writes, so the two must agree or a perfectly good row becomes unreadable.
	for _, mode := range []domain.SaleMode{domain.ByWeight, domain.ByUnit} {
		got, err := parseSaleMode(mode.String())
		if err != nil {
			t.Fatalf("parseSaleMode(%q): %v", mode.String(), err)
		}
		if got != mode {
			t.Fatalf("aller-retour %s -> %s", mode, got)
		}
	}
	// 'P' and 'U' existed only because Access stored them (§12.3).
	for _, bad := range []string{"P", "U", "au_poids", ""} {
		if _, err := parseSaleMode(bad); err == nil {
			t.Errorf("parseSaleMode(%q) accepté", bad)
		}
	}
}

func TestParseQualificationMatchesTheDomainSpelling(t *testing.T) {
	for _, q := range []domain.Qualification{domain.Weighable, domain.NotWeighable, domain.Anomaly} {
		got, err := parseQualification(q.String())
		if err != nil {
			t.Fatalf("parseQualification(%q): %v", q.String(), err)
		}
		if got != q {
			t.Fatalf("aller-retour %s -> %s", q, got)
		}
	}
	for _, bad := range []string{"pesable", "hidden", ""} {
		if _, err := parseQualification(bad); err == nil {
			t.Errorf("parseQualification(%q) accepté", bad)
		}
	}
}

func TestParseStabilityMatchesTheDomainSpelling(t *testing.T) {
	for _, s := range []domain.Stability{
		domain.Stable, domain.Unstable, domain.StabilityUnknown, domain.StabilityNotApplicable,
	} {
		got, err := parseStability(s.String())
		if err != nil {
			t.Fatalf("parseStability(%q): %v", s.String(), err)
		}
		if got != s {
			t.Fatalf("aller-retour %s -> %s", s, got)
		}
	}
	for _, bad := range []string{"instable", "sans_objet", ""} {
		if _, err := parseStability(bad); err == nil {
			t.Errorf("parseStability(%q) accepté", bad)
		}
	}
}

func TestNotFoundTranslatesOnlyNoRows(t *testing.T) {
	if !errors.Is(notFound(sql.ErrNoRows), ErrNotFound) {
		t.Error("sql.ErrNoRows n'est pas traduite en ErrNotFound")
	}
	broken := errors.New("disk I/O error")
	if !errors.Is(notFound(broken), broken) {
		t.Error("une erreur réelle a été transformée en ErrNotFound")
	}
	if notFound(nil) != nil {
		t.Error("notFound(nil) doit rendre nil")
	}
}

// TestDSNPathEscapesWhatWouldEndThePath: '?' and '#' would silently open a DIFFERENT
// database, and a data directory is not under our control.
//
// This half of the contract is INVARIANT, so it is tested with separators that mean
// the same thing everywhere. The other half — turning a Windows separator into the
// forward slash a DSN needs — is by nature platform-dependent, and lives in its own
// test below.
func TestDSNPathEscapesWhatWouldEndThePath(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`/var/lib/openscale/openscale.db`, `/var/lib/openscale/openscale.db`},
		{`/var/lib/dossier?piege/x.db`, `/var/lib/dossier%3Fpiege/x.db`},
		{`/var/lib/dossier#1/x.db`, `/var/lib/dossier%231/x.db`},
		{`/var/lib/100%/x.db`, `/var/lib/100%25/x.db`},
		// '%' first, because escaping it after the others would double-escape them.
		{`/a%b?c#d.db`, `/a%25b%3Fc%23d.db`},
	} {
		if got := dsnPath(c.in); got != c.want {
			t.Errorf("dsnPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestDSNPathUsesTheSeparatorOfThisPlatform is the platform-dependent half, and the
// reason it is separate is worth stating: filepath.ToSlash converts a backslash on
// Windows and deliberately does NOT on Linux, where a backslash is a legal character
// in a file name. A test that demanded the conversion everywhere would be asking the
// code to corrupt a legitimate Linux path — and it did, until the CI caught it on a
// runner that is not the machine this was written on.
func TestDSNPathUsesTheSeparatorOfThisPlatform(t *testing.T) {
	native := filepath.Join("data", "openscale.db")
	got := dsnPath(native)

	if strings.Contains(got, `\`) {
		t.Errorf("dsnPath(%q) = %q : un DSN se lit avec des barres obliques", native, got)
	}
	if want := "data/openscale.db"; got != want {
		t.Errorf("dsnPath(%q) = %q, want %q", native, got, want)
	}
}

func TestPageSizeIsBounded(t *testing.T) {
	for _, c := range []struct{ in, want int }{
		{0, defaultPageSize},
		{-1, defaultPageSize},
		{25, 25},
		{maxPageSize, maxPageSize},
		{maxPageSize + 1, maxPageSize},
		{1_000_000, maxPageSize},
	} {
		if got := pageSize(c.in); got != c.want {
			t.Errorf("pageSize(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestRowBoundAndDayBoundSpellNoLimit: getting these two backwards would erase a
// journal on a misconfigured file.
func TestRowBoundAndDayBoundSpellNoLimit(t *testing.T) {
	if rowBound(0) <= 1<<40 {
		t.Errorf("rowBound(0) = %d ; « aucune borne » doit rendre MAX(id) - borne négatif", rowBound(0))
	}
	if rowBound(5000) != 5000 {
		t.Errorf("rowBound(5000) = %d", rowBound(5000))
	}
	db := OpenTest(t)
	if got := db.dayBound(0); got != "" {
		t.Errorf("dayBound(0) = %q, want la chaîne vide", got)
	}
	if got := db.dayBound(90); got != formatTime(TestEpoch.AddDate(0, 0, -90)) {
		t.Errorf("dayBound(90) = %q", got)
	}
}

// TestBackupStampSortsOnTheTimestamp: sorting the whole name would put .before-v10-
// before .before-v9- and delete the wrong copy.
func TestBackupStampSortsOnTheTimestamp(t *testing.T) {
	v9 := backupStamp("openscale.db.before-v9-20260312T090000")
	v10 := backupStamp("openscale.db.before-v10-20260313T090000")
	if !(v9 < v10) {
		t.Fatalf("horodates mal ordonnées : %q puis %q", v9, v10)
	}
	if strings.Contains(v9, "before") {
		t.Fatalf("backupStamp rend %q ; il doit rendre l'horodate seule", v9)
	}
	if got := backupStamp("sans_tiret"); got != "sans_tiret" {
		t.Fatalf("backupStamp(%q) = %q", "sans_tiret", got)
	}
}
