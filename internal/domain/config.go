package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// This file owns the SCHEMA of config.json and its 45 validation controls.
//
// One JSON file, which is also the export format (§11.1): encoding/json serialises
// the very structure the administration screen edits, so "clone a station" is a
// file copy. It is deliberately NOT in the database -- a volunteer must be able to
// copy it onto a USB stick, and the process must start and show an administration
// screen even when the database is corrupt.
//
// Nothing here reads a clock, opens a file or a socket: Validate is pure, and the
// two questions a pure function cannot answer -- "does this path exist?", "is this
// print queue really enumerated?" -- arrive through Registries.

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

// fingerprintLength is how many hexadecimal characters the dashboard shows.
//
// Eight is what makes "do the four stations display the same string?" a check
// anybody can do by eye -- which the 227 _Poste1..4 columns of the legacy
// application never allowed.
const fingerprintLength = 8

// retiredKeys are the keys control 20 REFUSES outright, each with the reason §11.2
// gives for its removal.
//
// Two families, and refusing rather than ignoring is the whole point of both.
// The first six used to declare a piece of the numbering plan from a file; the
// plan is now a CONSTANT OF THE BINARY indexed by prefix and self-checked at
// start-up (ADR-028), because a field that changes the MEANING of the code the
// till reads is not a setting, it is an external contract. The last two are the
// rational coefficient ADR-034 replaced by a percentage: encoding/json drops
// what no field claims, so an old file would decode in silence with every
// discount at zero -- and every member would pay the full price with nothing to
// say why.
var retiredKeys = map[string]string{
	"weight_decimals":   "les décimales du poids sont déclarées par le plan compilé, indexé par préfixe (ADR-028)",
	"units_field_width": "la largeur du champ des unités est déclarée par le plan compilé, indexé par préfixe (ADR-028)",
	"weight_prefix":     "les préfixes au poids sont déclarés par le plan compilé (0493 à 0498), jamais par un fichier",
	"unit_prefix":       "le préfixe à l'unité est déclaré par le plan compilé (0499), jamais par un fichier",
	"content":           "ce que transporte la charge utile est déclaré par le plan compilé, jamais par un fichier",
	"rules_by_prefix":   "la table de règles par préfixe est remplacée par le plan compilé, auto-contrôlé au démarrage",
	"coef_num":          "la remise d'un tarif se déclare en pourcentage : discount_percent, au dixième de point (ADR-034)",
	"coef_den":          "la remise d'un tarif se déclare en pourcentage : discount_percent, il n'y a plus de dénominateur (ADR-034)",
	"tile_size":         "la densité de la grille s'adapte en continu à l'écran (clamp CSS), il n'y a plus de palier à choisir (ADR-035, remplace ADR-031)",
}

// retiredScaleTypes are the two values that LEFT the scale enumeration (§9.3),
// each with the reason it left.
//
// The previous version mixed two protocols, a DEGRADED MODE and a TEST TOOL in one
// drop-down list shown to a volunteer. The same state was then reachable through
// three doors -- a configuration value, an automatic fallback, a troubleshooting
// button -- which made the only question that matters on a bad morning undecidable:
// why is this station in manual entry? Refusing the two values is what keeps the
// three questions separate.
var retiredScaleTypes = map[string]string{
	SourceManual: "« manual » est un ÉTAT, pas un protocole : un poste sans balance se déclare avec scale.present = false, et la saisie à la main s'autorise avec manual_entry_allowed",
	SourceReplay: "« replay » est un outil de diagnostic (openscale capture / openscale replay, bouton « Rejouer cette trame »), il n'a rien à faire dans la liste du matériel de pesée",
}

// serialTransports are the transport names control 42 refuses for a printer.
var serialTransports = []string{"serial", "rs232", "rs-232", "com"}

// Config is the whole configuration of a station, and it is the file on disk.
type Config struct {
	// Version is the schema version of the file, not the version of the binary.
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

// UIConfig holds the only four screen settings an operator has a legitimate choice
// about.
//
// ABSENT ON PURPOSE, each for a written reason (§11.2, ADR-025):
//   - title: the name of the cooperative lives in station.coop;
//   - open_category: the idle view is the COMPLETE grid, categories are filters and
//     not four pre-built screens;
//   - grid_density: a single density, derived from two physical constraints -- a
//     touch target of at least 72 px and the readability of a 49-character name at
//     60-80 cm;
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

// --- JSON, for the three domain types that predate the configuration file ------
//
// Their codecs live here rather than beside them on purpose: safeguard.go says what
// a THRESHOLD is, quantity.go what a ROUNDING is, product.go what a CATEGORY is.
// How a file spells them is the business of the file, and the key names of §11.2
// then live in exactly one place.

// limitsJSON is the on-file shape of WeighingLimits.
type limitsJSON struct {
	EmptyMax           Grams `json:"empty_max_g"`
	BasketCheckEnabled bool  `json:"basket_check_enabled"`
	BasketMin          Grams `json:"basket_min_g"`
	BasketMax          Grams `json:"basket_max_g"`
	MinWeight          Grams `json:"min_weight_g"`
	MaxWeight          Grams `json:"max_weight_g"`
	MaxTare            Grams `json:"max_tare_g"`
	MinUnits           int   `json:"min_units"`
	MaxUnits           int   `json:"max_units"`
	MaxAmount          Cents `json:"max_amount_cents"`
}

// MarshalJSON writes the thresholds under the key names of §11.2.
func (l WeighingLimits) MarshalJSON() ([]byte, error) {
	return json.Marshal(limitsJSON{
		EmptyMax: l.EmptyMax, BasketCheckEnabled: l.BasketCheckEnabled,
		BasketMin: l.BasketMin, BasketMax: l.BasketMax,
		MinWeight: l.MinWeight, MaxWeight: l.MaxWeight, MaxTare: l.MaxTare,
		MinUnits: l.MinUnits, MaxUnits: l.MaxUnits, MaxAmount: l.MaxAmount,
	})
}

// UnmarshalJSON reads the thresholds, keeping whatever the block does not name.
//
// Keeping rather than zeroing is what makes the field-by-field merge of an import
// (§11.5) behave: a partial block overlays the target instead of erasing it.
func (l *WeighingLimits) UnmarshalJSON(raw []byte) error {
	on := limitsJSON{
		EmptyMax: l.EmptyMax, BasketCheckEnabled: l.BasketCheckEnabled,
		BasketMin: l.BasketMin, BasketMax: l.BasketMax,
		MinWeight: l.MinWeight, MaxWeight: l.MaxWeight, MaxTare: l.MaxTare,
		MinUnits: l.MinUnits, MaxUnits: l.MaxUnits, MaxAmount: l.MaxAmount,
	}
	if err := json.Unmarshal(raw, &on); err != nil {
		return err
	}
	*l = WeighingLimits{
		EmptyMax: on.EmptyMax, BasketCheckEnabled: on.BasketCheckEnabled,
		BasketMin: on.BasketMin, BasketMax: on.BasketMax,
		MinWeight: on.MinWeight, MaxWeight: on.MaxWeight, MaxTare: on.MaxTare,
		MinUnits: on.MinUnits, MaxUnits: on.MaxUnits, MaxAmount: on.MaxAmount,
	}
	return nil
}

// categoryJSON is the on-file shape of Category.
type categoryJSON struct {
	Code    string `json:"code"`
	Label   string `json:"label"`
	Rank    int    `json:"rank"`
	Color   string `json:"color"`
	Visible bool   `json:"visible"`
}

// MarshalJSON writes a category under the key names of §11.2.
func (c Category) MarshalJSON() ([]byte, error) {
	return json.Marshal(categoryJSON{c.Code, c.Label, c.Rank, c.Color, c.Visible})
}

// UnmarshalJSON reads a category, keeping whatever the object does not name.
func (c *Category) UnmarshalJSON(raw []byte) error {
	on := categoryJSON{c.Code, c.Label, c.Rank, c.Color, c.Visible}
	if err := json.Unmarshal(raw, &on); err != nil {
		return err
	}
	*c = Category{on.Code, on.Label, on.Rank, on.Color, on.Visible}
	return nil
}

// roundingSpellings maps the configuration wording of a policy to the policy.
var roundingSpellings = map[string]RoundingPolicy{
	"half_up":   RoundHalfUp,
	"truncate":  RoundTowardZero,
	"half_even": RoundHalfToEven,
}

// RoundingSpellings reports the three admissible spellings of a rounding policy,
// in a stable order, so that a fault and an admin drop-down list name the same
// three values.
func RoundingSpellings() []string { return []string{"half_up", "truncate", "half_even"} }

// MarshalJSON writes the policy as the word config.json uses.
func (p RoundingPolicy) MarshalJSON() ([]byte, error) { return json.Marshal(p.String()) }

// UnmarshalJSON reads one of the three words of RoundingSpellings.
//
// An unknown word is an ERROR and not a fault, so the configuration never holds a
// policy nobody declared: Divide would silently truncate, and a station would
// under-charge by a cent for months without anyone able to name why. §11.4 turns
// this into the 400 Bad Request of step 1, and the error names the three values.
func (p *RoundingPolicy) UnmarshalJSON(raw []byte) error {
	var word string
	if err := json.Unmarshal(raw, &word); err != nil {
		return fmt.Errorf("domain: un arrondi est un mot parmi %s : %w",
			strings.Join(RoundingSpellings(), ", "), err)
	}
	policy, ok := roundingSpellings[word]
	if !ok {
		return fmt.Errorf("domain: arrondi inconnu %q, valeurs admises : %s",
			word, strings.Join(RoundingSpellings(), ", "))
	}
	*p = policy
	return nil
}

// UnmarshalJSON reads the configuration and remembers the retired keys it carried.
//
// The scan happens HERE and not in Validate because Validate only sees a Go
// structure, in which a retired key cannot exist: encoding/json drops what no field
// claims. Control 20 has to refuse the FILE, so the file is what gets read.
func (c *Config) UnmarshalJSON(raw []byte) error {
	// The generic pass FIRST, so that "this is not JSON" and "this JSON puts a word
	// where a number belongs" are two distinct errors rather than one.
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return err
	}
	// An alias, otherwise this method calls itself.
	type alias Config
	shadow := alias(*c)
	if err := json.Unmarshal(raw, &shadow); err != nil {
		return err
	}
	*c = Config(shadow)
	c.retired = nil
	scanRetired("", document, &c.retired)
	return nil
}

// scanRetired appends the dotted path of every retired key of a decoded document.
func scanRetired(prefix string, value any, out *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range sortedKeys(typed) {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			if _, retired := retiredKeys[key]; retired {
				*out = append(*out, path)
			}
			scanRetired(path, typed[key], out)
		}
	case []any:
		for i, item := range typed {
			scanRetired(fmt.Sprintf("%s[%d]", prefix, i), item, out)
		}
	}
}

// --- Driver options ------------------------------------------------------------

// DriverOptions is the driver-specific half of a hardware or catalog block.
//
// It stays UNTYPED on purpose: the administration screen generates its form from
// the schema the driver DECLARES (§9.3), so adding a scale model must not mean
// adding a Go field here. The values are kept as raw JSON rather than as `any`
// because decoding into `any` turns every number into a float64, and no float
// carries a quantity in this application.
type DriverOptions map[string]json.RawMessage

// Text reports a string option, and whether it is present and really a string.
func (o DriverOptions) Text(key string) (string, bool) {
	raw, ok := o[key]
	if !ok {
		return "", false
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

// Int reports a whole-number option, and whether it is present and really whole.
func (o DriverOptions) Int(key string) (int64, bool) {
	number, ok := jsonNumber(o[key])
	if !ok {
		return 0, false
	}
	value, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

// jsonNumber decodes a raw value as a JSON number, refusing a QUOTED one.
//
// The refusal is deliberate: encoding/json happily reads a quoted numeric literal
// into a json.Number, so `"baud": "9600"` would pass silently. A configuration that
// spells a baud rate as text has a type error, and the driver form is what must say
// so -- the admin screen offers a numeric field, and a file that came from somewhere
// else has to be told.
func jsonNumber(raw json.RawMessage) (json.Number, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] == '"' {
		return "", false
	}
	var number json.Number
	if json.Unmarshal(trimmed, &number) != nil {
		return "", false
	}
	return number, true
}

// Ratio reports a fractional option, and whether it is present and numeric.
//
// The only floats a configuration carries are RATIOS -- min_readable_ratio,
// max_weighable_drop -- and never a mass, a price or a length.
func (o DriverOptions) Ratio(key string) (float64, bool) {
	number, ok := jsonNumber(o[key])
	if !ok {
		return 0, false
	}
	value, err := number.Float64()
	if err != nil {
		return 0, false
	}
	return value, true
}

// Bool reports a boolean option, and whether it is present and really a boolean.
func (o DriverOptions) Bool(key string) (bool, bool) {
	raw, ok := o[key]
	if !ok {
		return false, false
	}
	var value bool
	if json.Unmarshal(raw, &value) != nil {
		return false, false
	}
	return value, true
}

// Group reports a nested option object, such as printer.options.fallback.
func (o DriverOptions) Group(key string) (DriverOptions, bool) {
	raw, ok := o[key]
	if !ok {
		return nil, false
	}
	var value DriverOptions
	if json.Unmarshal(raw, &value) != nil {
		return nil, false
	}
	return value, true
}

// Has reports whether the option is present, whatever its value.
func (o DriverOptions) Has(key string) bool {
	_, ok := o[key]
	return ok
}

// Keys reports the option names in a stable order, so that two runs of the
// validation produce the faults in the same sequence.
func (o DriverOptions) Keys() []string { return sortedKeys(o) }

// clone returns a shallow copy, so that Export can strip a secret without reaching
// into the configuration the station is running on.
func (o DriverOptions) clone() DriverOptions {
	if o == nil {
		return nil
	}
	out := make(DriverOptions, len(o))
	for key, value := range o {
		out[key] = value
	}
	return out
}

// --- Registries ----------------------------------------------------------------

// OptionKind names the shape one driver option accepts.
type OptionKind uint8

const (
	// OptionText is any string.
	OptionText OptionKind = iota
	// OptionInt is a whole number, bounded by Min and Max when Max is non-zero.
	OptionInt
	// OptionBool is a boolean.
	OptionBool
	// OptionRatio is a fraction between Min and Max expressed in per mille, which is
	// how a ratio gets bounded without a float ever entering a declaration.
	OptionRatio
	// OptionEnum is one of Values.
	OptionEnum
	// OptionHostPort is a host:port pair.
	OptionHostPort
	// OptionURL is an absolute http or https URL.
	OptionURL
	// OptionGroup is a nested object whose own schema is Options.
	OptionGroup
)

// String reports the kind the way a fault names it, in French.
func (k OptionKind) String() string {
	switch k {
	case OptionText:
		return "texte"
	case OptionInt:
		return "nombre entier"
	case OptionBool:
		return "vrai ou faux"
	case OptionRatio:
		return "nombre"
	case OptionEnum:
		return "valeur d'une liste"
	case OptionHostPort:
		return "hôte:port"
	case OptionURL:
		return "URL http ou https"
	case OptionGroup:
		return "objet"
	}
	return "inconnu"
}

// OptionSchema declares one option of a driver.
//
// It is what lets the administration screen GENERATE its form and the validation
// check the OPTIONS of a driver instead of only its type name: `port` among the
// enumerated ports, `queue` among the queues REALLY visible, `address` as
// host:port (§11.3).
type OptionSchema struct {
	Key      string
	Kind     OptionKind
	Required bool
	// Values is the closed list of an enum, and for an option the platform can
	// enumerate -- a serial port, a print queue -- the values it REALLY found. An
	// empty list means "we could not enumerate": the form is checked, membership is
	// not.
	Values []string
	// Min and Max bound an OptionInt, and an OptionRatio IN PER MILLE. Both zero
	// means unbounded.
	Min, Max int64
	// Options is the schema of a nested OptionGroup.
	Options []OptionSchema
}

// DriverDescriptor is what validating a configuration needs to know about a
// driver: its registry key, the wording a volunteer reads, and the schema of its
// options.
type DriverDescriptor struct {
	// ID is the registry key, the value that goes into the file: "gram-xfoc-plus".
	ID string
	// Label is what the drop-down list shows, in French: "GRAM XFOC +".
	Label   string
	Options []OptionSchema
}

// PathChecker answers the one question a pure validation cannot: is this path
// readable FROM THE CONTEXT OF THE SERVICE?
//
// It is an interface declared on the consumer side, and a nil one is a legitimate
// state: `openscale config validate` on a laptop cannot know what the service
// account sees. The form is then validated and existence is not.
type PathChecker interface {
	// Readable reports nil when the service could read that path.
	Readable(path string) error
}

// Registries carries the driver descriptors and the templates a running binary
// knows.
//
// It exists so that the validation can check the OPTIONS of each driver and not
// merely say "unknown type". An EMPTY registry is a legitimate state -- the drivers
// are delivered by later lots, and `openscale config validate` may run outside the
// service: membership is then not checked and the message says so. What is always
// checked is the FORM, and the values that were RETIRED with a written reason.
type Registries struct {
	Scales         []DriverDescriptor
	Printers       []DriverDescriptor
	Transports     []DriverDescriptor
	CatalogSources []DriverDescriptor
	// Templates is the label layouts this binary can load. Nil means "the templates
	// compiled into the binary", which is where they live until L4.
	Templates map[string]Template
	// Paths probes the filesystem for control 44. Nil means "we cannot know".
	Paths PathChecker
}

// ScaleTypes reports the scale protocols a volunteer may choose from.
func (r Registries) ScaleTypes() []string { return descriptorIDs(r.Scales) }

// PrinterTypes reports the printer drivers a volunteer may choose from.
func (r Registries) PrinterTypes() []string { return descriptorIDs(r.Printers) }

// TransportNames reports the byte transports a volunteer may choose from.
func (r Registries) TransportNames() []string { return descriptorIDs(r.Transports) }

// CatalogSourceNames reports the catalog sources a volunteer may choose from.
func (r Registries) CatalogSourceNames() []string { return descriptorIDs(r.CatalogSources) }

// TemplateNames reports the label layouts this binary can load, in a stable order.
func (r Registries) TemplateNames() []string { return sortedKeys(r.templates()) }

// Template returns a layout by name, and whether it exists.
func (r Registries) Template(name string) (Template, bool) {
	template, ok := r.templates()[name]
	return template, ok
}

// templates falls back on the layouts compiled into the binary, which is where
// they live until the rendering engine turns them into files (templates.go).
func (r Registries) templates() map[string]Template {
	if r.Templates != nil {
		return r.Templates
	}
	return ShippedTemplates()
}

func descriptorIDs(list []DriverDescriptor) []string {
	if len(list) == 0 {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, descriptor := range list {
		out = append(out, descriptor.ID)
	}
	sort.Strings(out)
	return out
}

func descriptorByID(list []DriverDescriptor, id string) *DriverDescriptor {
	for i := range list {
		if list[i].ID == id {
			return &list[i]
		}
	}
	return nil
}

// --- Validation ----------------------------------------------------------------

// Validate returns ALL the faults, not the first one: the administration screen is
// used by volunteers, it must report everything at once, in French, with the
// offending field named and, whenever possible, the list of available values in
// Fault.Values.
//
// reg carries the driver descriptors, which is what allows the options of each
// driver to be validated instead of just its type; an empty registry validates the
// form and not the existence.
//
// An invalid configuration NEVER kills the process (§11.3): the server starts in
// "invalid configuration" mode, loads NeutralProfile in memory WITHOUT writing,
// serves this list of faults and shows a full-screen « Poste en configuration
// d'usine (ERR-CFG-01) ». A broken configuration must never produce a black screen.
func (c *Config) Validate(reg Registries) []Fault {
	var faults []Fault
	fail := func(field, format string, args ...any) {
		faults = append(faults, Fault{Field: field, Message: fmt.Sprintf(format, args...)})
	}
	failWith := func(field string, values []string, format string, args ...any) {
		faults = append(faults, Fault{
			Field: field, Message: fmt.Sprintf(format, args...), Values: values,
		})
	}

	// 1. station.number ∈ [1,99]. It is what the watched file name derives from.
	if c.Station.Number < 1 || c.Station.Number > 99 {
		fail("station.number", "%d hors bornes [1, 99] : c'est de ce numéro que dérive le nom du fichier surveillé, flv_<n>.csv",
			c.Station.Number)
	}

	// 2. network.listen parseable.
	if err := checkHostPort(c.Network.Listen); err != nil {
		fail("network.listen", "%q n'est pas une adresse hôte:port valide (%s)", c.Network.Listen, err)
	}

	// 3. scale.type known -- EXACTLY the protocols of the registry (§9.3) -- and the
	//    serial options are only required when scale.present.
	switch {
	case c.Scale.Type == "" && c.Scale.Present:
		failWith("scale.type", reg.ScaleTypes(), "aucun protocole n'est déclaré alors que le poste déclare une balance")
	case c.Scale.Type == "":
		// A station that declares it has no scale names no protocol, and that is
		// deliberate: the neutral profile must not name a piece of hardware.
	default:
		if reason, retired := retiredScaleTypes[c.Scale.Type]; retired {
			failWith("scale.type", reg.ScaleTypes(), "%q n'est plus une valeur de scale.type : %s", c.Scale.Type, reason)
		} else if available := reg.ScaleTypes(); len(available) > 0 && !known(available, c.Scale.Type) {
			failWith("scale.type", available, "protocole inconnu %q", c.Scale.Type)
		}
	}
	if c.Scale.Present {
		if port, ok := c.Scale.Options.Text("port"); !ok || port == "" {
			// Only the port cannot be guessed: baud, bits, parity and stop come from
			// the model defaults the driver declares (§9.3).
			fail("scale.options.port", "un poste qui déclare une balance doit nommer son port")
		}
	}

	// 4. printer.type known -- exactly the three registered descriptors, raster by
	//    default, sbpl and preview (§8.1, §8.2).
	if c.Printer.Type == "" {
		failWith("printer.type", reg.PrinterTypes(), "aucun driver d'impression n'est déclaré")
	} else if available := reg.PrinterTypes(); len(available) > 0 && !known(available, c.Printer.Type) {
		failWith("printer.type", available, "driver d'impression inconnu %q", c.Printer.Type)
	}

	// 5. catalog.type known. "manual" is NOT a source: the drag and drop of the
	//    administration screen writes into local_drop (A4, §10.1).
	switch {
	case c.Catalog.Type == "":
		failWith("catalog.type", reg.CatalogSourceNames(), "aucune source de catalogue n'est déclarée")
	case c.Catalog.Type == CatalogSourceManual:
		failWith("catalog.type", reg.CatalogSourceNames(),
			"%q n'est pas une source : le glisser-déposer de l'administration écrit dans %s, et la scrutation fait le reste",
			CatalogSourceManual, CatalogSourceLocalDrop)
	default:
		if available := reg.CatalogSourceNames(); len(available) > 0 && !known(available, c.Catalog.Type) {
			failWith("catalog.type", available, "source de catalogue inconnue %q", c.Catalog.Type)
		}
	}

	// 6. scale.options validated by the schema the scale driver declares.
	faults = append(faults, validateOptions("scale.options", c.Scale.Options,
		descriptorByID(reg.Scales, c.Scale.Type))...)

	// 7. printer.options validated by the schema the printer driver declares.
	faults = append(faults, validateOptions("printer.options", c.Printer.Options,
		descriptorByID(reg.Printers, c.Printer.Type))...)

	// 8. printer.options.transport is one of the registered transports.
	transport, hasTransport := c.Printer.Options.Text("transport")
	if hasTransport && transport != "" {
		if available := reg.TransportNames(); len(available) > 0 && !known(available, transport) {
			failWith("printer.options.transport", available, "transport inconnu %q", transport)
		}
	}

	// 9. catalog.options validated by the schema the source declares.
	faults = append(faults, validateOptions("catalog.options", c.Catalog.Options,
		descriptorByID(reg.CatalogSources, c.Catalog.Type))...)

	// 10. At least one tier. Dual pricing is not a boolean, it is the cardinality of
	//     the grid (§6.3).
	if len(c.Pricing.Tiers) == 0 {
		fail("pricing.tiers", "la grille de tarifs est vide : il en faut au moins un")
	}
	codes := make(map[string]bool, len(c.Pricing.Tiers))
	for i, tier := range c.Pricing.Tiers {
		// 11. The tier reference_code names is the catalog price -- the one the
		//     till charges. Its discount is not a setting, it is zero by
		//     definition, so a file that gives it one is REFUSED rather than
		//     quietly obeyed (ADR-034).
		if tier.Code == c.Pricing.ReferenceCode && tier.Discount != 0 {
			fail(fmt.Sprintf("pricing.tiers[%d].discount_percent", i),
				"le tarif de référence est le prix du catalogue : il ne porte pas de remise")
		}
		// 12. Codes unique: the code is the key of a tier, in the file, on the label
		//     and in the journal.
		if codes[tier.Code] {
			fail(fmt.Sprintf("pricing.tiers[%d].code", i), "le code %q est déclaré deux fois", tier.Code)
		}
		codes[tier.Code] = true
		// 13. A discount is a percentage between 0 and 100. A hundred is free, and
		//     that is a grid a cooperative may legitimately declare.
		if tier.Discount < 0 || tier.Discount > FullDiscount {
			fail(fmt.Sprintf("pricing.tiers[%d].discount_percent", i),
				"%s %% n'est pas une remise entre 0 et 100 %%", tier.Discount)
		}
	}
	tierCodes := make([]string, 0, len(c.Pricing.Tiers))
	for _, tier := range c.Pricing.Tiers {
		tierCodes = append(tierCodes, tier.Code)
	}

	// 14. primary_code belongs to the grid: it is the price printed LARGE (A7).
	if !codes[c.Pricing.PrimaryCode] {
		failWith("pricing.primary_code", tierCodes, "%q ne désigne aucun tarif de la grille", c.Pricing.PrimaryCode)
	}
	// 15. reference_code belongs to the grid: it is the one encoded when the payload
	//     carries a price, and the till must never under-charge.
	if !codes[c.Pricing.ReferenceCode] {
		failWith("pricing.reference_code", tierCodes, "%q ne désigne aucun tarif de la grille", c.Pricing.ReferenceCode)
	}
	// 16. Each secondary code belongs to the grid.
	for i, code := range c.Pricing.SecondaryCodes {
		if !codes[code] {
			failWith(fmt.Sprintf("pricing.secondary_codes[%d]", i), tierCodes,
				"%q ne désigne aucun tarif de la grille", code)
		}
	}

	// 17-19. The internal numbering plan SELF-CHECKS at start-up (§6.2, ADR-028):
	//        every declared prefix is exactly four digits, 4 + ref + payload + 1 = 13,
	//        and no prefix is declared twice. init() already panics on a broken plan,
	//        so these three can only fail in a test that hands over a broken table --
	//        which is exactly why they are a function and not inline code: an
	//        inconsistent plan must stop the process AT START-UP, never at print time.
	faults = append(faults, validateNumberingPlan(internalPlan)...)

	// 20. A configuration still carrying a retired key -- numbering plan or pricing
	//     coefficient -- is REFUSED.
	for _, path := range c.retired {
		key := path[strings.LastIndexByte(path, '.')+1:]
		fail(path, "clé supprimée : %s", retiredKeys[key])
	}

	// 21. template.media.dots_per_mm is the SINGLE source of resolution (mineur-3):
	//     barcode.resolution_dpi is gone, and every geometric rule divides the world
	//     by this number.
	template, templateExists := reg.Template(c.Printer.Template)
	resolutionUsable := templateExists && template.Media.DotsPerMM > 0
	if templateExists && !resolutionUsable {
		fail("template.media.dots_per_mm",
			"le gabarit %q ne déclare aucune résolution utilisable (8 sur une WS408, 12 sur une WS412)",
			c.Printer.Template)
	}

	// 22. basket_min_g ≤ basket_max_g ≤ 0: the window means "the customer lifted off
	//     a basket the scale was tared for", so it is NEGATIVE by nature.
	if c.Limits.BasketMin > c.Limits.BasketMax || c.Limits.BasketMax > 0 {
		fail("limits.basket_min_g",
			"la fenêtre du panier (%d ≤ %d ≤ 0) est incohérente : elle décrit un poids négatif",
			c.Limits.BasketMin, c.Limits.BasketMax)
	}

	// 23. min_weight_g < max_weight_g ≤ 99999. The ceiling is the CAPACITY of the
	//     NNDDD field of the barcode, not a plausibility threshold.
	if c.Limits.MinWeight >= c.Limits.MaxWeight || c.Limits.MaxWeight > MaxWeight {
		fail("limits.max_weight_g",
			"les bornes de poids (%d < %d ≤ %d) sont incohérentes : %d g est la capacité du champ NNDDD du code-barres",
			c.Limits.MinWeight, c.Limits.MaxWeight, MaxWeight, MaxWeight)
	}

	// 24. min_units ≤ max_units ≤ 99: two digits in the payload of prefix 0499.
	if c.Limits.MinUnits > c.Limits.MaxUnits || c.Limits.MaxUnits > 99 {
		fail("limits.max_units",
			"les bornes d'unités (%d ≤ %d ≤ 99) sont incohérentes : la charge utile du préfixe à l'unité fait deux chiffres",
			c.Limits.MinUnits, c.Limits.MaxUnits)
	}

	// 25. max_amount_cents ≤ 99999.
	if c.Limits.MaxAmount > 99_999 {
		fail("limits.max_amount_cents", "%d dépasse la capacité du champ de prix du code-barres (99 999 centimes)",
			c.Limits.MaxAmount)
	}

	// 26. timeout_ms > min_duration_ms: a window that expires before it can hold
	//     would time out every single weighing.
	if c.Stability.Timeout <= c.Stability.MinDuration {
		fail("stability.timeout_ms", "%s doit dépasser la durée de stabilité exigée (%s)",
			c.Stability.Timeout, c.Stability.MinDuration)
	}

	// 27. expiry_floor_ms ≥ 1000 and < expiry_ceiling_ms.
	if c.Stability.ExpiryFloor < Duration(time.Second) {
		fail("stability.expiry_floor_ms", "%s est sous le plancher de 1 s : le poids serait déclaré périmé avant la mesure suivante",
			c.Stability.ExpiryFloor)
	}
	if c.Stability.ExpiryFloor >= c.Stability.ExpiryCeiling {
		fail("stability.expiry_ceiling_ms", "%s doit dépasser le plancher de péremption (%s)",
			c.Stability.ExpiryCeiling, c.Stability.ExpiryFloor)
	}

	// 28. stability.mode and on_timeout in the list (A3).
	if !known(stabilityModes(), c.Stability.Mode) {
		failWith("stability.mode", stabilityModes(), "mode inconnu %q", c.Stability.Mode)
	}
	if !known(timeoutActions(), c.Stability.OnTimeout) {
		failWith("stability.on_timeout", timeoutActions(), "action inconnue %q", c.Stability.OnTimeout)
	}

	// 29. The template EXISTS and Template.Validate() passes -- the nine hard rules
	//     of §7.5, on the geometry RECOMPOSED with the operator's offsets.
	if !templateExists {
		failWith("printer.template", reg.TemplateNames(), "gabarit inconnu %q", c.Printer.Template)
	} else if resolutionUsable {
		shifted := template
		shifted.OffsetXDots, _ = intOption(c.Printer.Options, "offset_x")
		shifted.OffsetYDots, _ = intOption(c.Printer.Options, "offset_y")
		for _, fault := range shifted.Validate(len(c.Pricing.Tiers)) {
			fault.Field = "printer.template." + fault.Field
			faults = append(faults, fault)
		}
	}

	// 30. journal.max_rows ≥ 100: below that a purge would erase the day's weighings,
	//     which are the only data of a station that cannot be rebuilt.
	if c.Journal.MaxRows < 100 {
		fail("journal.max_rows", "%d est sous le plancher de 100 pesées conservées", c.Journal.MaxRows)
	}

	// 31. admin.password_hash and admin.recovery_code_hash are USABLE when present.
	//
	// # Empty is not a fault, and that is a correction
	//
	// A station is installed WITHOUT a password: §14.4 says the delivered configuration
	// is the export of §11.5, "qui ne porte aucun secret", and the first access is the
	// recovery code printed on the installation sheet. Refusing an empty field put such
	// a station OUT OF SERVICE (§11.3), so it could not weigh either — and weighing is
	// the one thing it must do whatever else is wrong. What answers "aucun mot de passe
	// n'est posé" is now the administration itself, which offers the recovery code.
	//
	// # What IS a fault: a hash nothing can match
	//
	// The delivered file carried « for-the-delivered-configurationg ». It parses, and its
	// payload is EXACTLY the 32 bytes argon2id produces — so a length check would not
	// have caught it either. It matches no password at all, `config validate` and
	// `doctor` both declared it sound, and install.ps1, seeing a non-empty recovery
	// field, skipped drawing a real code: the installation sheet went out blank and the
	// station was locked out for good.
	for _, secret := range []struct{ field, hash string }{
		{"admin.password_hash", c.Admin.PasswordHash},
		{"admin.recovery_code_hash", c.Admin.RecoveryCodeHash},
	} {
		switch {
		case secret.hash == "":
			// Documented state of a station between its installation and its first access.
		case !wellFormedArgon2id(secret.hash):
			fail(secret.field, "l'empreinte n'est pas une chaîne argon2id de la forme $argon2id$v=19$m=…,t=…,p=…$sel$empreinte")
		case !usableArgon2id(secret.hash):
			fail(secret.field, "l'empreinte est un remplissage : son corps est du texte, là où argon2id produit des octets tirés au sort — aucun mot de passe ne peut y correspondre")
		}
	}

	// 32. catalog.fallback_category belongs to the categories. It is what makes "the
	//     grid is empty because of an unexpected letter" impossible (§10.2 bis).
	categoryCodes := make([]string, 0, len(c.Catalog.Categories))
	present := make(map[string]bool, len(c.Catalog.Categories))
	for _, category := range c.Catalog.Categories {
		categoryCodes = append(categoryCodes, category.Code)
		present[category.Code] = true
	}
	if !present[c.Catalog.FallbackCategory] {
		failWith("catalog.fallback_category", categoryCodes,
			"%q ne désigne aucune catégorie : une lettre hors F/L/V/A n'aurait plus où atterrir",
			c.Catalog.FallbackCategory)
	}

	// 33. Category codes unique.
	seen := make(map[string]bool, len(c.Catalog.Categories))
	for i, category := range c.Catalog.Categories {
		if seen[category.Code] {
			fail(fmt.Sprintf("catalog.categories[%d].code", i), "le code %q est déclaré deux fois", category.Code)
		}
		seen[category.Code] = true
	}

	// 34. min_readable_ratio ∈ [0,1] -- the ABSOLUTE guard, on UNREADABLE rows
	//     (§10.4a).
	if ratio, ok := c.Catalog.Options.Ratio("min_readable_ratio"); ok && (ratio < 0 || ratio > 1) {
		fail("catalog.options.min_readable_ratio", "%v hors bornes [0, 1] : c'est une proportion de lignes lisibles", ratio)
	}

	// 35. Colours as #RRGGBB.
	for i, category := range c.Catalog.Categories {
		if !wellFormedColor(category.Color) {
			fail(fmt.Sprintf("catalog.categories[%d].color", i), "%q n'est pas une couleur #RRGGBB", category.Color)
		}
	}

	// 36. poll_interval_s ≥ 1. The stability check needs two consecutive polls, so a
	//     zero interval would read a file while the producer is still writing it.
	if interval, ok := c.Catalog.Options.Int("poll_interval_s"); ok && interval < 1 {
		fail("catalog.options.poll_interval_s", "%d est sous le plancher d'une seconde", interval)
	}

	// 37. copies ∈ [1,10].
	if copies, ok := c.Printer.Options.Int("copies"); ok && (copies < 1 || copies > 10) {
		fail("printer.options.copies", "%d hors bornes [1, 10]", copies)
	}

	// 38. offset_x/y RECOMPOSED with the geometry of the template (mineur-2): the ±1
	//     dot arrows of the admin screen invite that adjustment, so it must be bounded
	//     by the geometry and not merely by ±99. The message names the admissible
	//     maximum instead of just saying no.
	if templateExists && resolutionUsable {
		maxX, maxY := template.MaxOffsetDots(len(c.Pricing.Tiers))
		if offset, ok := intOption(c.Printer.Options, "offset_x"); ok && (offset < 0 || offset > maxX) {
			fail("printer.options.offset_x",
				"%d dots hors bornes [0, %d] pour le gabarit %q : au-delà, le contenu encré sortirait de l'étiquette",
				offset, maxX, c.Printer.Template)
		}
		if offset, ok := intOption(c.Printer.Options, "offset_y"); ok && (offset < 0 || offset > maxY) {
			fail("printer.options.offset_y",
				"%d dots hors bornes [0, %d] pour le gabarit %q : au-delà, le contenu encré sortirait de l'étiquette",
				offset, maxY, c.Printer.Template)
		}
	}

	// 39. No HTTP(S) host behind a drop path (important-11). local_drop is a
	//     directory the service OWNS and CREATES: a "local" directory demanding an
	//     account and a password would be the Z: drive of the legacy application under
	//     another name.
	if c.Catalog.Type == CatalogSourceLocalDrop {
		for _, key := range c.Catalog.Options.Keys() {
			if value, ok := c.Catalog.Options.Text(key); ok && isHTTPURL(value) {
				failWith("catalog.options."+key, []string{CatalogSourceWebDAV},
					"%q est un hôte HTTP(S) derrière un chemin de dépôt : c'est la source %q qu'il faut choisir",
					value, CatalogSourceWebDAV)
			}
		}
		for _, secret := range []string{"username", "password"} {
			if c.Catalog.Options.Has(secret) {
				failWith("catalog.options."+secret, []string{CatalogSourceWebDAV},
					"%q ne porte ni utilisateur ni mot de passe : il n'y a aucun secret à porter pour lire un répertoire qu'on possède",
					CatalogSourceLocalDrop)
			}
		}
	}

	// 40. max_weighable_drop ∈ [0, 0.5] -- the RELATIVE guard, on WEIGHABLE products
	//     (§10.4b, important-13).
	if drop, ok := c.Catalog.Options.Ratio("max_weighable_drop"); ok && (drop < 0 || drop > 0.5) {
		fail("catalog.options.max_weighable_drop", "%v hors bornes [0, 0,5] : c'est une baisse relative du nombre de produits pesables", drop)
	}

	// 41. roll_capacity ≥ 50. Below that the 90 % alert would fire on the first
	//     labels of a fresh roll and teach a volunteer to ignore it.
	if capacity, ok := c.Printer.Options.Int("roll_capacity"); ok && capacity < 50 {
		fail("printer.options.roll_capacity", "%d est sous le plancher de 50 étiquettes", capacity)
	}

	// 42. A SERIAL transport is forbidden for the printer: a label weighs 16 ko, that
	//     is about 17 s at 9 600 bauds (§8.3).
	if hasTransport && known(serialTransports, strings.ToLower(transport)) {
		failWith("printer.options.transport",
			[]string{TransportWinspool, TransportDevfile, TransportTCP, TransportFile},
			"un transport série est interdit pour l'imprimante : une étiquette pèse 16 ko, soit environ 17 s à 9 600 bauds")
	}

	// 43. Every price carried by a DELIVERED configuration file verifies
	//     0 ≤ price ≤ 999 999 cents -- the third and last imposition of MaxUnitPrice,
	//     with the DDL (§12.3) and the price rule of §10.3. Since §11.5 it is an
	//     ORDINARY configuration control, applied to a file like any other; it used to
	//     validate compiled values, that is, source code (ADR-026).
	faults = append(faults, CheckPrice("limits.max_amount_cents", c.Limits.MaxAmount)...)

	// 44. catalog.images.source in the list, and path readable FROM THE CONTEXT OF THE
	//     SERVICE when the source is image_directory.
	if !known(imageSources(), c.Catalog.Images.Source) {
		failWith("catalog.images.source", imageSources(), "source d'images inconnue %q", c.Catalog.Images.Source)
	}
	if c.Catalog.Images.Source == ImageSourceDirectory {
		switch {
		case c.Catalog.Images.Path == "":
			// Empty is legitimate: it means <data>/product_images/, a directory the
			// service owns.
		case reg.Paths == nil:
			// No probe: we validate the form, we cannot validate the existence.
		default:
			if err := reg.Paths.Readable(c.Catalog.Images.Path); err != nil {
				fail("catalog.images.path", "%q n'est pas lisible depuis le contexte du service (%s)",
					c.Catalog.Images.Path, err)
			}
		}
	}

	// 45. max_image_size_kb ∈ [16, 4096] AND max_image_size_kb × 1024 ≤
	//     max_file_size_mb × 1 048 576: an image cannot be allowed to exceed the file
	//     that contains it (§10.7). The largest image really observed is 11 kB, the
	//     real file 527 kB.
	imageKB, hasImageKB := c.Catalog.Options.Int("max_image_size_kb")
	if hasImageKB && (imageKB < 16 || imageKB > 4096) {
		fail("catalog.options.max_image_size_kb", "%d ko hors bornes [16, 4096]", imageKB)
	}
	if fileMB, ok := c.Catalog.Options.Int("max_file_size_mb"); ok && hasImageKB {
		if imageKB*1024 > fileMB*1_048_576 {
			fail("catalog.options.max_image_size_kb",
				"%d ko dépasse le plafond du fichier qui la contient (%d Mo) : une image ne peut pas être plus grosse que son catalogue",
				imageKB, fileMB)
		}
	}

	return faults
}

// CheckPrice reports the fault a price carried by a delivered configuration file
// breaks, or nothing.
//
// It is the SINGLE implementation of control 43, called by Config.Validate and by
// whoever loads the demonstration products and flv_demo.csv: three files, one rule,
// so that MaxUnitPrice cannot be enforced differently in three places.
func CheckPrice(field string, price Cents) []Fault {
	if price < 0 || price > MaxUnitPrice {
		return []Fault{{
			Field:   field,
			Message: fmt.Sprintf("%d hors bornes [0, %d] centimes", price, MaxUnitPrice),
		}}
	}
	return nil
}

// validateNumberingPlan reports the faults of controls 17 to 19 on a numbering
// plan.
//
// It reuses the very check init() runs at start-up, so the two can never diverge:
// what stops the process is what the administration screen would explain.
func validateNumberingPlan(plan map[string]PrefixPlan) []Fault {
	if err := validatePlan(plan); err != nil {
		return []Fault{{
			Field:   "barcode.plan",
			Message: fmt.Sprintf("le plan de numérotation interne est incohérent : %s", err),
		}}
	}
	return nil
}

// validateOptions reports every fault the options of one driver break against the
// schema THE DRIVER DECLARES.
//
// An unregistered driver -- no descriptor at all -- yields no fault: inventing a
// schema for a driver that has not been written yet would be a second source of
// truth for something the driver owns (ADR-025).
func validateOptions(field string, options DriverOptions, descriptor *DriverDescriptor) []Fault {
	if descriptor == nil {
		return nil
	}
	var faults []Fault
	declared := make(map[string]bool, len(descriptor.Options))
	names := make([]string, 0, len(descriptor.Options))
	for _, schema := range descriptor.Options {
		declared[schema.Key] = true
		names = append(names, schema.Key)
	}
	sort.Strings(names)

	for _, schema := range descriptor.Options {
		path := field + "." + schema.Key
		raw, ok := options[schema.Key]
		if !ok {
			if schema.Required {
				faults = append(faults, Fault{
					Field:   path,
					Message: fmt.Sprintf("option exigée par le driver %q", descriptor.ID),
				})
			}
			continue
		}
		faults = append(faults, schema.check(path, raw)...)
	}
	for _, key := range options.Keys() {
		if !declared[key] {
			faults = append(faults, Fault{
				Field:   field + "." + key,
				Message: fmt.Sprintf("option inconnue du driver %q", descriptor.ID),
				Values:  names,
			})
		}
	}
	return faults
}

// check reports the faults one raw value breaks against this schema entry.
func (s OptionSchema) check(field string, raw json.RawMessage) []Fault {
	fault := func(format string, args ...any) []Fault {
		return []Fault{{Field: field, Message: fmt.Sprintf(format, args...)}}
	}
	single := DriverOptions{s.Key: raw}
	switch s.Kind {
	case OptionText:
		if _, ok := single.Text(s.Key); !ok {
			return fault("attendu : %s", s.Kind)
		}
	case OptionBool:
		if _, ok := single.Bool(s.Key); !ok {
			return fault("attendu : %s", s.Kind)
		}
	case OptionInt:
		value, ok := single.Int(s.Key)
		if !ok {
			return fault("attendu : %s", s.Kind)
		}
		if s.Max != 0 && (value < s.Min || value > s.Max) {
			return fault("%d hors bornes [%d, %d]", value, s.Min, s.Max)
		}
	case OptionRatio:
		value, ok := single.Ratio(s.Key)
		if !ok {
			return fault("attendu : %s", s.Kind)
		}
		// The bounds are declared IN PER MILLE, so no float ever enters a
		// declaration; the comparison converts once, here.
		if s.Max != 0 && (value < float64(s.Min)/1000 || value > float64(s.Max)/1000) {
			return fault("%v hors bornes [%v, %v]", value, float64(s.Min)/1000, float64(s.Max)/1000)
		}
	case OptionEnum:
		value, ok := single.Text(s.Key)
		if !ok {
			return fault("attendu : %s", s.Kind)
		}
		if len(s.Values) > 0 && !known(s.Values, value) {
			return []Fault{{
				Field:   field,
				Message: fmt.Sprintf("valeur inconnue %q", value),
				Values:  s.Values,
			}}
		}
	case OptionHostPort:
		value, ok := single.Text(s.Key)
		if !ok {
			return fault("attendu : %s", s.Kind)
		}
		if value == "" {
			return nil // an unused option, such as address when the transport is winspool
		}
		if err := checkHostPort(value); err != nil {
			return fault("%q n'est pas une adresse hôte:port valide (%s)", value, err)
		}
	case OptionURL:
		value, ok := single.Text(s.Key)
		if !ok {
			return fault("attendu : %s", s.Kind)
		}
		if value != "" && !isHTTPURL(value) {
			return fault("%q n'est pas une URL http ou https absolue", value)
		}
	case OptionGroup:
		nested, ok := single.Group(s.Key)
		if !ok {
			return fault("attendu : %s", s.Kind)
		}
		return validateOptions(field, nested, &DriverDescriptor{ID: s.Key, Options: s.Options})
	}
	return nil
}

// stabilityModes reports the two admissible values of stability.mode.
func stabilityModes() []string { return []string{ModeAdvisory, ModeBlocking} }

// timeoutActions reports the three admissible values of stability.on_timeout.
func timeoutActions() []string {
	return []string{OnTimeoutWarnAndPrint, OnTimeoutReject, OnTimeoutManualEntry}
}

// imageSources reports the three admissible values of catalog.images.source.
func imageSources() []string {
	return []string{ImageSourceCSV, ImageSourceDirectory, ImageSourceNone}
}

// intOption reads an option that must be a whole number of dots, and reports
// whether it was there and readable.
func intOption(options DriverOptions, key string) (int, bool) {
	value, ok := options.Int(key)
	return int(value), ok
}

// checkHostPort reports why an address is not a usable host:port.
func checkHostPort(address string) error {
	if address == "" {
		return fmt.Errorf("adresse vide")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("port %q hors bornes [1, 65535]", port)
	}
	// An empty host is legitimate: ":8085" listens on every interface, which is what
	// admin_on_lan describes.
	_ = host
	return nil
}

// isHTTPURL reports whether a value is an absolute http or https URL.
func isHTTPURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

// wellFormedColor reports whether a colour is spelled #RRGGBB.
func wellFormedColor(color string) bool {
	if len(color) != 7 || color[0] != '#' {
		return false
	}
	for i := 1; i < len(color); i++ {
		c := color[i]
		hex := c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
		if !hex {
			return false
		}
	}
	return true
}

// wellFormedArgon2id reports whether a hash is an argon2id PHC string.
//
// The shape is checked, never the cost: raising m, t or p is a legitimate
// hardening, and a validation that froze them would refuse a configuration that is
// SAFER than the one it was written against.
func wellFormedArgon2id(hash string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false
	}
	if !strings.HasPrefix(parts[2], "v=") {
		return false
	}
	for _, parameter := range []string{"m=", "t=", "p="} {
		if !strings.Contains(parts[3], parameter) {
			return false
		}
	}
	return isBase64Raw(parts[4], 8) && isBase64Raw(parts[5], 16)
}

// usableArgon2id reports whether a hash could have come out of argon2id at all.
//
// Being well formed is not enough, and the delivered configuration is the proof: its
// payload decoded to « for-the-delivered-configurationg », thirty-two bytes of typed
// text where argon2id writes thirty-two bytes drawn at random. What gives a placeholder
// away is therefore not its length but its ALPHABET — thirty-two random bytes are all
// printable ASCII once in 10^14, which is never.
//
// It is not this check that repairs the defect: emptying the field does. This is what
// stops the same gesture from coming back without a sound.
func usableArgon2id(hash string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 {
		return false
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return false
	}
	for _, b := range key {
		if b < 0x20 || b > 0x7e {
			return true
		}
	}
	return false
}

// isBase64Raw reports whether s is unpadded base64 of at least minimum characters.
func isBase64Raw(s string, minimum int) bool {
	if len(s) < minimum {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '+' || c == '/' || c == '-' || c == '_'
		if !ok {
			return false
		}
	}
	return true
}

// sortedKeys reports the keys of a map in a stable order, which is what makes both
// the canonical JSON and the sequence of faults reproducible.
func sortedKeys[V any](m map[string]V) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// --- Fingerprint ---------------------------------------------------------------

// CanonicalJSON returns the canonical JSON of a value: keys sorted, no whitespace,
// whole numbers in plain decimal.
//
// Canonical and not merely compact, and that is the point of §11.4: two
// configurations that are semantically identical but serialised with a different
// key order must NOT cut the serial port in the middle of a service. Whole numbers
// are re-emitted in decimal so that 9600 and 9.6e3 -- two spellings of the same
// baud rate -- cannot produce two fingerprints.
func CanonicalJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var generic any
	if err := decoder.Decode(&generic); err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	if err := writeCanonical(&buffer, generic); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// writeCanonical writes one decoded JSON value in canonical form.
func writeCanonical(buffer *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case nil:
		buffer.WriteString("null")
	case bool:
		if typed {
			buffer.WriteString("true")
		} else {
			buffer.WriteString("false")
		}
	case json.Number:
		buffer.WriteString(canonicalNumber(typed))
	case string:
		writeJSONString(buffer, typed)
	case []any:
		buffer.WriteByte('[')
		for i, item := range typed {
			if i > 0 {
				buffer.WriteByte(',')
			}
			if err := writeCanonical(buffer, item); err != nil {
				return err
			}
		}
		buffer.WriteByte(']')
	case map[string]any:
		buffer.WriteByte('{')
		for i, key := range sortedKeys(typed) {
			if i > 0 {
				buffer.WriteByte(',')
			}
			writeJSONString(buffer, key)
			buffer.WriteByte(':')
			if err := writeCanonical(buffer, typed[key]); err != nil {
				return err
			}
		}
		buffer.WriteByte('}')
	default:
		return fmt.Errorf("domain: valeur JSON non canonisable de type %T", value)
	}
	return nil
}

// writeJSONString writes one quoted JSON string.
//
// encoding/json cannot fail on a string -- invalid UTF-8 is replaced by U+FFFD, not
// rejected -- so there is no error to propagate, and no unreachable branch is left
// in a function the fingerprint of every configuration goes through.
func writeJSONString(buffer *bytes.Buffer, value string) {
	encoded, _ := json.Marshal(value)
	buffer.Write(encoded)
}

// exactIntegerFloat is 2^53, beyond which a float64 no longer holds every integer.
const exactIntegerFloat = 1 << 53

// canonicalNumber reports the one spelling of a JSON number.
//
// It exists so that 9600, 9.6e3 and 0.10 cannot produce three fingerprints of one
// configuration. The float64 detour is a canonicalisation of BYTES and never carries
// a quantity -- a mass, a price and a length are integers in this application, and
// the detour is refused past 2^53 rather than silently losing a digit.
func canonicalNumber(number json.Number) string {
	if whole, err := strconv.ParseInt(number.String(), 10, 64); err == nil {
		return strconv.FormatInt(whole, 10)
	}
	value, err := number.Float64()
	if err != nil {
		return number.String()
	}
	if value <= -exactIntegerFloat || value >= exactIntegerFloat {
		// Too big to be re-spelled without dropping a digit: the original wins.
		return number.String()
	}
	if value == float64(int64(value)) {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'g', -1, 64)
}

// BlockFingerprint reports the SHA-256 of the canonical JSON of one configuration
// block, as eight hexadecimal characters.
//
// It is what Station.Reload compares to decide whether a block REALLY changed
// (§11.4): a normalised comparison and not reflect.DeepEqual over raw JSON, so that
// a reformatted file does not close the serial port under a customer.
func BlockFingerprint(block any) string {
	canonical, err := CanonicalJSON(block)
	if err != nil {
		return strings.Repeat("?", fingerprintLength)
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])[:fingerprintLength]
}

// Fingerprint reports the eight characters the dashboard shows, so that "do the
// four stations display the same string?" is a check anybody can do by eye (§11.5).
//
// IT IS COMPUTED ON THE HARDWARE-FREE VIEW, Export(false), with modified_at and
// _readme cleared -- and it has to be, otherwise it could never do the one job it
// exists for: four stations of one homogeneous fleet differ by their number, their
// name, their COM port and their print queue, and each file was written at a
// different instant. What the figure compares is what MUST be identical: the price
// grid, the safeguards, the template, the categories, the retention.
func (c *Config) Fingerprint() string {
	subject := c.Export(false)
	subject.ModifiedAt, subject.Readme = time.Time{}, ""
	return BlockFingerprint(subject)
}

// Export returns a copy of the configuration fit to leave the station.
//
// With includeHardware false it drops station.number, station.name, scale.options,
// printer.options, catalog.options, network and the admin fingerprints: what is
// left is exactly what four stations of one fleet must share, and it is what
// "clone a station" copies (§11.5).
//
// TWO SECRETS NEVER LEAVE, whatever includeHardware says: the admin password, and
// the WebDAV password of the catalog. On import a station without a password runs
// the "first access" journey, which IMPOSES setting one -- an exported password
// would turn a fleet into four stations sharing one secret nobody chose.
//
// The result is NOT a loadable configuration: without a station number it fails
// control 1. It is meant to be MERGED into a target, field by field, with the diff
// preview of §11.5.
func (c *Config) Export(includeHardware bool) Config {
	out := *c
	out.retired = nil
	out.Admin.PasswordHash = ""

	out.Scale.Options = c.Scale.Options.clone()
	out.Printer.Options = c.Printer.Options.clone()
	out.Catalog.Options = c.Catalog.Options.clone()
	delete(out.Catalog.Options, "password")

	// Copies, so that a caller editing the export cannot reach into the
	// configuration the station is running on.
	out.Pricing.Tiers = append([]PriceTier(nil), c.Pricing.Tiers...)
	out.Pricing.SecondaryCodes = append([]string(nil), c.Pricing.SecondaryCodes...)
	out.Catalog.Categories = append([]Category(nil), c.Catalog.Categories...)

	if includeHardware {
		return out
	}
	out.Station.Number, out.Station.Name = 0, ""
	out.Network = NetworkConfig{}
	out.Scale.Options = nil
	out.Printer.Options = nil
	out.Catalog.Options = nil
	out.Admin.RecoveryCodeHash = ""
	return out
}

// Retired reports the dotted paths of the retired keys the file carried, in a
// stable order.
//
// It exists so that the administration screen can say « supprimez ces lignes »
// while pointing at the file, and so that a test can assert on the FILE rather than
// on a structure in which a retired key cannot exist.
func (c *Config) Retired() []string {
	return append([]string(nil), c.retired...)
}

// RetiredKeysError reports that a Config still carries a key control 20 refuses.
//
// It is what ConfigStore.Save returns instead of writing: the struct is about to be
// marshalled, and marshalling is what LAUNDERS the key -- encoding/json already
// dropped it once, at decode, and the field it stood for (a member's discount, for
// coef_num) goes with it. A caller that reaches Save without having checked first
// gets this instead of a file that decodes clean on the very next read.
type RetiredKeysError struct {
	// Keys are the dotted paths Config.Retired returned.
	Keys []string
}

// Error names the retired keys.
func (e *RetiredKeysError) Error() string {
	return fmt.Sprintf("domain: config still carries retired key(s): %s", strings.Join(e.Keys, ", "))
}

// RefuseIfRetired reports a *RetiredKeysError when the configuration still carries a
// key control 20 refuses, and nil otherwise.
//
// It is deliberately narrower than Validate: Validate needs Registries and can fail
// on a print queue this station does not have, which is not a reason to refuse
// WRITING a configuration that was already sitting on disk. This checks the one
// thing that must never reach a file regardless of everything else about it -- and
// it is cheap enough to run on every save, by every caller, including the ones that
// will never think to call Validate first (the recovery route does not: a rescue
// cannot be made to depend on the very validation that put the station out of
// service to begin with).
func (c *Config) RefuseIfRetired() error {
	if keys := c.Retired(); len(keys) > 0 {
		return &RetiredKeysError{Keys: keys}
	}
	return nil
}
