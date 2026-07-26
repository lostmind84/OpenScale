package csvodoo_test

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"openscale/internal/catalog"
	"openscale/internal/catalog/csvodoo"
	"openscale/internal/domain"
)

// The fourteen corrupted variants of failure test 9 (§16.2), every one of them
// DERIVED from an authentic export rather than invented: what a broken file looks
// like is decided by what a whole one looks like.
//
// The expectation of each is stated in full, and two of them are worth reading twice
// because they say something the specification does not:
//
//   - a file truncated in mid-flight is NOT caught by the absolute guard. Cutting a
//     CSV in half leaves every surviving line perfectly readable — it loses products,
//     it does not mangle them. What catches it is the stability check before the read
//     (failure test 8) and the RELATIVE guard after it (§10.4b, failure test 12).
//     Pretending otherwise would leave a hole nobody looks at;
//   - a wrong separator, a missing column and an extra column all make EVERY line
//     unreadable, and those are what the absolute guard is for.

// corruption is one way a file arrives broken.
type corruption struct {
	// what names the fault in French, because it is what a failure message reads like.
	what string
	// from is the authentic export the variant is derived from.
	from string
	// build applies the fault.
	build func(source []byte) []byte
	// ceiling overrides max_file_size_mb, in bytes, when the fault is a size.
	ceiling int64

	// refused is true when Parse must return ErrContent and refuse the whole batch.
	refused bool
	// rows, unreadable and products are checked when the batch is accepted.
	rows, unreadable, products int
}

// line returns the index of the n-th line of a CRLF file, header included.
func lineOffset(source []byte, n int) int {
	offset := 0
	for i := 1; i < n; i++ {
		next := bytes.Index(source[offset:], []byte("\r\n"))
		if next < 0 {
			return offset
		}
		offset += next + 2
	}
	return offset
}

// replaceLine rewrites one whole line of a CRLF file.
func replaceLine(source []byte, n int, replacement string) []byte {
	start := lineOffset(source, n)
	end := bytes.Index(source[start:], []byte("\r\n"))
	if end < 0 {
		end = len(source) - start
	}
	out := make([]byte, 0, len(source)+len(replacement))
	out = append(out, source[:start]...)
	out = append(out, replacement...)
	return append(out, source[start+end:]...)
}

// mapLines applies a transformation to every line, header included.
func mapLines(source []byte, apply func(string) string) []byte {
	lines := strings.Split(string(source), "\r\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = apply(line)
		}
	}
	return []byte(strings.Join(lines, "\r\n"))
}

// corruptions is the corpus. The three-figure expectations are MEASURED on the
// variants, not decided in advance.
var corruptions = []corruption{{
	what:    "fichier vide",
	from:    flv1,
	build:   func([]byte) []byte { return nil },
	refused: true,
}, {
	what:    "en-tête seul, aucune ligne de produit",
	from:    flv1,
	build:   func(s []byte) []byte { return s[:lineOffset(s, 2)] },
	refused: true,
}, {
	// Cut inside the hundredth line. The 98 lines before it are perfectly readable —
	// which is exactly why the absolute guard does NOT fire here, and why the two
	// defences against a file caught mid-write are the stability check and the
	// relative guard.
	what:       "tronqué en plein milieu d'une ligne",
	from:       flv1,
	build:      func(s []byte) []byte { return s[:lineOffset(s, 100)+20] },
	rows:       99,
	unreadable: 1,
	products:   98,
}, {
	what: "tronqué à l'intérieur d'un champ entre guillemets",
	from: flv1,
	build: func(s []byte) []byte {
		return append(append([]byte(nil), s[:lineOffset(s, 100)]...), `"1234";"NOM INTERR`...)
	},
	rows:       99,
	unreadable: 1,
	products:   98,
}, {
	what:    "séparateur virgule au lieu du point-virgule",
	from:    flv1,
	build:   func(s []byte) []byte { return bytes.ReplaceAll(s, []byte(`";"`), []byte(`","`)) },
	refused: true,
}, {
	what: "une colonne en moins sur toutes les lignes",
	from: flv1,
	build: func(s []byte) []byte {
		return mapLines(s, func(l string) string { return l[:strings.LastIndex(l, ";")] })
	},
	refused: true,
}, {
	what: "une colonne en plus sur toutes les lignes",
	from: flv1,
	build: func(s []byte) []byte {
		return mapLines(s, func(l string) string { return l + `;"en trop"` })
	},
	refused: true,
}, {
	what: "un identifiant Odoo en double",
	from: flv1,
	build: func(s []byte) []byte {
		return replaceLine(s, 3, `"32";"DOUBLON DE L'ID 32";"0493171000007";"1.00";"V";"kg";""`)
	},
	rows:       153,
	unreadable: 1,
	products:   152,
}, {
	what: "un identifiant vide",
	from: flv1,
	build: func(s []byte) []byte {
		return replaceLine(s, 3, `"";"SANS IDENTIFIANT";"0493171000007";"1.00";"V";"kg";""`)
	},
	rows:       153,
	unreadable: 1,
	products:   152,
}, {
	what: "un nom vide",
	from: flv1,
	build: func(s []byte) []byte {
		return replaceLine(s, 3, `"90001";"";"0493171000007";"1.00";"V";"kg";""`)
	},
	rows:       153,
	unreadable: 1,
	products:   152,
}, {
	what: "un guillemet nu au milieu d'un champ",
	from: flv1,
	build: func(s []byte) []byte {
		return replaceLine(s, 3, `"90002";"NOM "AVEC" GUILLEMETS";"0493171000007";"1.00";"V";"kg";""`)
	},
	rows:       153,
	unreadable: 1,
	products:   152,
}, {
	what: "des octets nuls au milieu du fichier",
	from: flv1,
	build: func(s []byte) []byte {
		start, end := lineOffset(s, 40), lineOffset(s, 60)
		out := append([]byte(nil), s[:start]...)
		out = append(out, bytes.Repeat([]byte{0}, end-start)...)
		return append(out, s[end:]...)
	},
	rows:       133,
	unreadable: 1,
	products:   132,
}, {
	what:    "des noms encodés en Latin-1 au lieu d'UTF-8",
	from:    flv,
	build:   toLatin1,
	refused: true,
}, {
	what:    "un fichier au-delà de max_file_size_mb",
	from:    flv1,
	build:   func(s []byte) []byte { return s },
	ceiling: 4096,
	refused: true,
}}

// toLatin1 re-encodes every character that Latin-1 can carry, which is what a
// producer's export does the day somebody changes a locale.
//
// It is applied to flv.csv on purpose: 45 of its 355 names carry an accent Latin-1
// can hold, that is 12,7 %, which is past the 10 % the absolute guard allows. On
// flv_1.csv the same fault would spoil 12 names out of 153 — 7,8 % — and the file
// would be ACCEPTED, minus twelve products. Two files, two verdicts, one rule.
func toLatin1(source []byte) []byte {
	var out bytes.Buffer
	for _, r := range string(source) {
		if r < 0x100 {
			out.WriteByte(byte(r))
			continue
		}
		out.WriteRune(r)
	}
	return out.Bytes()
}

// TestTheFourteenCorruptedVariants is failure test 9 (§16.2), on the parser side: a
// broken file is either refused whole with ERR-CAT-03, or it names the lines it lost.
// In neither case does it silently become a catalog.
func TestTheFourteenCorruptedVariants(t *testing.T) {
	if len(corruptions) != 14 {
		t.Fatalf("le corpus compte %d variantes, §16.2 en demande 14", len(corruptions))
	}
	for _, c := range corruptions {
		t.Run(c.what, func(t *testing.T) {
			source, err := os.ReadFile(fixture(c.from))
			if err != nil {
				t.Fatalf("lecture de la fixture : %v", err)
			}
			options := csvodoo.Options{FallbackCategory: "other", Now: readAt, MaxFileSize: c.ceiling}
			batch, err := csvodoo.Parse(bytes.NewReader(c.build(source)), options)

			if c.refused {
				if err == nil {
					t.Fatalf("le lot a été accepté : %s", catalog.Summarize(batch))
				}
				if !errors.Is(err, catalog.ErrContent) {
					t.Fatalf("erreur %v, attendu une erreur de contenu (ERR-CAT-03)", err)
				}
				if batch != nil {
					t.Error("un lot refusé ne doit pas être remis : le catalogue N−1 reste en service")
				}
				return
			}

			if err != nil {
				t.Fatalf("le lot a été refusé alors qu'il reste exploitable : %v", err)
			}
			report := catalog.Summarize(batch)
			if report.RowsRead != c.rows || report.UnreadableRows != c.unreadable {
				t.Errorf("%d ligne(s) lue(s) dont %d illisible(s), attendu %d et %d",
					report.RowsRead, report.UnreadableRows, c.rows, c.unreadable)
			}
			if len(batch.Products) != c.products {
				t.Errorf("%d produit(s), attendu %d", len(batch.Products), c.products)
			}
			// Every lost line is NAMED, with its number: a report is a work plan.
			named := 0
			for _, f := range batch.Findings {
				if f.Code == domain.FindingUnreadableRow {
					named++
					if f.CSVLine <= 0 {
						t.Errorf("ligne illisible sans numéro : %s", f.Message)
					}
				}
			}
			if named != c.unreadable {
				t.Errorf("%d ligne(s) illisible(s) nommée(s), attendu %d", named, c.unreadable)
			}
		})
	}
}

// TestARefusedFileNeverProducesABatch: the catalog in service is replaced by a batch
// or by nothing, and a refusal is nothing (§10.4, failure test 9).
func TestARefusedFileNeverProducesABatch(t *testing.T) {
	batch, err := csvodoo.Parse(strings.NewReader(""), csvodoo.Options{})
	if err == nil || batch != nil {
		t.Fatalf("lot %v, erreur %v : un fichier vide ne remplace rien", batch, err)
	}
	if !strings.Contains(err.Error(), "vide") {
		t.Errorf("message « %v » : il doit dire ce qui ne va pas, en français", err)
	}
}
