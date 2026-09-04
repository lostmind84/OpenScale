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
	UpdatedAt string `json:"updated_at"`
	// AppVersion is what the client screen states permanently, beside the number of
	// weighable products: « 331 produits pesables · application 2.4.0 » (§14.3). It
	// travels with the catalog because it changes once per deployment and this
	// payload is already cached behind an ETag — and because the other place that
	// knows it, /admin/api/health, must not be reachable from the grid (§14.1).
	AppVersion   string                 `json:"app_version"`
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
	// Prices is one derived unit price per configured tier (§14.2, dual
	// pricing) — the front picks primary vs secondary from pricing.primary_code
	// and pricing.tiers, this only carries the numbers.
	Prices []catalogTilePriceDTO `json:"prices"`
}

// catalogTilePriceDTO is one configured tier's derived price for one product —
// the arithmetic of domain.Price, run without a weight, so the grid can show
// what a customer will actually pay before they even pick anything up.
type catalogTilePriceDTO struct {
	Code string `json:"code"`
	Text string `json:"text"`
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
//
// It is ALSO what presentationDigest is taken over, which is what gives this struct its
// second job: what is in it reaches the client screen when it changes, what is not in it
// never forces a reload.
type catalogPresentationDTO struct {
	ShowGridPrices       bool `json:"show_grid_prices"`
	IdleTimeoutSeconds   int  `json:"idle_timeout_s"`
	ReprintWindowSeconds int  `json:"reprint_window_s"`
	Sound                bool `json:"sound"`
	// ShowByUnitProducts is STATED here and applied by the grid, never applied here.
	// Filtering the payload would make product_count differ from the catalog_count of
	// the state stream for ever, and the browser asks for the catalog again on that
	// difference alone -- ten times a second, answering 304, visible to nobody.
	ShowByUnitProducts bool `json:"show_by_unit_products"`
	// GridColumns is domain.GridColumnsAutomatic -- the zero, which means AUTOMATIC and
	// never « aucune colonne » -- or a count between domain.MinGridColumns and
	// domain.MaxGridColumns, which means THAT MANY COLUMNS ON ANY SCREEN (ADR-057). It
	// is stated here and applied by the grid, like the two flags above.
	//
	// The zero travels rather than being omitted: « automatique » is a VALUE of this
	// setting and not its absence, and a front end reading `undefined` would have to
	// invent which of the two it meant.
	GridColumns int `json:"grid_columns"`
	// MinProductsForChip is how many tiles a category needs before the grid gives it a
	// filter chip (ADR-059). Stated here and applied by the grid, like the settings
	// above: which categories end up with a chip depends on what the grid actually shows,
	// and the grid is the only side that knows it.
	MinProductsForChip int `json:"min_products_for_chip"`
}

// presentationOf carries the screen settings of one configuration.
//
// It is the ONE place this payload is built, and presentationDigest hashes what it
// returns: that is the whole mechanism by which a setting added here tomorrow reaches
// the client screen without anybody remembering to widen a digest.
func presentationOf(ui domain.UIConfig) catalogPresentationDTO {
	return catalogPresentationDTO{
		ShowGridPrices:       ui.ShowGridPrices,
		IdleTimeoutSeconds:   ui.IdleTimeoutSeconds,
		ReprintWindowSeconds: ui.ReprintWindowSeconds,
		Sound:                ui.Sound,
		ShowByUnitProducts:   ui.ShowByUnitProducts,
		GridColumns:          ui.GridColumns,
		MinProductsForChip:   ui.MinProductsForChip,
	}
}

// presentationDigest is the string the state stream carries so that a setting saved on
// the administration screen reaches the client screen next door.
//
// # Why it exists at all
//
// The browser asks for the catalog again when catalog_count moves and, since
// 04/09/2026, when catalog_updated_at moves (web/src/lib/session.svelte.ts). A
// presentation that changes without an import therefore never arrives on the grid --
// which is already true of show_grid_prices, and with a grid setting would become « on
// règle, on enregistre, et rien ne se passe sur l'écran d'à côté ». The browser only ever
// COMPARES this string to the previous one; it is opaque to it, and the ETag of the
// catalog makes an unchanged presentation cost a 304.
//
// # What it is taken over, because two readings are possible
//
// Over the PRESENTATION DTO, and never over the configuration as a whole: a global
// fingerprint would reload the whole grid on a change of serial port or of print
// darkness -- a full reload for a value the client screen does not read. Hashing the
// struct rather than a hand-written list of its fields is what keeps that true in BOTH
// directions: a field added to the presentation enters the digest by itself, and a block
// that is not in the presentation never can.
func presentationDigest(p catalogPresentationDTO) string {
	return domain.BlockFingerprint(p)
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
		Options:    presentationOf(cfg.UI),
		UpdatedAt:  rfc3339OrEmpty(s.hub.CatalogUpdatedAt()),
		AppVersion: s.version,
	}
	for _, p := range products {
		if p.Qualification != domain.Weighable {
			continue
		}
		counts[p.CategoryCode]++
		prices := make([]catalogTilePriceDTO, 0, len(cfg.Pricing.Tiers))
		for _, tier := range cfg.Pricing.SortedTiers() {
			unit := domain.UnitPriceFor(p.UnitPrice, tier, cfg.Pricing.UnitPriceRounding)
			prices = append(prices, catalogTilePriceDTO{Code: tier.Code, Text: unit.Euro()})
		}
		out.Products = append(out.Products, catalogProductDTO{
			ID: p.ID, Name: p.Name, Search: domain.Normalize(p.Name),
			CategoryCode:   p.CategoryCode,
			Mode:           p.Mode.String(),
			UnitPriceCents: int64(p.UnitPrice),
			UnitPriceText:  p.UnitPrice.Euro(),
			PriceSuffix:    p.PriceSuffix,
			ImageURL:       s.imageURLFor(ctx, p.ImageSHA),
			Prices:         prices,
		})
	}
	out.ProductCount = len(out.Products)

	// From the CONFIGURATION, and not from the snapshot. The exchange file carries a
	// letter; the label, the rank, the colour and « montrer cette catégorie sur ce
	// poste » are shop decisions that live in config.json and reach THIS endpoint
	// immediately (§10.2 bis, §11.4) — no restart, no waiting for the next import.
	// The snapshot carries them too — the store hands back the `categories` table,
	// which exists as the parent of the foreign key products.category_code and which
	// only an IMPORT ever upserts. Reading the wording of the grid from there tied a
	// configuration change to the next catalog a producer happens to publish: a shelf
	// renamed on Monday kept its old name until the Friday export, with nothing on any
	// screen to say why. The effectif below still comes from the catalog in service,
	// because that is the one number a customer can check against the tiles.
	//
	// What is still open: this endpoint answers with the new wording the moment it is
	// asked, but nothing tells a browser already holding a loaded grid to ask again —
	// catalog_count, catalog_updated_at and presentation_digest are what trigger that
	// (web/src/lib/session.svelte.ts), and a rename moves none of the three. A kiosk
	// left running keeps the old label until its next catalog load.
	for _, c := range cfg.Catalog.Categories {
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
