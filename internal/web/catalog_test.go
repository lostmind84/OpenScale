package web

import (
	"net/http"
	"strings"
	"testing"

	"openscale/internal/domain"
)

// TestTheCatalogIsServedWholeWithAValidator: the grid arrives once, and a browser that
// reconnects revalidates it in a few hundred microseconds instead of downloading it
// again.
func TestTheCatalogIsServedWholeWithAValidator(t *testing.T) {
	b := newBench(t)

	response := b.get("/api/v1/catalog")
	etag := response.Header.Get("ETag")
	page := decodeStatus[catalogDTO](t, response, http.StatusOK)

	if etag == "" || page.Revision != etag {
		t.Fatalf("ETag = %q, révision du corps = %q : les deux doivent coïncider",
			etag, page.Revision)
	}
	if page.ProductCount != 1 || len(page.Products) != 1 {
		t.Fatalf("%d produits servis, attendu 1", page.ProductCount)
	}
	tile := page.Products[0]
	if tile.ID != garlicID || tile.UnitPriceText != "5,32" || tile.PriceSuffix != " €/kg" {
		t.Fatalf("tuile = %+v", tile)
	}
	if len(page.Categories) != 1 || page.Categories[0].ProductCount != 1 {
		t.Fatalf("catégories = %+v", page.Categories)
	}

	// The revalidation.
	second := b.do(http.MethodGet, "/api/v1/catalog", "", http.Header{"If-None-Match": {etag}})
	defer second.Body.Close()
	if second.StatusCode != http.StatusNotModified {
		t.Fatalf("revalidation = %d, attendu 304", second.StatusCode)
	}
}

// TestTheSearchNameIsDesaccentuatedByTheServer is §14.3's « divergence de
// normalisation, fermée par la machine ».
//
// The server sends the normalized name, computed at the moment of serving; the browser
// normalizes only the QUERY. Without it, the 127 products of the real file whose name
// starts with a heart are unreachable from the reduced keyboard.
func TestTheSearchNameIsDesaccentuatedByTheServer(t *testing.T) {
	b := newBench(t, func(o *benchOptions) {
		o.catalog = domain.NewCatalog([]domain.Product{
			{
				ID: "1", Name: "♥ ÉPINARDS", Reference: "0493021000003",
				Mode: domain.ByWeight, UnitPrice: 640, CategoryCode: "vegetables",
				Qualification: domain.Weighable,
			},
			{
				ID: "2", Name: "Œufs bio", Reference: "0499021000009",
				Mode: domain.ByUnit, UnitPrice: 300, CategoryCode: "other",
				Qualification: domain.Weighable,
			},
		}, []domain.Category{{Code: "vegetables", Label: "Légumes", Visible: true}})
	})

	page := decodeStatus[catalogDTO](t, b.get("/api/v1/catalog"), http.StatusOK)
	byID := make(map[string]catalogProductDTO, len(page.Products))
	for _, tile := range page.Products {
		byID[tile.ID] = tile
	}
	if got := byID["1"].Search; got != "epinards" {
		t.Fatalf("nom cherchable = %q, attendu « epinards » (le cœur et l'accent tombent)", got)
	}
	if got := byID["2"].Search; !strings.HasPrefix(got, "oeufs") {
		t.Fatalf("nom cherchable = %q : la ligature Œ doit se chercher par OE", got)
	}
}

// TestWhatHasNoTileIsNotServedToTheScreen: a prepackaged product is not an error, it
// is not the scale's business, and it has no tile (§10.3, ADR-021).
func TestWhatHasNoTileIsNotServedToTheScreen(t *testing.T) {
	b := newBench(t, func(o *benchOptions) {
		o.catalog = domain.NewCatalog([]domain.Product{
			{ID: "1", Name: "AIL", Mode: domain.ByWeight, UnitPrice: 532,
				CategoryCode: "vegetables", Qualification: domain.Weighable},
			{ID: "2", Name: "BOULGOUR 500 G", Mode: domain.ByWeight, UnitPrice: 250,
				CategoryCode: "other", Qualification: domain.NotWeighable,
				Reason: "PREPACKAGED_PRODUCT"},
			{ID: "3", Name: "LIGNE FAUTIVE", Mode: domain.ByWeight,
				CategoryCode: "other", Qualification: domain.Anomaly},
		}, nil)
	})

	page := decodeStatus[catalogDTO](t, b.get("/api/v1/catalog"), http.StatusOK)
	if page.ProductCount != 1 {
		t.Fatalf("%d tuiles servies, attendu 1 : seuls les pesables ont une tuile", page.ProductCount)
	}
}

// TestTheCatalogCarriesTheAddressOfEachPhoto, and only when the photo really exists —
// 174 of the 355 real products have none, which is not a degraded case.
func TestTheCatalogCarriesTheAddressOfEachPhoto(t *testing.T) {
	sha := strings.Repeat("cd", 32)
	b := newBench(t, func(o *benchOptions) {
		o.catalog = domain.NewCatalog([]domain.Product{
			{ID: "1", Name: "AVEC PHOTO", Mode: domain.ByWeight, UnitPrice: 100,
				CategoryCode: "vegetables", Qualification: domain.Weighable, ImageSHA: sha},
			{ID: "2", Name: "SANS PHOTO", Mode: domain.ByWeight, UnitPrice: 100,
				CategoryCode: "vegetables", Qualification: domain.Weighable},
		}, nil)
	})
	b.store.images[sha] = domain.Image{SHA256: sha, Format: domain.ImagePNG}

	page := decodeStatus[catalogDTO](t, b.get("/api/v1/catalog"), http.StatusOK)
	for _, tile := range page.Products {
		switch tile.ID {
		case "1":
			// The extension comes from the DETECTED format, never from a stored name.
			if tile.ImageURL != "/images/"+sha+".png" {
				t.Fatalf("adresse de la photo = %q", tile.ImageURL)
			}
		case "2":
			if tile.ImageURL != "" {
				t.Fatalf("un produit sans photo porte une adresse : %q", tile.ImageURL)
			}
		}
	}
}

// TestTheCatalogPayloadIsBuiltOncePerCatalog: §4 promises no disk access on the
// weighing path, and building the payload asks the store for the format of every photo.
func TestTheCatalogPayloadIsBuiltOncePerCatalog(t *testing.T) {
	b := newBench(t)
	first := b.server.catalogBytes(t.Context(), b.hub.Catalog(), b.hub.Config())
	second := b.server.catalogBytes(t.Context(), b.hub.Catalog(), b.hub.Config())
	if first != second {
		t.Fatal("le catalogue a été re-sérialisé alors qu'il n'a pas bougé")
	}
}

// TestAnEmptyCatalogIsServedAsAnEmptyGrid, not as an error: a station waiting for its
// first file must show « Catalogue vide », which needs a document to show it with.
func TestAnEmptyCatalogIsServedAsAnEmptyGrid(t *testing.T) {
	b := newBench(t, func(o *benchOptions) { o.catalog = nil })
	page := decodeStatus[catalogDTO](t, b.get("/api/v1/catalog"), http.StatusOK)
	if page.ProductCount != 0 || page.Products == nil {
		t.Fatalf("catalogue vide = %+v, attendu une liste vide et non un nul", page)
	}
}
