package store

import (
	"context"
	"testing"
	"time"

	"openscale/internal/domain"
)

// Reading a catalog back out: the snapshot a station starts on, which applies the grid
// predicate rather than deleting anything, the categories in the order the
// configuration declares them, and the photos addressed by their content — the same
// image stored once, whatever the number of products carrying it.

// TestLoadCatalogAppliesTheGridPredicate: the predicate of §12.3 lives in SQL, once,
// so that no consumer can forget a clause it never sees.
func TestLoadCatalogAppliesTheGridPredicate(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)
	db, _ := openAt(t, clk)

	prepackaged := product("40", "CONFITURE 250g", "3760123456789", 350)
	prepackaged.Qualification = domain.NotWeighable
	prepackaged.Reason = domain.FindingPrepackagedProduct

	if _, err := db.ReplaceCatalog(ctx, batch("flv_1.csv", "sha-1", TestEpoch,
		product("20", "AIL", "0493021000003", 532),
		product("32", "AMANDES", "0493117000009", 1605),
		prepackaged,
	)); err != nil {
		t.Fatalf("ReplaceCatalog: %v", err)
	}

	// A prepackaged product is a row of the catalog; it simply gets no tile.
	catalog := mustLoadCatalog(t, db)
	if catalog.Len() != 3 {
		t.Fatalf("%d ligne(s) au catalogue, want 3", catalog.Len())
	}
	if catalog.WeighableCount() != 2 {
		t.Fatalf("%d tuile(s), want 2", catalog.WeighableCount())
	}

	// A human refusal removes the product from the snapshot entirely: the front must not
	// be able to show a tile for something the shop decided to stop offering.
	if err := db.SaveDecision(ctx, domain.LocalDecision{
		ProductID: "32", Offered: false, Reason: "code appartenant à un autre article",
		DecidedAt: clk.Now(), DecidedBy: "bénévole",
	}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	catalog = mustLoadCatalog(t, db)
	if _, ok := catalog.ByID("32"); ok {
		t.Fatal("un produit non proposé reste dans la grille")
	}
	if catalog.WeighableCount() != 1 {
		t.Fatalf("%d tuile(s) après la décision locale, want 1", catalog.WeighableCount())
	}
}

func TestLoadCatalogReturnsTheConfiguredCategoriesInOrder(t *testing.T) {
	db := OpenTest(t)
	seedCatalog(t, db, product("20", "AIL", "0493021000003", 532))

	categories := mustLoadCatalog(t, db).Categories()
	if len(categories) != 4 {
		t.Fatalf("%d catégorie(s), want 4", len(categories))
	}
	want := []string{"fruits", "vegetables", "bulk", "other"}
	for i, code := range want {
		if categories[i].Code != code {
			t.Fatalf("catégorie %d = %q, want %q", i, categories[i].Code, code)
		}
	}
}

// TestImagesAreAddressedByContent: re-importing the same catalog recomputes the
// fingerprints and writes no new row -- which is what makes an import idempotent (§10.7).
func TestImagesAreAddressedByContent(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)
	db, _ := openAt(t, clk)

	// 10 of the 181 images of flv.csv are PNGs the legacy application named .jpg. The
	// stored format is the REAL one.
	img := domain.Image{SHA256: "abc123", Format: domain.ImagePNG, ByteCount: 1400, Width: 120, Height: 120, SeenAt: TestEpoch}
	withImage := product("20", "AIL", "0493021000003", 532)
	withImage.ImageSHA = img.SHA256

	b := batch("flv_1.csv", "sha-1", TestEpoch, withImage)
	b.Images = []domain.Image{img}
	b.Import.ImagesDecoded = 1
	if _, err := db.ReplaceCatalog(ctx, b); err != nil {
		t.Fatalf("ReplaceCatalog: %v", err)
	}

	got, err := db.Image(ctx, "abc123")
	if err != nil {
		t.Fatalf("Image: %v", err)
	}
	if got.Format != domain.ImagePNG {
		t.Fatalf("format = %q, want png : l'extension ne fait pas foi", got.Format)
	}

	clk.Advance(24 * time.Hour)
	img.SeenAt = clk.Now()
	b2 := batch("flv_1.csv", "sha-2", clk.Now(), withImage)
	b2.Images = []domain.Image{img}
	if _, err := db.ReplaceCatalog(ctx, b2); err != nil {
		t.Fatalf("second ReplaceCatalog: %v", err)
	}
	var n int
	if err := db.reader.QueryRow(`SELECT COUNT(*) FROM images`).Scan(&n); err != nil {
		t.Fatalf("COUNT images: %v", err)
	}
	if n != 1 {
		t.Fatalf("%d image(s) en base, want 1 : l'adressage par sha n'est pas idempotent", n)
	}
	if again, _ := db.Image(ctx, "abc123"); !again.SeenAt.Equal(clk.Now()) {
		t.Errorf("seen_at = %s, want %s", again.SeenAt, clk.Now())
	}
}

func TestImageAndProductReportNotFound(t *testing.T) {
	ctx := context.Background()
	db := OpenTest(t)

	if _, err := db.Image(ctx, "inconnu"); err != ErrNotFound {
		t.Fatalf("Image(inconnu) = %v, want ErrNotFound", err)
	}
	if _, err := db.Product(ctx, "inconnu"); err != ErrNotFound {
		t.Fatalf("Product(inconnu) = %v, want ErrNotFound", err)
	}
}

// TestCatalogWithoutImagesIsANormalCase: flv_1.csv is exactly that, and it must not
// raise anything.
func TestCatalogWithoutImagesIsANormalCase(t *testing.T) {
	db := OpenTest(t)
	out := seedCatalog(t, db,
		product("20", "LENTILLES", "0493171000007", 789),
		product("32", "AMANDES", "0493117000009", 1605),
	)
	if out.Inserted != 2 {
		t.Fatalf("outcome = %+v", out)
	}
	catalog := mustLoadCatalog(t, db)
	for _, p := range catalog.Products() {
		if p.ImageSHA != "" {
			t.Errorf("produit %s : ImageSHA = %q, want vide", p.ID, p.ImageSHA)
		}
	}
}
