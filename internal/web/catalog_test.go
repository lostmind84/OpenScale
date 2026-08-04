package web

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

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
	byCode := map[string]string{}
	for _, price := range tile.Prices {
		byCode[price.Code] = price.Text
	}
	if len(tile.Prices) != 2 || byCode["MEMBER"] != "4,79" || byCode["SOLIDARITY"] != "5,32" {
		t.Fatalf("tarifs de la tuile = %+v, attendu MEMBER=4,79 SOLIDARITY=5,32", tile.Prices)
	}
	// The FOUR shelves the configuration declares, and not the one the snapshot of this
	// bench happens to carry: the shelves belong to config.json (§10.2 bis). A shelf with
	// no tile costs the screen nothing — the grid gives a chip only from
	// MIN_PRODUCTS_FOR_CHIP products up — and the payload stays the inventory of what
	// this station is configured to show. What is asserted here is the effectif, which is
	// counted on the catalog in service.
	byCategory := map[string]int{}
	for _, shelf := range page.Categories {
		byCategory[shelf.Code] = shelf.ProductCount
	}
	if len(page.Categories) != 4 || byCategory["vegetables"] != 1 {
		t.Fatalf("catégories = %+v", page.Categories)
	}

	// The revalidation.
	second := b.do(http.MethodGet, "/api/v1/catalog", "", http.Header{"If-None-Match": {etag}})
	defer second.Body.Close()
	if second.StatusCode != http.StatusNotModified {
		t.Fatalf("revalidation = %d, attendu 304", second.StatusCode)
	}
}

// TestTheCatalogCarriesTheApplicationVersion: the client screen states, permanently,
// which version is running — « 331 produits pesables · application 2.4.0 » (§14.3).
//
// It travels with the CATALOG and not with the state stream, because it changes once
// per deployment and the catalog is already cached behind an ETag. The other place that
// knows it, /admin/api/health, is deliberately out of reach: `npm run budget` asserts
// that not one byte of the administration is loaded to draw the grid.
func TestTheCatalogCarriesTheApplicationVersion(t *testing.T) {
	b := newBench(t)

	page := decodeStatus[catalogDTO](t, b.get("/api/v1/catalog"), http.StatusOK)

	if page.AppVersion != "test" {
		t.Fatalf("app_version = %q, attendu « test » — la version que ce banc injecte", page.AppVersion)
	}
}

// TestTheCatalogSaysWhenItEnteredService: « ces prix datent de quand ? » is the one
// question a volunteer asks in front of a grid, and §14.3 now answers it permanently.
//
// The instant is the one of the IMPORT that produced the catalog, read back from the
// imports table, and never a date read in a file nor a clock read at start-up: a station
// that received nothing for three days says so by not moving.
func TestTheCatalogSaysWhenItEnteredService(t *testing.T) {
	b := newBench(t)

	page := decodeStatus[catalogDTO](t, b.get("/api/v1/catalog"), http.StatusOK)

	swap := b.hub.CatalogUpdatedAt()
	if swap.IsZero() {
		t.Fatal("le Hub ne date pas le catalogue qu'il sert")
	}
	if page.UpdatedAt != swap.Format(time.RFC3339) {
		t.Fatalf("updated_at = %q, attendu %q", page.UpdatedAt, swap.Format(time.RFC3339))
	}
}

// TestAStationWithoutACatalogHasNoDateToShow: the zero instant is served EMPTY.
//
// « 0001-01-01 » on the screen of a station whose first file has not arrived would be
// a date, and a volunteer would go looking for the import that produced it.
func TestAStationWithoutACatalogHasNoDateToShow(t *testing.T) {
	if got := rfc3339OrEmpty(time.Time{}); got != "" {
		t.Fatalf("instant zéro servi %q, attendu la chaîne vide", got)
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

// TestTheGridSettingAboutByUnitProductsTravelsWithTheCatalog, in both directions.
//
// The flag is a SCREEN setting and rides with the other three, exactly as
// show_grid_prices does: the station takes no decision here, it states one so that the
// grid can apply it.
func TestTheGridSettingAboutByUnitProductsTravelsWithTheCatalog(t *testing.T) {
	for name, shown := range map[string]bool{"masqués": false, "montrés": true} {
		t.Run(name, func(t *testing.T) {
			b := newBench(t, func(o *benchOptions) {
				o.config = func(c *domain.Config) { c.UI.ShowByUnitProducts = shown }
			})

			page := decodeStatus[catalogDTO](t, b.get("/api/v1/catalog"), http.StatusOK)

			if page.Options.ShowByUnitProducts != shown {
				t.Fatalf("presentation.show_by_unit_products = %v, attendu %v",
					page.Options.ShowByUnitProducts, shown)
			}
		})
	}
}

// TestTheNumberOfGridColumnsTravelsWithTheCatalog, at both ends of its range and at
// « automatique ».
//
// It rides with the other screen settings, and for the same reason: the station takes no
// decision here, it states one so that the grid can apply it. What « 7 » means — seven
// columns on ANY screen — is the grid's business, not this payload's.
func TestTheNumberOfGridColumnsTravelsWithTheCatalog(t *testing.T) {
	for name, columns := range map[string]int{
		"automatique": domain.GridColumnsAutomatic,
		"plancher":    domain.MinGridColumns,
		"plafond":     domain.MaxGridColumns,
	} {
		t.Run(name, func(t *testing.T) {
			b := newBench(t, func(o *benchOptions) {
				o.config = func(c *domain.Config) { c.UI.GridColumns = columns }
			})

			page := decodeStatus[catalogDTO](t, b.get("/api/v1/catalog"), http.StatusOK)

			if page.Options.GridColumns != columns {
				t.Fatalf("presentation.grid_columns = %d, attendu %d",
					page.Options.GridColumns, columns)
			}
		})
	}
}

// TestAutomaticIsServedAsTheZeroItIsAndNeverByOmission.
//
// « Automatique » is a VALUE of this setting and not its absence: served by omission, a
// front end would read `undefined` and have to invent which of the two it meant — and
// the assertion has to be on the RAW bytes, because `omitempty` and a real zero both
// decode to 0 in Go.
func TestAutomaticIsServedAsTheZeroItIsAndNeverByOmission(t *testing.T) {
	b := newBench(t, func(o *benchOptions) {
		o.config = func(c *domain.Config) { c.UI.GridColumns = domain.GridColumnsAutomatic }
	})

	if raw := body(t, b.get("/api/v1/catalog")); !strings.Contains(raw, `"grid_columns":0`) {
		t.Fatalf("« automatique » n'est pas servi comme le zéro qu'il est :\n%s", raw)
	}
}

// TestThePresentationDigestReachesTheClientScreen is the reason this digest exists.
//
// The browser asks for the catalog again only when `catalog_count` moves
// (web/src/lib/session.svelte.ts). A presentation that changes without changing the
// count therefore never arrives — which is already true of show_grid_prices, and with a
// grid setting would become « on règle, on enregistre, et rien ne se passe sur l'écran
// d'à côté ». The state stream carries a string the browser only ever COMPARES to the
// previous one, and that string has to move when the setting does.
func TestThePresentationDigestReachesTheClientScreen(t *testing.T) {
	automatic := newBench(t)
	before := automatic.state().PresentationDigest
	if before == "" {
		t.Fatal("le flux d'état ne porte aucune empreinte de présentation")
	}

	seven := newBench(t, func(o *benchOptions) {
		o.config = func(c *domain.Config) { c.UI.GridColumns = 7 }
	})
	if after := seven.state().PresentationDigest; after == before {
		t.Fatalf("empreinte inchangée (%q) alors que la grille est passée à 7 colonnes : "+
			"le navigateur ne redemanderait jamais le catalogue", after)
	}
}

// TestThePresentationDigestFollowsThePresentationAndNothingElse is the decision of §3,
// checked rather than reviewed.
//
// The digest is taken over the PRESENTATION and never over the configuration as a whole:
// a global fingerprint would reload the whole grid on a change of serial port or of print
// darkness — a full reload for a value the client screen does not read.
func TestThePresentationDigestFollowsThePresentationAndNothingElse(t *testing.T) {
	reference := loadConfig(t)
	base := presentationDigest(presentationOf(reference.UI))

	for name, tweak := range map[string]func(*domain.Config){
		"le nombre de colonnes": func(c *domain.Config) { c.UI.GridColumns = 7 },
		"le seuil de puce":      func(c *domain.Config) { c.UI.MinProductsForChip = 12 },
		"les prix sur les tuiles": func(c *domain.Config) {
			c.UI.ShowGridPrices = !c.UI.ShowGridPrices
		},
		"les tuiles à l'unité": func(c *domain.Config) {
			c.UI.ShowByUnitProducts = !c.UI.ShowByUnitProducts
		},
	} {
		t.Run("bouge quand "+name+" bouge", func(t *testing.T) {
			if got := digestOf(t, tweak); got == base {
				t.Fatalf("empreinte inchangée (%q) alors que %s a bougé", got, name)
			}
		})
	}

	for name, tweak := range map[string]func(*domain.Config){
		"la noirceur d'impression": func(c *domain.Config) {
			c.Printer.Options["darkness"] = json.RawMessage("4")
		},
		"le nom du poste":         func(c *domain.Config) { c.Station.Name = "Poste 9 — bocaux" },
		"la patience du port":     func(c *domain.Config) { c.Scale.DegradeAfterSeconds = 30 },
		"la rétention du journal": func(c *domain.Config) { c.Journal.MaxDays = 400 },
	} {
		t.Run("ne bouge pas quand "+name+" bouge", func(t *testing.T) {
			if got := digestOf(t, tweak); got != base {
				t.Fatalf("empreinte %q au lieu de %q : %s a fait recharger toute la grille "+
					"pour une donnée que l'écran client ne lit pas", got, base, name)
			}
		})
	}
}

// digestOf renders the presentation digest of the shipped configuration, tweaked.
func digestOf(t *testing.T, tweak func(*domain.Config)) string {
	t.Helper()
	cfg := loadConfig(t)
	tweak(&cfg)
	return presentationDigest(presentationOf(cfg.UI))
}

// TestEveryFieldOfThePresentationEntersItsDigest is what makes « un champ ajouté demain
// entre dans l'empreinte sans que personne y pense » a property and not an intention.
//
// The digest is a hash of the WHOLE struct, so this test walks the struct rather than a
// list of names: the day a seventh setting joins the presentation, it is covered without
// anybody remembering this test exists. A hand-written concatenation of five fields would
// pass every other test in this file and fail this one.
func TestEveryFieldOfThePresentationEntersItsDigest(t *testing.T) {
	reference := presentationOf(loadConfig(t).UI)
	base := presentationDigest(reference)

	typ := reflect.TypeOf(reference)
	for i := 0; i < typ.NumField(); i++ {
		moved := reference
		field := reflect.ValueOf(&moved).Elem().Field(i)
		switch field.Kind() {
		case reflect.Bool:
			field.SetBool(!field.Bool())
		case reflect.Int:
			field.SetInt(field.Int() + 1)
		case reflect.String:
			field.SetString(field.String() + "x")
		default:
			t.Fatalf("%s est de type %s, que ce test ne sait pas faire bouger : "+
				"une empreinte dont on ne sait pas prouver qu'elle suit un champ ne "+
				"protège pas ce champ", typ.Field(i).Name, field.Kind())
		}
		if got := presentationDigest(moved); got == base {
			t.Errorf("l'empreinte ne bouge pas quand %s bouge : elle n'est pas prise sur "+
				"le DTO entier, et le prochain champ ajouté n'y entrera pas non plus",
				typ.Field(i).Name)
		}
	}
}

// TestTheStationServesEveryWeighableTileWhateverTheGridShows is the guard against a
// reload ten times a second.
//
// The browser asks for the catalog again the moment `catalog_count` of the state stream
// differs from `product_count` of the payload. Filtering the by-unit products HERE would
// make those two numbers differ FOR EVER, and every SSE event — ten a second — would
// fire a GET /api/v1/catalog. The masking is a station's display choice and it belongs
// to the grid; the payload stays the inventory of what has a tile.
func TestTheStationServesEveryWeighableTileWhateverTheGridShows(t *testing.T) {
	byUnit := domain.NewCatalog([]domain.Product{
		{
			ID: "1", Name: "AIL", Reference: "0493021000003",
			Mode: domain.ByWeight, UnitPrice: 532, CategoryCode: "vegetables",
			Qualification: domain.Weighable,
		},
		{
			ID: "2", Name: "MELON unite SAF", Reference: "0499000064004",
			Mode: domain.ByUnit, UnitPrice: 300, CategoryCode: "fruits",
			Qualification: domain.Weighable,
		},
	}, []domain.Category{{Code: "vegetables", Label: "Légumes", Visible: true}})

	b := newBench(t, func(o *benchOptions) {
		o.catalog = byUnit
		o.config = func(c *domain.Config) { c.UI.ShowByUnitProducts = false }
	})

	page := decodeStatus[catalogDTO](t, b.get("/api/v1/catalog"), http.StatusOK)

	if page.ProductCount != 2 {
		t.Fatalf("%d tuiles servies, attendu 2 : le masquage se fait à l'écran, pas ici",
			page.ProductCount)
	}
	if got := b.state().CatalogCount; got != page.ProductCount {
		t.Fatalf("catalog_count du flux = %d, product_count du catalogue = %d : "+
			"le navigateur redemanderait le catalogue à chaque événement", got, page.ProductCount)
	}
}

// TestTheCategoryLabelsFollowTheConfigurationAndNotTheLastImport pins §10.2 bis: the
// exchange file carries a LETTER, and the configuration carries the label, the rank,
// the colour and « montrer cette catégorie sur ce poste ».
//
// Those four reload hot (§11.4), so a volunteer who renames a shelf sees it on the grid
// without waiting for a producer to publish a file. Reading them from the snapshot
// instead made them wait: `categories` is a table an IMPORT upserts — it exists as the
// parent of the foreign key products.category_code — so a rename applied on the next
// catalog and on nothing else, and a station whose producer publishes weekly showed the
// old wording for a week with no way to tell why.
//
// The snapshot below therefore carries what the last import wrote, and the
// configuration carries what somebody has just typed. What the screen gets is the
// second one.
func TestTheCategoryLabelsFollowTheConfigurationAndNotTheLastImport(t *testing.T) {
	imported := domain.NewCatalog([]domain.Product{
		{ID: "1", Name: "AIL", Reference: "0493021000003", Mode: domain.ByWeight,
			UnitPrice: 532, CategoryCode: "vegetables", Qualification: domain.Weighable},
	}, []domain.Category{
		{Code: "vegetables", Label: "Légumes", Rank: 2, Color: "#27AE60", Visible: true},
	})

	b := newBench(t, func(o *benchOptions) {
		o.catalog = imported
		o.config = func(c *domain.Config) {
			for i := range c.Catalog.Categories {
				if c.Catalog.Categories[i].Code != "vegetables" {
					continue
				}
				c.Catalog.Categories[i].Label = "Légumes du jardin"
				c.Catalog.Categories[i].Rank = 1
				c.Catalog.Categories[i].Color = "#1E8449"
			}
		}
	})

	page := decodeStatus[catalogDTO](t, b.get("/api/v1/catalog"), http.StatusOK)

	var shelf categoryDTO
	for _, c := range page.Categories {
		if c.Code == "vegetables" {
			shelf = c
		}
	}
	switch {
	case shelf.Code == "":
		t.Fatalf("la catégorie « vegetables » n'est pas servie : %+v", page.Categories)
	case shelf.Label != "Légumes du jardin":
		t.Errorf("libellé servi = %q : c'est celui du dernier import, pas celui du "+
			"fichier de configuration", shelf.Label)
	case shelf.Rank != 1:
		t.Errorf("rang servi = %d, attendu 1 : l'ordre de la barre vient de la "+
			"configuration", shelf.Rank)
	case shelf.Color != "#1E8449":
		t.Errorf("couleur servie = %q, attendu « #1E8449 »", shelf.Color)
	}
	if shelf.ProductCount != 1 {
		t.Errorf("%d tuile(s) comptée(s) pour la catégorie, attendu 1 : l'effectif se "+
			"compte toujours sur le catalogue en service", shelf.ProductCount)
	}
}

// TestTheChipThresholdTravelsWithTheCatalog: the grid decides which categories get a
// chip, and it decides it on a number this station sets (ADR-059).
//
// It rides in `presentation` with the other screen settings and for the same reason: the
// station states the setting, the grid applies it. Which categories end up with a chip is
// never computed here -- the payload stays the inventory of what this station shows.
func TestTheChipThresholdTravelsWithTheCatalog(t *testing.T) {
	b := newBench(t, func(o *benchOptions) {
		o.config = func(c *domain.Config) { c.UI.MinProductsForChip = 12 }
	})

	page := decodeStatus[catalogDTO](t, b.get("/api/v1/catalog"), http.StatusOK)

	if page.Options.MinProductsForChip != 12 {
		t.Fatalf("presentation.min_products_for_chip = %d, attendu 12",
			page.Options.MinProductsForChip)
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

// TestNoListOfThisPayloadIsEverNull.
//
// A nil slice marshals to `null`, and `null.filter(…)` is a TypeError. On a station whose
// catalog has not arrived — a station installed this morning, the case §14.3 has a sentence
// for — the categories were served as `null` and the client screen fell into the ERR-UI-01
// overlay with its automatic reload every five seconds. The defect was invisible for as long
// as the bundle could not mount at all; it showed up the minute a browser really ran it.
//
// The assertion is on the RAW bytes and not on the decoded structure, because
// `json.Unmarshal` is exactly what hides the difference: `null` and `[]` both decode to a
// nil slice in Go, and only a browser can tell them apart.
func TestNoListOfThisPayloadIsEverNull(t *testing.T) {
	// A catalog that EXISTS and is empty, which is what a station serves between its first
	// tick and its first import: `o.catalog = nil` takes another path (the constant payload).
	b := newBench(t, func(o *benchOptions) {
		o.catalog = domain.NewCatalog(nil, nil)
	})
	raw := body(t, b.get("/api/v1/catalog"))

	for _, list := range []string{"products", "categories", "tiers"} {
		if strings.Contains(raw, `"`+list+`":null`) {
			t.Fatalf("%q est servi à null : un écran qui filtre cette liste tombe en ERR-UI-01\n%s",
				list, raw)
		}
	}
}
