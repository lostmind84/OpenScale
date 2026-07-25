package store

import (
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"openscale/internal/domain"
)

// This file loads the two AUTHENTIC exports into a real database. It is a store test
// and not a parser test: what it checks is that one transaction swallows a whole
// measured catalog, that the Odoo ids really are usable as a primary key, and that the
// figures §10.2, §10.7 and §10.9 quote can be reproduced from the files themselves.
//
// The reader below is deliberately naive -- seven positional columns, semicolon,
// quoted fields. It is NOT the production parser (internal/catalog/csvodoo owns
// qualification, the barcode prefix and the images); it only needs to be faithful
// enough for the counts to mean something.

// csvRow is one line of an Odoo export, by position: the header is advisory and the
// order of the seven columns is the contract (§10.2).
type csvRow struct {
	id, name, barcode, price, category, unit, image string
}

func readFixture(t *testing.T, name string) []csvRow {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "catalog", name)
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("ouverture de %s : %v", path, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Comma = ';'
	reader.FieldsPerRecord = 7
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("lecture de %s : %v", name, err)
	}
	if len(records) == 0 {
		t.Fatalf("%s est vide", name)
	}
	// First line is the header: id;nom;code-barre;prix;categorie;unite;image.
	rows := make([]csvRow, 0, len(records)-1)
	for _, r := range records[1:] {
		rows = append(rows, csvRow{r[0], r[1], r[2], r[3], r[4], r[5], r[6]})
	}
	return rows
}

// TestFixtureFiguresMatchTheDocument checks, against the files themselves, every number
// of §10.2/§10.7/§10.9 this package depends on.
func TestFixtureFiguresMatchTheDocument(t *testing.T) {
	cases := []struct {
		file        string
		wantRows    int
		wantBytes   int64
		wantImages  int
		wantNoImage int
		wantNoCode  int
	}{
		// 355 products, 181 images, 174 without -- and 527 233 bytes.
		{"flv.csv", 355, 527_233, 181, 174, 0},
		// 153 products, no image at all, 9 rows without a barcode.
		{"flv_1.csv", 153, 10_413, 0, 153, 9},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			info, err := os.Stat(filepath.Join("..", "..", "testdata", "catalog", c.file))
			if err != nil {
				t.Fatalf("Stat: %v", err)
			}
			if info.Size() != c.wantBytes {
				t.Errorf("taille = %d octets, le document annonce %d", info.Size(), c.wantBytes)
			}

			rows := readFixture(t, c.file)
			if len(rows) != c.wantRows {
				t.Fatalf("%d ligne(s), le document annonce %d", len(rows), c.wantRows)
			}

			ids := make(map[string]bool, len(rows))
			var images, noImage, noCode int
			for _, r := range rows {
				if ids[r.id] {
					t.Fatalf("id %s en doublon ; c'est la clé primaire de products (§10.9)", r.id)
				}
				ids[r.id] = true
				if r.image == "" {
					noImage++
				} else {
					images++
				}
				if r.barcode == "" {
					noCode++
				}
			}
			if images != c.wantImages || noImage != c.wantNoImage {
				t.Errorf("%d avec image / %d sans, le document annonce %d / %d",
					images, noImage, c.wantImages, c.wantNoImage)
			}
			if noCode != c.wantNoCode {
				t.Errorf("%d ligne(s) sans code-barres, le document annonce %d", noCode, c.wantNoCode)
			}
		})
	}
}

// TestFlvIDsAreTheProducerKey checks the claim §10.9 rests on: the ids run from 20 to
// 5209, are not contiguous, and are therefore an identity and never an index.
func TestFlvIDsAreTheProducerKey(t *testing.T) {
	rows := readFixture(t, "flv.csv")

	lowest, highest := rows[0].id, rows[0].id
	for _, r := range rows {
		if len(r.id) < len(lowest) || (len(r.id) == len(lowest) && r.id < lowest) {
			lowest = r.id
		}
		if len(r.id) > len(highest) || (len(r.id) == len(highest) && r.id > highest) {
			highest = r.id
		}
	}
	if lowest != "20" || highest != "5209" {
		t.Fatalf("ids de %s à %s, le document annonce 20 à 5209", lowest, highest)
	}
	if len(rows) == 5209-20+1 {
		t.Fatal("les ids sont contigus ; le document dit qu'ils ne le sont pas")
	}
}

// TestLongestNameAndHeartCount checks the two figures the label renderer and §10.6
// quote: the longest name of flv.csv is 69 characters, and 127 of the 355 names carry
// the U+2665 heart.
func TestLongestNameAndHeartCount(t *testing.T) {
	rows := readFixture(t, "flv.csv")

	longest, hearts := "", 0
	for _, r := range rows {
		if utf8.RuneCountInString(r.name) > utf8.RuneCountInString(longest) {
			longest = r.name
		}
		if strings.ContainsRune(r.name, '♥') {
			hearts++
		}
	}
	if n := utf8.RuneCountInString(longest); n != 69 {
		t.Errorf("nom le plus long : %d caractères (%q), le document annonce 69", n, longest)
	}
	if hearts != 127 {
		t.Errorf("%d nom(s) portent ♥, le document annonce 127", hearts)
	}
}

// TestWholeRealCatalogFitsInOneTransaction is the volumetry check of §12.4 done on the
// real thing: 355 products in one transaction, then the same file again, which must
// update 355 rows and insert none.
func TestWholeRealCatalogFitsInOneTransaction(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)
	db, path := openAt(t, clk)

	products := fixtureProducts(t, "flv.csv")
	if len(products) != 355 {
		t.Fatalf("%d produit(s) convertis, want 355", len(products))
	}

	out, err := db.ReplaceCatalog(ctx, batch("flv_1.csv", "sha-reel", TestEpoch, products...))
	if err != nil {
		t.Fatalf("ReplaceCatalog: %v", err)
	}
	if out.Inserted != 355 || out.Updated != 0 || out.Withdrawn != 0 {
		t.Fatalf("outcome = %+v, want 355 insertions", out)
	}

	catalog := mustLoadCatalog(t, db)
	if catalog.Len() != 355 {
		t.Fatalf("%d produit(s) au catalogue, want 355", catalog.Len())
	}

	// Second import of the same content: an UPSERT, not a rebuild (§10.9).
	clk.Advance(24 * 3600 * 1e9)
	again, err := db.ReplaceCatalog(ctx, batch("flv_1.csv", "sha-reel", clk.Now(), products...))
	if err != nil {
		t.Fatalf("second ReplaceCatalog: %v", err)
	}
	if again.Inserted != 0 || again.Updated != 355 {
		t.Fatalf("outcome = %+v, want 0 insertion et 355 mises à jour", again)
	}

	// And the whole thing stays well under the 4 MB §12.4 announces for a complete
	// database -- images excluded, they are files addressed by their sha.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() > 4<<20 {
		t.Errorf("base de %d octets pour 355 produits ; §12.4 annonce moins de 4 Mo au total", info.Size())
	}
}

// TestRealCatalogWithoutImagesLoads covers flv_1.csv, the file that legitimately has no
// photo at all.
func TestRealCatalogWithoutImagesLoads(t *testing.T) {
	ctx := context.Background()
	db := OpenTest(t)

	products := fixtureProducts(t, "flv_1.csv")
	if len(products) != 153 {
		t.Fatalf("%d produit(s), want 153", len(products))
	}
	if _, err := db.ReplaceCatalog(ctx, batch("flv_1.csv", "sha-153", TestEpoch, products...)); err != nil {
		t.Fatalf("ReplaceCatalog: %v", err)
	}
	catalog := mustLoadCatalog(t, db)
	if catalog.Len() != 153 {
		t.Fatalf("%d produit(s), want 153", catalog.Len())
	}
	for _, p := range catalog.Products() {
		if p.ImageSHA != "" {
			t.Fatalf("produit %s porte une image ; flv_1.csv n'en contient aucune", p.ID)
		}
	}
}

// fixtureProducts turns fixture rows into products the store can hold.
//
// It applies only what the columns say, plus the two things the schema requires: a
// category code among the configured four, and a reference of 0 or 13 characters. The
// real qualification belongs to internal/catalog/csvodoo.
func fixtureProducts(t *testing.T, file string) []domain.Product {
	t.Helper()
	// F/L/V/A -> the four configured categories; anything else lands on
	// fallback_category (§10.2 bis).
	category := map[string]string{"F": "fruits", "L": "vegetables", "V": "bulk", "A": "other"}

	rows := readFixture(t, file)
	products := make([]domain.Product, 0, len(rows))
	for i, r := range rows {
		cents, err := domain.ParseCents(r.price)
		if err != nil {
			t.Fatalf("%s ligne %d : prix %q : %v", file, i+2, r.price, err)
		}
		if cents > domain.MaxUnitPrice {
			t.Fatalf("%s ligne %d : prix %d centimes au-delà de MaxUnitPrice", file, i+2, cents)
		}
		code, ok := category[r.category]
		if !ok {
			code = "other"
		}
		reference := r.barcode
		if len(reference) != 0 && len(reference) != 13 {
			t.Fatalf("%s ligne %d : code-barres de %d caractères (%q)", file, i+2, len(reference), reference)
		}
		products = append(products, domain.Product{
			ID: r.id, Name: r.name, Reference: domain.EAN13(reference),
			Mode: domain.ByWeight, PriceSuffix: " €/kg", UnitPrice: cents,
			CategoryCode: code, Qualification: domain.Weighable, CSVLine: i + 2,
		})
	}
	return products
}
