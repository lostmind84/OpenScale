package catalog_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"openscale/internal/catalog"
	"openscale/internal/domain"
)

// The photos of an assembly, and the one rule that governs all of them: A PHOTO IS
// NEVER WORTH A PRODUCT. The same image on two products is one file, a photo the sink
// refuses leaves the product without its photo and never without its place in the grid,
// and a station configured to keep none opens none at all.

// --- Les photos, qui sont les règles de §10.7 et non celles d'un format ----------

// TestTheSamePhotoOnTwoProductsIsOneFile.
//
// The sha IS the address, which is what turns 181 rows carrying a photo into 165 files
// written — and what makes a re-import write nothing at all (§10.7).
func TestTheSamePhotoOnTwoProductsIsOneFile(t *testing.T) {
	shared := pngOf(t, 8, 8)
	rows := manyGoodRows(t, 10)
	rows[0].Image, rows[1].Image = shared, shared
	sink := newCollecting()

	batch, err := catalog.Assemble(reads(rows...), catalog.AssembleOptions{
		KeepPhotos: true, Images: sink})
	if err != nil {
		t.Fatalf("Assemble : %v", err)
	}
	if len(batch.Images) != 1 {
		t.Fatalf("%d image(s) dans le lot, attendu 1", len(batch.Images))
	}
	if len(sink.put) != 1 {
		t.Fatalf("%d empreinte(s) écrite(s), attendu 1", len(sink.put))
	}
	for sha, times := range sink.put {
		if times != 1 {
			t.Errorf("la photo %s a été écrite %d fois", sha[:8], times)
		}
	}
	if batch.Products[0].ImageSHA == "" || batch.Products[0].ImageSHA != batch.Products[1].ImageSHA {
		t.Errorf("les deux produits ne partagent pas l'adresse : %q et %q",
			batch.Products[0].ImageSHA, batch.Products[1].ImageSHA)
	}
}

// TestAPhotoRefusedLosesItsPhotoAndNeverItsProduct: les deux refus de §10.7 sont NON
// BLOQUANTS — le produit garde sa tuile dans les deux cas.
func TestAPhotoRefusedLosesItsPhotoAndNeverItsProduct(t *testing.T) {
	for _, c := range []struct {
		name    string
		image   []byte
		ceiling int
		want    string
	}{
		{"au-delà du plafond", pngOf(t, 64, 64), 64, domain.FindingImageTooLarge},
		{"en-tête d'aucun format accepté", []byte("ceci n'est pas une image"), 0,
			domain.FindingImageInvalid},
	} {
		t.Run(c.name, func(t *testing.T) {
			rows := manyGoodRows(t, 10)
			rows[0].Image = c.image
			batch, err := catalog.Assemble(reads(rows...), catalog.AssembleOptions{
				KeepPhotos: true, MaxImageSize: c.ceiling})
			if err != nil {
				t.Fatalf("Assemble : %v", err)
			}
			if len(batch.Products) != 10 ||
				batch.Products[0].Qualification != domain.Weighable {
				t.Fatal("le produit a perdu sa tuile en même temps que sa photo")
			}
			if batch.Products[0].ImageSHA != "" || len(batch.Images) != 0 {
				t.Fatal("une photo refusée a été adressée quand même")
			}
			if !hasCode(batch.Findings, c.want) {
				t.Errorf("le refus n'est pas classé %s : %v", c.want, codesOf(batch.Findings))
			}
		})
	}
}

// TestAStationThatKeepsNoPhotoOpensNone: `catalog.images.source` à autre chose que les
// photos de la source, et rien n'est décodé ni écrit.
func TestAStationThatKeepsNoPhotoOpensNone(t *testing.T) {
	rows := manyGoodRows(t, 10)
	rows[0].Image = pngOf(t, 8, 8)
	sink := newCollecting()

	batch, err := catalog.Assemble(reads(rows...), catalog.AssembleOptions{
		KeepPhotos: false, Images: sink})
	if err != nil {
		t.Fatalf("Assemble : %v", err)
	}
	if len(batch.Images) != 0 || len(sink.put) != 0 || batch.Products[0].ImageSHA != "" {
		t.Fatal("une photo a été retenue sur un poste qui n'en garde aucune")
	}
}

// TestASinkThatRefusesLeavesTheProductWithoutItsPhoto: un disque plein dégrade le
// confort, jamais le service (principe 6 de §4).
func TestASinkThatRefusesLeavesTheProductWithoutItsPhoto(t *testing.T) {
	rows := manyGoodRows(t, 10)
	rows[0].Image = pngOf(t, 8, 8)

	batch, err := catalog.Assemble(reads(rows...), catalog.AssembleOptions{
		KeepPhotos: true, Images: &collecting{put: map[string]int{}, refuse: true}})
	if err != nil {
		t.Fatalf("Assemble : %v", err)
	}
	if len(batch.Products) != 10 || batch.Products[0].ImageSHA != "" {
		t.Fatal("le produit a suivi le sort de sa photo")
	}
	if !hasCode(batch.Findings, domain.FindingImageInvalid) {
		t.Errorf("le refus du puits n'est pas signalé : %v", codesOf(batch.Findings))
	}
}

// TestANilSinkCountsThePhotosAndKeepsNone: c'est exactement ce que veut une lecture à
// blanc du rapport d'import (§10.3 bis).
func TestANilSinkCountsThePhotosAndKeepsNone(t *testing.T) {
	rows := manyGoodRows(t, 10)
	rows[0].Image = pngOf(t, 8, 8)

	batch, err := catalog.Assemble(reads(rows...), catalog.AssembleOptions{KeepPhotos: true})
	if err != nil {
		t.Fatalf("Assemble : %v", err)
	}
	if len(batch.Images) != 1 || batch.Products[0].ImageSHA == "" {
		t.Fatal("un puits nul doit compter la photo et l'adresser sans l'écrire")
	}
}

// pngOf builds a PNG of the requested size, so that a test about a CEILING carries a
// real image rather than bytes that would be refused for their header instead.
func pngOf(t *testing.T, width, height int) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	// Pseudo-random noise, and both words matter. NOISE, because PNG squeezes a regular
	// picture down to almost nothing and a test about a size ceiling would never reach
	// it. PSEUDO-random, from a generator seeded here, because a test whose subject
	// varies from one run to the next is a test that fails on somebody else's machine.
	seed := uint32(1)
	next := func() uint8 {
		seed = seed*1664525 + 1013904223
		return uint8(seed >> 24)
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			canvas.Set(x, y, color.RGBA{R: next(), G: next(), B: next(), A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		t.Fatalf("encodage du PNG d'essai : %v", err)
	}
	return encoded.Bytes()
}
