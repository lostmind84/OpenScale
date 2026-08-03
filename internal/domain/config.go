package domain

import "time"

// This file owns the SCHEMA of config.json: the fourteen blocks a station declares,
// and the closed lists of values they name.
//
// One JSON file, which is also the export format (§11.1): encoding/json serialises
// the very structure the administration screen edits, so "clone a station" is a
// file copy. It is deliberately NOT in the database -- a volunteer must be able to
// copy it onto a USB stick, and the process must start and show an administration
// screen even when the database is corrupt.
//
// Nothing here reads a clock, opens a file or a socket: the 48 controls that judge
// these types are pure (validate.go), and the two questions a pure function cannot
// answer -- "does this path exist?", "is this print queue really enumerated?" --
// arrive through Registries (options.go).

// The three printer drivers, all selectable through printer.type (§8.1).
//
// They are a CLOSED list rather than a registry lookup for the two values the
// binary itself has to name: NeutralProfile ships PrinterPreview, and the shipped
// configuration file ships PrinterRaster. The registry still decides what a
// volunteer may choose from, which is what control 4 checks.
const (
	// PrinterRaster is the DEFAULT and the production path (A2, ADR-002): the
	// bitmap is encapsulated in <A>…<G>…<Z> and handed to the print system of the
	// host.
	PrinterRaster = "raster"
	// PrinterSBPL produces the same frame and writes it straight to the device,
	// bypassing the spooler. It is the immediate fallback of unknown n° 4.
	PrinterSBPL = "sbpl"
	// PrinterPreview writes a life-size PDF or a PNG. Acceptance, offset tuning,
	// remote support -- and the printer of the neutral profile.
	PrinterPreview = "preview"
)

// The four byte transports (§8.4). Local by default, because decision 4 forbids
// any network dependency for weighing and the real installation is one Windows
// queue per station.
const (
	TransportWinspool = "winspool"
	TransportDevfile  = "devfile"
	TransportTCP      = "tcp"
	TransportFile     = "file"
)

// The three printer.options keys that DESIGNATE A DEVICE (§8.4), spelled once.
//
// Each belongs to exactly ONE of the four transports above, and none of them is required:
// a station on `winspool` leaves address and path empty, and that emptiness is nominal.
// Which one has to be filled is the business of the transport that was named, and each of
// the four says so in French when it is built.
//
// They live here, next to the transports themselves, because THREE layers read the same
// three words and a second spelling is how they stop meaning the same thing: the driver
// schema declares them, the transport registry names the one it reads, and the platform
// says which one an enumerated destination goes into. The administration screen used to
// know only `queue`, and wrote an IP address into it on a station set to `tcp`.
const (
	DeviceKeyQueue   = "queue"
	DeviceKeyPath    = "path"
	DeviceKeyAddress = "address"
)

// Where the images of the catalog come from (§10.7).
const (
	// ImageSourceCSV is the DEFAULT: the reference file carries 181 images out of
	// 355, and the format is recognised from the header bytes, never from the
	// extension.
	ImageSourceCSV = "csv"
	// ImageSourceDirectory reads them from catalog.images.path.
	ImageSourceDirectory = "image_directory"
	// ImageSourceNone ignores them; every tile falls back on its name.
	ImageSourceNone = "none"
)

// Config is the whole configuration of a station, and it is the file on disk.
type Config struct {
	// Version is the schema version of the FILE, not the version of the binary, and
	// domain.Migrate is what reads it (configmigration.go). It was written and never read
	// until 01/08/2026, which is why the migration steps are driven by the keys a document
	// carries rather than by this number.
	Version int `json:"version"`
	// Readme is the mode d'emploi that JSON cannot carry as a comment. It describes
	// the FILE and not the station, so it stays out of the fingerprint.
	Readme string `json:"_readme"`
	// ModifiedAt is stamped by whoever writes the file, from the INJECTED clock.
	// Nothing in internal/domain reads a wall clock, so nothing here fills it.
	ModifiedAt time.Time `json:"modified_at"`

	Station     StationConfig     `json:"station"`
	Network     NetworkConfig     `json:"network"`
	UI          UIConfig          `json:"ui"`
	Scale       ScaleConfig       `json:"scale"`
	Printer     PrinterConfig     `json:"printer"`
	Pricing     PricingRules      `json:"pricing"`
	Barcode     BarcodeConfig     `json:"barcode"`
	Limits      WeighingLimits    `json:"limits"`
	Stability   StabilityPolicy   `json:"stability"`
	Catalog     CatalogConfig     `json:"catalog"`
	Journal     JournalConfig     `json:"journal"`
	Admin       AdminConfig       `json:"admin"`
	Maintenance MaintenanceConfig `json:"maintenance"`
	Update      UpdateConfig      `json:"update"`

	// retired holds the dotted paths of the keys control 20 refuses, exactly as they
	// were found in the file. It is unexported and filled by UnmarshalJSON: a Config
	// built in Go carries none, by construction.
	retired []string
}

// StationConfig identifies this station inside the cooperative.
type StationConfig struct {
	// Number is what the watched file name derives from, flv_<n>.csv, and its only
	// real consumer (§11.2). It is excluded from a hardware-free export.
	Number int `json:"number"`
	// Name is the wording a volunteer reads, "Poste 2 — fruits". French content.
	Name string `json:"name"`
	// Coop is where the name of the cooperative lives -- and the reason ui.title is
	// gone: "La Cagette" was not decoration, it was the string passed to FindWindowA
	// to lock the Access kiosk down. It is shown on the administration dashboard.
	Coop string `json:"coop"`
}

// NetworkConfig is the listening surface of the station.
type NetworkConfig struct {
	// Listen is a host:port. A net.Listener closes and reopens in three lines, so
	// changing it never demands a process restart (ADR-027): it goes through the
	// same three-step window as the hardware, with automatic rollback.
	Listen string `json:"listen"`
	// AdminOnLAN opens the administration screen beyond the loopback.
	AdminOnLAN bool `json:"admin_on_lan"`
}

// What ui.grid_columns is allowed to say, spelled once (ADR-057).
//
// The two bounds are GUARD RAILS and not a calculation, and saying so is part of the
// decision: the same count is comfortable on a 4K and absurd on a 15", so no pair of
// bounds can be true for the whole fleet. What protects an operator BETWEEN them is
// the administration screen, which shows the resulting grid before the file is saved,
// and the fact that getting it wrong is repaired by coming back.
const (
	// GridColumnsAutomatic is the DEFAULT, and it is a BEHAVIOUR and not a number: the
	// grid fills itself the way it does today -- five columns on the 24" of the parc,
	// ten on a 4K -- so a file written before this setting existed, and a cooperative
	// that never touches it, keep the grid they have. ADR-035 stays whole.
	GridColumnsAutomatic = 0
	// MinGridColumns is the floor of the override: under three, a grid is no longer a
	// grid.
	//
	// True of the reference screen and FALSE of a 15", where three columns in two tiers
	// show no whole tile at all -- 439,6 px of tile for 424 px of height. That is
	// geometry, not a defect, and it is what §14.4 shows an operator BEFORE the save.
	MinGridColumns = 3
	// MaxGridColumns is the ceiling: beyond twelve, the tile of the reference screen of
	// §14.2 passes under what that section holds for readable.
	//
	// CONFIRMED by browser measurement on 01/08/2026 -- 355 real products, every count
	// from MinGridColumns to MaxGridColumns, on 1366, 1920 and 3840, in two tiers: no
	// name cropped, no price cropped, no horizontal scrollbar anywhere. It holds ONLY
	// because the typographic floor came down to 16 px: at 18, twelve columns on a 15"
	// cropped 38 prices and this ceiling would have had to be 11 (ADR-057).
	MaxGridColumns = 12
)

// UIConfig holds the screen settings an operator has a legitimate choice about.
//
// ShowByUnitProducts is a choice about the SHOP and not about the screen's looks: a
// tile sold by unit prints a label WITHOUT EVER READING THE SCALE, and a cooperative
// whose counter only weighs has no reason to offer that gesture. It is a display and
// never a refusal — Prepare still judges the qualification alone — so hiding a product
// closes no path, it only stops proposing it.
//
// ABSENT ON PURPOSE, each for a written reason (§11.2, ADR-025):
//   - title: the name of the cooperative lives in station.coop;
//   - open_category: the idle view is the COMPLETE grid, categories are filters and
//     not four pre-built screens;
//   - grid_density: still absent, and GridColumns below is NOT it under another name.
//     A density is a proportion, so one figure written by hand lands on five, six or
//     twelve columns depending on the screen -- fitting the setting to a HETEROGENEOUS
//     FLEET, which is the work `clamp()` does better than an operator, and still does,
//     since it remains the default. GridColumns settles another question altogether,
//     « combien de produits voir d'un coup »: no measurement of a screen answers that
//     one, so ADR-025 grants it a setting (ADR-057 amends ADR-035 without reversing
//     it, and does not revive ADR-031). The two physical constraints that once forbade
//     any such setting -- a touch target of at least 72 px, a 69-character name read
//     at 60-80 cm -- have not moved an inch: they stopped being a prohibition and
//     became what the administration screen ANNOUNCES before the file is saved.
//     (69 and not 49: 49 was the longest name of flv_1.csv, the 2022 catalogue, which
//     §10.2 records as unrepresentative. The catalogue in service reaches 69, which is
//     what app.css and Grid.svelte have always been sized against);
//   - success_delay_ms, reject_delay_ms, switch_delay_s: code constants. No
//     operator has a legitimate choice to make about how long a success
//     acknowledgement lasts.
type UIConfig struct {
	Language string `json:"language"`
	Sound    bool   `json:"sound"`
	// IdleTimeoutSeconds clears a FORGOTTEN entry -- a tare, a tile count. Default
	// 45: a trade-off between the slow customer and the reset for the next one. It
	// closes no "screen", there are none left.
	IdleTimeoutSeconds int `json:"idle_timeout_s"`
	// ReprintWindowSeconds is how long the PERMANENT bottom bar stays active.
	// Default 60: a trade-off between serving the customer and the fraud window.
	ReprintWindowSeconds int  `json:"reprint_window_s"`
	ShowGridPrices       bool `json:"show_grid_prices"`
	// ShowByUnitProducts puts the by-unit tiles back into the grid. Default false: the
	// key is named after the PREFIX of the barcode, which alone carries the sale mode,
	// and never after the `unite` column of the CSV, which is a price label and decides
	// nothing.
	ShowByUnitProducts bool `json:"show_by_unit_products"`
	// GridColumns is HOW MANY COLUMNS the client grid shows, and GridColumnsAutomatic
	// -- its default, which is also the zero value -- means AUTOMATIC and never « aucune
	// colonne »: the grid then behaves exactly as it does today, following the screen it
	// is displayed on.
	//
	// From MinGridColumns to MaxGridColumns it means THAT MANY COLUMNS ON ANY SCREEN,
	// and the rest of the tile follows. A count and not a scale factor, on purpose: a
	// factor sits on top of the automatic density and therefore lands on five, six or
	// twelve columns depending on the screen for the ONE value written, whereas the file
	// here describes a grid.
	//
	// It is a SETTING because what it settles is « combien de produits voir d'un coup »
	// -- a shop decision, taken by whoever knows their customers and their catalogue,
	// which no measurement of a screen can answer. It is an OVERRIDE and never a
	// replacement (ADR-057).
	GridColumns int `json:"grid_columns"`
}

// ScaleConfig is the weighing device of this station.
type ScaleConfig struct {
	// Type names a HARDWARE PROTOCOL and nothing else: gram-xfoc-rs or
	// gram-xfoc-plus (§9.3). It may be empty on a station that declares it has no
	// scale -- there is then no protocol to name.
	Type string `json:"type"`
	// Present means "this station has a scale". It is PROPOSED by the detection at
	// first start; at false it is the EXPLICIT and unique declaration of a station
	// without one, which turns the light off instead of leaving it red and makes
	// manual entry nominal.
	Present bool `json:"present"`
	// Options carries port, baud, bits, parity, stop and the reconnection backoff.
	// They are only required when Present.
	Options DriverOptions `json:"options"`
	// ManualEntryAllowed is the ONLY operator switch of the degraded mode.
	ManualEntryAllowed bool `json:"manual_entry_allowed"`
	// DegradeAfterSeconds is how long a silent scale is tolerated before the station
	// says so. Default 20.
	DegradeAfterSeconds int `json:"degrade_after_s"`
}

// PrinterConfig is the label printer of this station.
type PrinterConfig struct {
	// Type is raster (default), sbpl or preview (§8.1).
	Type string `json:"type"`
	// Template names one of the shipped label layouts, weighing_identical in
	// production (A1).
	Template string `json:"template"`
	// Options carries the transport and everything the device needs: queue, path,
	// address, fallback, darkness, speed, offsets, invert_bits, copies and the roll
	// capacity.
	Options DriverOptions `json:"options"`
}

// BarcodeConfig is what is left of the barcode block once the numbering plan
// became a constant of the binary -- and that is the whole point (ADR-028).
//
// ABSENT ON PURPOSE: content, weight_decimals, units_field_width, weight_prefix,
// unit_prefix and rules_by_prefix, all six REFUSED by control 20; resolution_dpi,
// because template.media.dots_per_mm is the single source of resolution
// (mineur-3); the module and the bar height, which belong to the TEMPLATE (§7.2).
type BarcodeConfig struct {
	// VerifyReferenceCheckDigit rejects a catalog reference whose check digit is
	// wrong instead of re-deriving one silently.
	VerifyReferenceCheckDigit bool `json:"verify_reference_check_digit"`
}

// CatalogConfig is where the products come from and how they are shelved.
//
// ABSENT ON PURPOSE (§11.2):
//   - options.pattern: "flv_<n>.csv" is a constant of the exchange format, like the
//     semicolon and the order of the seven columns. The name derives from
//     station.number and from nothing else; two declarations of the same fact is
//     the failure the legacy application died of;
//   - mappings: F/L/V/A → fruits/vegetables/bulk/other is a constant of the Odoo
//     adapter. No operator has a legitimate choice to make about "does L mean
//     vegetables or fruits?".
type CatalogConfig struct {
	// Type is local_drop or webdav. "manual" is NOT a source: the drag and drop of
	// the administration screen writes into local_drop and the polling does the
	// rest (A4).
	Type string `json:"type"`
	// Options carries the URL and credentials of webdav, the separator, the polling
	// and stability counts, the last-resort size guards and the two quality guards.
	Options DriverOptions `json:"options"`
	Images  ImagesConfig  `json:"images"`
	// FallbackCategory is where a letter outside F/L/V/A lands (§10.2 bis). It is
	// what makes "the grid is empty because of an unexpected category" impossible.
	FallbackCategory string `json:"fallback_category"`
	// Categories carry the label, the rank, the colour and "show this category ON
	// THIS STATION" -- real shop decisions. The LETTER belongs to the producer.
	Categories []Category `json:"categories"`
}

// ImagesConfig says where the product images come from (§10.7).
type ImagesConfig struct {
	Source string `json:"source"`
	// Path is only read when Source is image_directory; empty means
	// <data>/product_images/.
	Path string `json:"path"`
}

// JournalConfig bounds what the station KEEPS.
//
// ABSENT ON PURPOSE: capture_frames. A setting with a single correct value is not a
// setting -- at false it broke, in one go, the viewer of the last 20 frames, the
// last 30 frames of diagnostic.zip and the living corpus that feeds the permanent
// tests, that is, the backbone of remote support. Capture is a bounded in-memory
// ring, ALWAYS on. What stays adjustable is the RETENTION of what is persisted.
type JournalConfig struct {
	MaxRows      int `json:"max_rows"`
	MaxDays      int `json:"max_days"`
	MaxTechnical int `json:"max_technical"`
}

// AdminConfig protects everything that WRITES the configuration.
//
// Troubleshooting itself is deliberately unprotected (ADR-018): whoever stands
// behind the counter can already unplug the printer, so a password adds no security
// there and removes all the troubleshooting.
type AdminConfig struct {
	// PasswordHash is an argon2id PHC string. It is NEVER exported, hashed or not:
	// on import a station without one runs the "first access" journey, which imposes
	// setting one.
	PasswordHash string `json:"password_hash"`
	// RecoveryCodeHash is the hash of the 8-character code printed on the
	// installation sheet, which resets the password FROM THE SCREEN -- indispensable
	// on a station in Assigned Access, where there is neither desktop nor prompt.
	RecoveryCodeHash string `json:"recovery_code_hash"`
	SessionMinutes   int    `json:"session_minutes"`
	// AttemptsPerMinute is the per-IP rate limit before a five-minute lockout.
	AttemptsPerMinute int `json:"attempts_per_minute"`
}

// MaintenanceConfig holds the two housekeeping settings.
type MaintenanceConfig struct {
	WeeklyIntegrityCheck bool `json:"weekly_integrity_check"`
	DiskAlertMB          int  `json:"disk_alert_mb"`
}

// DefaultUpdateRepository is the repository a station follows when its file names
// none.
//
// THE ABSENCE OF THE KEY IS LEGAL, and that is deliberate: a file written before
// this block existed must read back with nothing said. The symmetric mistake --
// making a new key mandatory -- is what made a station refuse its own delivered
// configuration on 28/07/2026, and it took seven tests in three packages down at
// once.
const DefaultUpdateRepository = "lostmind84/OpenScale"

// UpdateConfig says where this station looks for a newer version of itself.
//
// The code is under the AGPL: a cooperative running its own fork must be able to
// follow it, and that is the whole reason this is a setting rather than a
// constant.
//
// WHAT THE FILE NAMES IS AN owner/repo PAIR AND NEVER A URL. The host is compiled
// into the binary. A field taking a whole address would turn « save the
// configuration » into « download code from anywhere, and run it as LocalSystem »
// -- and writing the configuration is precisely what the administration screen
// exists to do. Control 48 is what holds that line.
//
// It travels in Export(false), so it enters the fingerprint: the four stations of
// one cooperative must follow the same repository, and a station that diverges is
// visible on the eight characters a volunteer compares by eye.
type UpdateConfig struct {
	Repository string `json:"repository"`
}
