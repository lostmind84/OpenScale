package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"openscale/internal/domain"
)

// catalogDTO is the whole grid, served once and kept by the browser.
//
// Keeping it in the browser is what makes the search of §14.3 instantaneous AND
// what keeps the grid usable while the service restarts: the tiles are already
// there, only the weight stops moving.
type catalogDTO struct {
	// Revision is the ETag, carried in the body as well so that a front end can log
	// which catalog it is showing without reading its own response headers.
	Revision string `json:"revision"`
	// UpdatedAt is when this catalog entered service, RFC 3339, or empty when none
	// ever has. The client screen shows it PERMANENTLY: « ces prix datent de
	// quand ? » is the one question a volunteer asks in front of a grid, and a date
	// that stops moving is how a station says it received nothing (§14.3).
	UpdatedAt    string                 `json:"updated_at"`
	ProductCount int                    `json:"product_count"`
	Categories   []categoryDTO          `json:"categories"`
	Products     []catalogProductDTO    `json:"products"`
	Fallback     string                 `json:"fallback_category"`
	Prices       catalogPricingDTO      `json:"pricing"`
	Options      catalogPresentationDTO `json:"presentation"`
}

// categoryDTO is one shelf of the grid, as configured for THIS station.
//
// ProductCount counts the WEIGHABLE products, never the rows received: the received
// count includes prepackaged goods, which have no tile, and a chip that promised
// 140 tiles and drew 126 would be a lie the volunteer cannot check (§14.4).
type categoryDTO struct {
	Code         string `json:"code"`
	Label        string `json:"label"`
	Rank         int    `json:"rank"`
	Color        string `json:"color"`
	Visible      bool   `json:"visible"`
	ProductCount int    `json:"product_count"`
}

// catalogProductDTO is one tile.
//
// Search is the DESACCENTUATED name, computed by domain.Normalize AT THE MOMENT OF
// SERVING and never stored: one source of truth for the name, and a browser that
// only normalizes the QUERY (§14.3). Without it, « ♥ LENTILLES VERTES » is
// unreachable from a reduced keyboard, and that is 127 products out of 355.
type catalogProductDTO struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Search         string `json:"search"`
	CategoryCode   string `json:"category_code"`
	Mode           string `json:"mode"`
	UnitPriceCents int64  `json:"unit_price_cents"`
	UnitPriceText  string `json:"unit_price_text"`
	PriceSuffix    string `json:"price_suffix"`
	// ImageURL is empty for 174 of the 355 real products, and that is not a degraded
	// case: it is one product in two. The tile is sized on the NAME (§14.2).
	ImageURL string `json:"image_url"`
}

// catalogPricingDTO is what the grid needs to show a price on a tile.
type catalogPricingDTO struct {
	PrimaryCode  string    `json:"primary_code"`
	PrimaryLabel string    `json:"primary_label"`
	Tiers        []tierDTO `json:"tiers"`
}

// tierDTO is one configured price level.
type tierDTO struct {
	Code   string `json:"code"`
	Label  string `json:"label"`
	Abbrev string `json:"abbrev"`
	Rank   int    `json:"rank"`
}

// catalogPresentationDTO carries the screen settings the grid depends on.
type catalogPresentationDTO struct {
	ShowGridPrices       bool `json:"show_grid_prices"`
	IdleTimeoutSeconds   int  `json:"idle_timeout_s"`
	ReprintWindowSeconds int  `json:"reprint_window_s"`
	Sound                bool `json:"sound"`
	// TileSize is `small`, `medium` or `large` — the density of the grid (ADR-031).
	TileSize string `json:"tile_size"`
}

// rfc3339OrEmpty formats an instant, and the zero time as an empty string.
//
// Empty and not « 0001-01-01 »: a station whose first catalog has not arrived has
// no date to show, and a screen must be able to tell that from a real one.
func rfc3339OrEmpty(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.Format(time.RFC3339)
}

// catalogPayload is one serialized catalog, kept until the catalog itself changes.
type catalogPayload struct {
	// source is the snapshot these bytes were built from. A catalog is IMMUTABLE and
	// only ever replaced wholesale, so a pointer that has not moved describes bytes
	// that cannot have moved either.
	source *domain.Catalog
	// fingerprint is the configuration fingerprint the payload was built under: the
	// categories, the tiers and the three screen settings come from the configuration,
	// which reloads hot (§11.4).
	fingerprint string
	etag        string
	body        []byte
}

// catalogPage is GET /api/v1/catalog: the whole grid, with an ETag.
//
// # There is no page size, and that is the point
//
// The legacy application had 120 tile slots per category and 126 « Autres » products
// on the real file: six sellable products were displayed on no station at all, with
// no message and no log line. A list has no such limit, and this route is where that
// stops being an anecdote.
func (s *Server) catalogPage(w http.ResponseWriter, r *http.Request) {
	catalog := s.hub.Catalog()
	cfg := s.hub.Config()
	payload := s.catalogBytes(r.Context(), catalog, cfg)

	w.Header().Set("ETag", payload.etag)
	// The catalog changes at an import and at nothing else, but a station that has
	// just restarted must not serve a stale grid from a browser cache: the validator
	// is revalidated on every load and answers 304 in a few hundred microseconds.
	w.Header().Set("Cache-Control", "no-cache")
	if match := r.Header.Get("If-None-Match"); match != "" && match == payload.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload.body)
}

// catalogBytes returns the serialized catalog, building it only when it moved.
//
// Building it walks 355 products and asks the store for the format of 181 images:
// doing that on every page load of a browser that reconnects would put a database
// read on a path §4 promises is free of them.
func (s *Server) catalogBytes(ctx context.Context, catalog *domain.Catalog, cfg domain.Config) *catalogPayload {
	fingerprint := cfg.Fingerprint()
	if cached := s.catalogPayload.Load(); cached != nil &&
		cached.source == catalog && cached.fingerprint == fingerprint {
		return cached
	}

	body := s.catalogOf(ctx, catalog, cfg)

	// Serialized TWICE on purpose: the digest is taken over the document with an
	// empty revision, and the revision is then filled with that digest. Hashing the
	// document that carries its own hash is not possible, and re-deriving the
	// validator from a map would reorder every key of the payload.
	raw, err := json.Marshal(body)
	if err != nil {
		// A catalog that cannot be serialized is a bug in this file and not a station
		// failure: an empty document keeps the screen alive, and the technical journal
		// says what happened.
		s.technical.Technical(domain.LevelError, "http", "",
			"Catalogue non sérialisable.", err.Error())
		body, raw = catalogDTO{}, nil
	}
	sum := sha256.Sum256(raw)
	body.Revision = `"` + hex.EncodeToString(sum[:8]) + `"`
	final, err := json.Marshal(body)
	if err != nil {
		final = []byte(`{"revision":"","product_count":0,"products":[],"categories":[]}`)
	}

	payload := &catalogPayload{
		source: catalog, fingerprint: fingerprint,
		etag: body.Revision, body: final,
	}
	s.catalogPayload.Store(payload)
	return payload
}

// catalogOf converts the immutable snapshot into the grid.
//
// It serves the WEIGHABLE products only. The others — prepackaged goods, a code
// belonging to another shop — have no tile by construction (§10.3, ADR-021), and
// sending them to a screen would be asking a front end to re-derive a decision the
// import already took. The inventory of what was received lives on the
// administration dashboard, where a volunteer can act on it.
func (s *Server) catalogOf(ctx context.Context, catalog *domain.Catalog, cfg domain.Config) catalogDTO {
	products := catalog.Products()
	counts := make(map[string]int, len(cfg.Catalog.Categories))

	out := catalogDTO{
		Products: make([]catalogProductDTO, 0, len(products)),
		// Allocated like Products, and for the same reason: a nil slice marshals to `null`,
		// and `null.filter(…)` is a TypeError that lands on the client screen as ERR-UI-01
		// with an automatic reload every five seconds. It happens on a station whose catalog
		// has not arrived yet — the very case §14.3 has a sentence for.
		Categories: make([]categoryDTO, 0, len(cfg.Catalog.Categories)),
		Fallback:   cfg.Catalog.FallbackCategory,
		Options: catalogPresentationDTO{
			ShowGridPrices:       cfg.UI.ShowGridPrices,
			IdleTimeoutSeconds:   cfg.UI.IdleTimeoutSeconds,
			ReprintWindowSeconds: cfg.UI.ReprintWindowSeconds,
			Sound:                cfg.UI.Sound,
			TileSize:             cfg.UI.TileSize,
		},
		UpdatedAt: rfc3339OrEmpty(s.hub.CatalogUpdatedAt()),
	}
	for _, p := range products {
		if p.Qualification != domain.Weighable {
			continue
		}
		counts[p.CategoryCode]++
		out.Products = append(out.Products, catalogProductDTO{
			ID: p.ID, Name: p.Name, Search: domain.Normalize(p.Name),
			CategoryCode:   p.CategoryCode,
			Mode:           p.Mode.String(),
			UnitPriceCents: int64(p.UnitPrice),
			UnitPriceText:  p.UnitPrice.Euro(),
			PriceSuffix:    p.PriceSuffix,
			ImageURL:       s.imageURLFor(ctx, p.ImageSHA),
		})
	}
	out.ProductCount = len(out.Products)

	for _, c := range catalog.Categories() {
		out.Categories = append(out.Categories, categoryDTO{
			Code: c.Code, Label: c.Label, Rank: c.Rank, Color: c.Color,
			Visible: c.Visible, ProductCount: counts[c.Code],
		})
	}
	out.Prices = pricingOf(cfg.Pricing)
	return out
}

// pricingOf carries the grid of tiers, so that a tile can show the price a customer
// will actually pay without the front end re-deriving a coefficient.
func pricingOf(rules domain.PricingRules) catalogPricingDTO {
	out := catalogPricingDTO{PrimaryCode: rules.PrimaryCode}
	for _, t := range rules.SortedTiers() {
		out.Tiers = append(out.Tiers, tierDTO{
			Code: t.Code, Label: t.Label, Abbrev: t.Abbrev, Rank: t.Rank,
		})
		if t.Code == rules.PrimaryCode {
			out.PrimaryLabel = t.Label
		}
	}
	return out
}
