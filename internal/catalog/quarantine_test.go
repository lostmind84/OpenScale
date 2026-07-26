package catalog_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"openscale/internal/catalog"
	"openscale/internal/domain"
)

// ledger is the quarantine table, reduced to what §10.5 asks of it: count by sha, read
// by sha. It is a map because the arithmetic is the subject here; the SQL version is
// exercised end to end by failure test 9.
type ledger struct {
	entries map[string]domain.QuarantineEntry
	now     time.Time
	failing bool
}

func newLedger() *ledger {
	return &ledger{
		entries: map[string]domain.QuarantineEntry{},
		now:     time.Date(2026, 7, 24, 15, 38, 12, 0, time.UTC),
	}
}

// RecordContentFailure counts one failure against a content.
func (l *ledger) RecordContentFailure(_ context.Context, sha, code, reason string) (domain.QuarantineEntry, error) {
	if l.failing {
		return domain.QuarantineEntry{}, errors.New("base indisponible")
	}
	entry, seen := l.entries[sha]
	if !seen {
		entry = domain.QuarantineEntry{SHA256: sha, FirstFailureAt: l.now}
	}
	entry.FailureCount++
	entry.LastFailureAt, entry.Code, entry.Reason = l.now, code, reason
	l.entries[sha] = entry
	return entry, nil
}

// Quarantine reports what stands against a content.
func (l *ledger) Quarantine(_ context.Context, sha string) (domain.QuarantineEntry, error) {
	entry, seen := l.entries[sha]
	if !seen {
		return domain.QuarantineEntry{}, errors.New("contenu jamais refusé")
	}
	return entry, nil
}

// contentFailure is what the parser hands back when a file cannot become a catalog.
func contentFailure(sha string) error {
	return catalog.Refused(sha, 527233, fmt.Errorf("%w : 34 lignes illisibles sur 355", catalog.ErrContent))
}

// TestThreeRefusalsOfTheSameContentBanIt is the counter of §10.5.
//
// By SHA and never by file name: the name is flv_2.csv every night, and it is the
// CONTENT that fails.
func TestThreeRefusalsOfTheSameContentBanIt(t *testing.T) {
	const sha = "40ed0f86b79d37beb3def0f587b42c127c72ee6d2c601c2761085bd63413d519"
	ctx := context.Background()
	book := newLedger()
	quarantine := catalog.NewQuarantine(book, 3)

	for attempt := 1; attempt <= 2; attempt++ {
		entry, counted := quarantine.Count(ctx, contentFailure(sha))
		if !counted || entry.FailureCount != attempt {
			t.Fatalf("essai %d : compté=%v, %d échec(s)", attempt, counted, entry.FailureCount)
		}
		if _, banned := quarantine.Banned(ctx, sha); banned {
			t.Fatalf("banni au bout de %d refus alors que le seuil est 3 : un producteur "+
				"qui corrige son fichier doit pouvoir le redéposer", attempt)
		}
	}
	if _, counted := quarantine.Count(ctx, contentFailure(sha)); !counted {
		t.Fatal("le troisième refus n'a pas été compté")
	}
	entry, banned := quarantine.Banned(ctx, sha)
	if !banned {
		t.Fatal("trois refus du même contenu ne l'ont pas banni")
	}
	if entry.Code != "ERR-CAT-03" {
		t.Errorf("code %q, attendu ERR-CAT-03 : c'est un échec de CONTENU", entry.Code)
	}
	// Another content is another count: the ban follows the bytes.
	if _, banned := quarantine.Banned(ctx, "004f179695bbed6108c5a05bfafea96834ea59bc9f4817f2e58505cbc792f435"); banned {
		t.Error("un autre contenu a hérité du bannissement du premier")
	}
}

// TestAFileThatCouldNotBeDeletedIsNeverCounted is the trap of §16.2 line 11, and it is
// the reason Count takes an ERROR rather than a sha.
//
// The file is not corrupted, the directory is. Counting it would light a red light on a
// perfectly good catalog, and after three false alarms the team stops looking at the
// lights (§10.5).
func TestAFileThatCouldNotBeDeletedIsNeverCounted(t *testing.T) {
	ctx := context.Background()
	book := newLedger()
	quarantine := catalog.NewQuarantine(book, 3)

	for _, err := range []error{
		fmt.Errorf("%w : droits en écriture manquants sur \\\\serveur\\balance\\ pour le compte balance",
			catalog.ErrNotAcknowledged),
		errors.New("le partage ne répond pas"),
		context.Canceled,
		nil,
	} {
		if _, counted := quarantine.Count(ctx, err); counted {
			t.Fatalf("compté en quarantaine : %v", err)
		}
	}
	if len(book.entries) != 0 {
		t.Fatalf("%d entrée(s) de quarantaine pour zéro échec de contenu", len(book.entries))
	}
}

// TestARefusalWithoutADigestIsNotCounted: a content nobody can name cannot be counted
// three times, and pretending otherwise would ban the next file that also fails to be
// identified.
func TestARefusalWithoutADigestIsNotCounted(t *testing.T) {
	book := newLedger()
	quarantine := catalog.NewQuarantine(book, 3)
	if _, counted := quarantine.Count(context.Background(),
		catalog.Refused("", 0, catalog.ErrContent)); counted {
		t.Error("un refus sans empreinte a été compté")
	}
}

// TestALedgerThatIsUnavailableRefusesNothing: the quarantine is a counter, not a gate.
// A database that will not answer must not turn every import into a refusal.
func TestALedgerThatIsUnavailableRefusesNothing(t *testing.T) {
	book := newLedger()
	book.failing = true
	quarantine := catalog.NewQuarantine(book, 3)
	if _, counted := quarantine.Count(context.Background(), contentFailure("ab")); counted {
		t.Error("un échec d'écriture en quarantaine a été rapporté comme compté")
	}
}

// TestAQuarantineWithoutALedgerIsANoOp: a source exercised without a database must
// still be able to refuse a file — the archive, the reason and the removal owe nothing
// to the counter.
func TestAQuarantineWithoutALedgerIsANoOp(t *testing.T) {
	ctx := context.Background()
	for _, quarantine := range []*catalog.Quarantine{nil, catalog.NewQuarantine(nil, 3)} {
		if _, counted := quarantine.Count(ctx, contentFailure("ab")); counted {
			t.Error("compté sans registre")
		}
		if _, banned := quarantine.Banned(ctx, "ab"); banned {
			t.Error("banni sans registre")
		}
		if got := quarantine.Threshold(); got != catalog.DefaultFailuresBeforeReject {
			t.Errorf("seuil %d, attendu %d", got, catalog.DefaultFailuresBeforeReject)
		}
	}
}

// TestAThresholdBelowOneFallsBackOnTheShippedValue: a configuration that lost the line
// keeps the guard rather than banning every file at the first refusal.
func TestAThresholdBelowOneFallsBackOnTheShippedValue(t *testing.T) {
	if got := catalog.NewQuarantine(newLedger(), 0).Threshold(); got != 3 {
		t.Errorf("seuil %d, attendu 3 (§11.2)", got)
	}
	if catalog.DefaultFailuresBeforeReject != 3 {
		t.Errorf("failures_before_reject par défaut = %d, §11.2 dit 3",
			catalog.DefaultFailuresBeforeReject)
	}
}

// TestARefusalCarriesTheContentItBearsOn: without the digest, §10.5 cannot count.
func TestARefusalCarriesTheContentItBearsOn(t *testing.T) {
	const sha = "40ed0f86b79d37beb3def0f587b42c127c72ee6d2c601c2761085bd63413d519"
	err := contentFailure(sha)
	if !errors.Is(err, catalog.ErrContent) {
		t.Fatalf("le refus ne s'annonce pas comme un échec de contenu : %v", err)
	}
	content, ok := catalog.ContentOf(err)
	if !ok {
		t.Fatal("le refus ne nomme pas le contenu refusé")
	}
	if content.SHA256 != sha || content.Bytes != 527233 {
		t.Errorf("contenu %s / %d octets", content.SHA256, content.Bytes)
	}
	for _, fragment := range []string{"illisibles", "355"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("le message perd la cause (%q absent) : %v", fragment, err)
		}
	}
	if _, ok := catalog.ContentOf(catalog.ErrNotAcknowledged); ok {
		t.Error("un échec d'acquittement s'est fait passer pour un échec de contenu")
	}
}
