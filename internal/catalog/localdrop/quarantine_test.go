package localdrop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"openscale/internal/catalog"
	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/station/ports"
)

// A content this source could not turn into rows is counted under ITS OWN DIGEST, so
// that the same broken file arriving twice is one entry and not two. What is asserted
// here is the boundary: a failure of the CONTENT reaches the ledger, a failure of the
// FILE SYSTEM never does.

// TestNextSurfacesAContentFailureSoThatItIsJournalled: the station logs ERR-CAT-03
// and calls Next again — the file is already gone, so it does not spin (§10.5).
func TestNextSurfacesAContentFailureSoThatItIsJournalled(t *testing.T) {
	source, clock := station(t, "")
	drop(t, source, "\"id\";\"nom\"\r\n\"20\";\"UNE SEULE COLONNE UTILE\"\r\n")

	done := make(chan error, 1)
	go func() {
		_, err := source.Next(context.Background())
		done <- err
	}()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case err := <-done:
			if !errors.Is(err, catalog.ErrContent) {
				t.Fatalf("Next a rendu %v, attendu ERR-CAT-03", err)
			}
			if _, err := os.Stat(source.Path()); !errors.Is(err, os.ErrNotExist) {
				t.Error("le fichier refusé est resté : la scrutation suivante le relirait")
			}
			return
		case <-deadline:
			t.Fatal("Next n'a rien rendu")
		default:
			clock.Advance(5 * time.Second)
		}
	}
}

// book is the quarantine table reduced to the two calls §10.5 makes of it.
//
// A map rather than the real SQLite one, because what is under test here is WHICH
// failures reach the counter — the arithmetic is exercised against the real table by
// failure test 9, end to end.
type book struct {
	counted map[string]int
	codes   map[string]string
}

func newBook() *book {
	return &book{counted: map[string]int{}, codes: map[string]string{}}
}

// RecordContentFailure counts one failure against a content.
func (b *book) RecordContentFailure(_ context.Context, sha, code, reason string) (domain.QuarantineEntry, error) {
	b.counted[sha]++
	b.codes[sha] = code
	return domain.QuarantineEntry{SHA256: sha, FailureCount: b.counted[sha], Code: code, Reason: reason}, nil
}

// Quarantine reports what stands against a content.
func (b *book) Quarantine(_ context.Context, sha string) (domain.QuarantineEntry, error) {
	count, seen := b.counted[sha]
	if !seen {
		return domain.QuarantineEntry{}, errors.New("contenu jamais refusé")
	}
	return domain.QuarantineEntry{SHA256: sha, FailureCount: count, Code: b.codes[sha]}, nil
}

// counting builds a source whose refusals are counted, at station number 2.
func counting(t *testing.T, options string) (*Source, *book, *recorder) {
	t.Helper()
	ledger, journal := newBook(), &recorder{}
	source, err := New(catalog.SourceConfig{
		Catalog: domain.CatalogConfig{
			Options:          driverOptions(t, options),
			Images:           domain.ImagesConfig{Source: domain.ImageSourceCSV},
			FallbackCategory: "other",
		},
		StationNumber: 2,
		DataDir:       t.TempDir(),
		Clock:         fake.NewClock(t0),
		Log:           journal,
		Quarantine:    ledger,
	})
	if err != nil {
		t.Fatalf("construction de la source : %v", err)
	}
	t.Cleanup(func() { source.Close() })
	return source, ledger, journal
}

// TestARefusedContentIsCountedUnderItsOwnDigest.
//
// The quarantine of §10.5 is indexed by sha256, so the refusal has to carry the digest
// of what it refused: three drops of the same broken content must reach three, and the
// name of the file — identical every night — must play no part in it.
func TestARefusedContentIsCountedUnderItsOwnDigest(t *testing.T) {
	source, ledger, journal := counting(t, "")
	broken := "\"id\";\"nom\"\r\n\"20\";\"UNE SEULE COLONNE UTILE\"\r\n"
	sum := sha256.Sum256([]byte(broken))
	sha := hex.EncodeToString(sum[:])

	for attempt := 1; attempt <= 3; attempt++ {
		drop(t, source, broken)
		source.poll(context.Background())
		if _, err := source.poll(context.Background()); !errors.Is(err, catalog.ErrContent) {
			t.Fatalf("essai %d : erreur %v", attempt, err)
		}
		if ledger.counted[sha] != attempt {
			t.Fatalf("essai %d : %d échec(s) comptés sous %s", attempt, ledger.counted[sha], sha)
		}
	}
	if ledger.codes[sha] != "ERR-CAT-03" {
		t.Errorf("code %q, attendu ERR-CAT-03", ledger.codes[sha])
	}
	if len(ledger.counted) != 1 {
		t.Errorf("%d contenus comptés, attendu un seul : le compteur suit les octets, "+
			"pas le nom du fichier", len(ledger.counted))
	}
	// The light goes red on the third refusal and not before: a producer who fixes the
	// file after one bad export must not find a station that has already given up.
	levels := make([]string, 0, 3)
	for _, e := range journal.entries {
		if e.code == "ERR-CAT-03" && e.message == "Catalogue refusé, fichier mis de côté." {
			levels = append(levels, e.level)
		}
	}
	want := []string{domain.LevelWarn, domain.LevelWarn, domain.LevelError}
	if len(levels) != 3 || levels[0] != want[0] || levels[1] != want[1] || levels[2] != want[2] {
		t.Errorf("niveaux %v, attendu %v", levels, want)
	}
}

// TestAFileThatCannotBeDeletedReachesNoCounter is the second half of the trap of §16.2
// line 11, and this is where the REAL Acknowledge runs.
//
// The removal is the only thing injected, because no portable file system produces a
// file that can be read and not deleted. Everything else — the parse, the archive, the
// error, the counter that must stay at zero — is the shipped code.
func TestAFileThatCannotBeDeletedReachesNoCounter(t *testing.T) {
	source, ledger, journal := counting(t, "")
	source.remove = func(string) error { return os.ErrPermission }
	drop(t, source, aCatalog)
	source.poll(context.Background())
	batch, err := source.poll(context.Background())
	if err != nil || batch == nil {
		t.Fatalf("lecture : %v", err)
	}

	err = source.Acknowledge(context.Background(), batch,
		ports.BatchResult{Result: domain.ImportApplied})
	if !errors.Is(err, catalog.ErrNotAcknowledged) {
		t.Fatalf("erreur %v, attendu ERR-CAT-05", err)
	}
	if len(ledger.counted) != 0 {
		t.Fatalf("%d contenu(s) comptés : une suppression impossible n'est PAS un échec "+
			"de contenu, et un feu rouge qui se déclenche à tort est le pire ennemi de "+
			"l'exploitation", len(ledger.counted))
	}
	for _, e := range journal.entries {
		if e.code == "ERR-CAT-03" {
			t.Errorf("ERR-CAT-03 journalisé pour un fichier parfaitement lisible : %+v", e)
		}
	}
}

// truncated is a file cut off in mid-flight: eight products, then two lines that are
// not products at all. Eight readable out of ten is 80 %, under the 90 % of §10.4a.
func truncated() string {
	file := "\"id\";\"nom\";\"code-barre\";\"prix\";\"categorie\";\"unite\";\"image\"\r\n"
	for i := 0; i < 8; i++ {
		file += fmt.Sprintf("\"%d\";\"LENTILLES VERTES %d\";\"0493171000007\";\"7.89\";\"V\";\"kg\";\"\"\r\n",
			20+i, i)
	}
	return file + "\"90\";\"COUPE EN PLEIN VOL\"\r\n\"91\";\"COUPE AUSSI\"\r\n"
}

// TestAFileRefusedByTheAbsoluteGuardIsCountedToo.
//
// A file can be refused for two very different reasons — it does not parse at all, or
// too few of its lines are products — and BOTH are content failures of §10.5. The
// second one only shows up after the whole file has been read, so it is the one whose
// digest is easiest to lose; without it the same truncated export could be re-dropped
// for ever without the counter ever reaching three.
func TestAFileRefusedByTheAbsoluteGuardIsCountedToo(t *testing.T) {
	source, ledger, _ := counting(t, "")
	content := truncated()
	sum := sha256.Sum256([]byte(content))
	sha := hex.EncodeToString(sum[:])

	drop(t, source, content)
	source.poll(context.Background())
	_, err := source.poll(context.Background())
	if !errors.Is(err, catalog.ErrContent) {
		t.Fatalf("erreur %v, attendu un échec de contenu", err)
	}
	if !strings.Contains(err.Error(), "illisible") {
		t.Errorf("le motif ne dit pas ce qui manque : %v", err)
	}
	if ledger.counted[sha] != 1 {
		t.Fatalf("compté %v, attendu un échec sous %s : un lot refusé par le garde "+
			"ABSOLU est un échec de contenu comme un autre", ledger.counted, sha)
	}
}

// TestAFileRefusedBeforeItsFirstRowIsCountedToo closes the last hole of §10.5.
//
// A refusal can happen before a single product has been read — an empty file, or one
// past the ceiling of §10.1 — and those are the ones whose digest is easiest to lose,
// because there is no batch to hang it on. Without them the same unusable file could be
// re-dropped for ever without the counter ever reaching three.
//
// The digest of a file past the ceiling covers what was READ and not the whole file,
// which is what catalog.ContentError says it is: it still identifies the thing that
// keeps coming back, which is all §10.5 asks of it.
func TestAFileRefusedBeforeItsFirstRowIsCountedToo(t *testing.T) {
	for _, c := range []struct {
		what    string
		options string
		content string
		read    int
		says    string
	}{
		{"fichier vide", "", "", 0, "vide"},
		{"fichier au-delà du plafond", `{"max_file_size_mb": 1}`,
			strings.Repeat("x", 1<<20+64), 1<<20 + 1, "plafond"},
	} {
		t.Run(c.what, func(t *testing.T) {
			source, ledger, _ := counting(t, c.options)
			sum := sha256.Sum256([]byte(c.content)[:c.read])
			sha := hex.EncodeToString(sum[:])

			drop(t, source, c.content)
			source.poll(context.Background())
			_, err := source.poll(context.Background())
			if !errors.Is(err, catalog.ErrContent) {
				t.Fatalf("erreur %v, attendu un échec de contenu", err)
			}
			if !strings.Contains(err.Error(), c.says) {
				t.Errorf("le motif ne dit pas %q : %v", c.says, err)
			}
			if ledger.counted[sha] != 1 {
				t.Fatalf("compté %v, attendu un échec sous %s", ledger.counted, sha)
			}
		})
	}
}
