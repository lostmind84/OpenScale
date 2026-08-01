package web

import (
	"time"

	"openscale/internal/domain"
	"openscale/internal/station"
	"openscale/internal/station/ports"
)

// This file is cut 4 of §5.2, and it is the whole of it: NO type of internal/domain
// and NO type of internal/station is ever marshalled. Every field below is a plain
// Go type carrying an explicit JSON name.
//
// The reason is one real morning: a volunteer updates three stations out of four.
// The fourth serves an old bundle against a new service, and it has to keep working.
// So domain.Label.NetWeight may be renamed, Grams may grow a MarshalJSON, State may
// get a sixteenth value spelled differently — and none of it reaches a screen
// without passing through this file, where a golden test says so out loud.
//
// The naming convention is the one of the glossary and it is not negotiable: `_g`
// for masses, `_ms` for durations, `_cents` for amounts, `_count` for counts, and a
// `_text` twin wherever a value is also READ by a human, so that no front end ever
// re-implements the French decimal comma.

// timeLayout is how an instant travels: RFC 3339 in UTC, to the millisecond.
//
// To the millisecond and not to the nanosecond: a screen refreshes at 10 Hz, the
// extra digits are noise in every log line and in every golden file.
const timeLayout = "2006-01-02T15:04:05.000Z07:00"

// stamp renders an instant, and renders the zero instant as an empty string.
//
// Empty and not "0001-01-01T00:00:00Z": a front end tests a string for truth, and
// the year one is a value that looks like a date and means « never ».
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(timeLayout)
}

// millis renders a duration the way every duration travels: whole milliseconds.
func millis(d time.Duration) int64 { return d.Milliseconds() }

// stateDTO is the payload of the SSE event named "state", and the only thing the
// client screen ever reads about the station.
type stateDTO struct {
	Revision uint64 `json:"revision"`
	At       string `json:"at"`
	State    string `json:"state"`
	Station  int    `json:"station"`

	Weight weightDTO `json:"weight"`

	Product   *productDTO `json:"product"`
	Label     *labelDTO   `json:"label"`
	LastLabel *labelDTO   `json:"last_label"`
	Reprint   reprintDTO  `json:"reprint"`

	Message     *messageDTO     `json:"message"`
	Sound       string          `json:"sound"`
	Diagnostics []diagnosticDTO `json:"diagnostics"`
	FaultCode   string          `json:"fault_code"`
	// ArmingExpiresAt is when the bounded wait the station is in runs out, so the
	// screen can show it running out. Empty when nothing is being waited for.
	ArmingExpiresAt string `json:"arming_expires_at"`

	Scale    scaleDTO        `json:"scale"`
	Printer  printerDTO      `json:"printer"`
	Degraded *degradationDTO `json:"degraded"`

	// CatalogCount is how many tiles the grid holds. The tiles themselves come from
	// GET /api/v1/catalog, which the browser keeps: a snapshot at 10 Hz has no
	// business carrying 355 products.
	CatalogCount int `json:"catalog_count"`
	// PresentationDigest moves when, and only when, the screen settings served with the
	// catalog move. It rides next to CatalogCount because it answers the same question
	// -- « faut-il redemander le catalogue ? » -- for the half of that payload no count
	// can speak for. The browser COMPARES it and never reads it; presentationDigest in
	// catalog.go owns what goes into it and why.
	PresentationDigest string `json:"presentation_digest"`
	// UnloggedCount is the counter of ADR-013: labels that came out and could not be
	// journalled. A red light on the dashboard, never a refusal.
	UnloggedCount int64 `json:"unlogged_weighings_count"`
}

// weightDTO is what the top banner says about the plate.
type weightDTO struct {
	// Available distinguishes « no frame has ever arrived » from « the plate reads
	// zero ». Without it a station that has just started would show a weight of
	// 0 g it never measured.
	Available bool `json:"available"`
	// Expired is failure test 3 ter: the reading is older than the DERIVED expiry,
	// so the screen hides the weight and a weigh command is refused.
	Expired bool `json:"expired"`

	GrossG   int64 `json:"gross_g"`
	TareG    int64 `json:"tare_g"`
	NetG     int64 `json:"net_g"`
	Quantity int   `json:"quantity"`
	// NetText is the net mass in kilograms, French comma, three decimals: the string
	// the 96 px display shows and the one printed on the label.
	NetText   string `json:"net_text"`
	Stability string `json:"stability"`
	Latched   bool   `json:"latched"`
	Seq       int64  `json:"seq"`
	AgeMS     int64  `json:"age_ms"`
	ExpiryMS  int64  `json:"expiry_ms"`
}

// productDTO is the selected tile.
//
// It carries NO image address, and that is a decision rather than an omission: a
// snapshot goes out ten times a second, and resolving the address means asking the
// store for the DETECTED format of the photo — a disk read, on the path §4 promises is
// free of them. The browser already holds the whole catalog and joins on the product
// id, which is also how the grid transports a selection (§14.3).
type productDTO struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	CategoryCode string `json:"category_code"`
	Mode         string `json:"mode"`
	// UnitPriceCents is the CATALOG price, before any tier coefficient.
	UnitPriceCents int64  `json:"unit_price_cents"`
	UnitPriceText  string `json:"unit_price_text"`
	PriceSuffix    string `json:"price_suffix"`
}

// labelDTO is what a label carries, computed once by the single calculation path.
type labelDTO struct {
	JobID       string `json:"job_id"`
	Barcode     string `json:"barcode"`
	ProductID   string `json:"product_id"`
	ProductName string `json:"product_name"`
	Mode        string `json:"mode"`

	GrossG   int64  `json:"gross_g"`
	TareG    int64  `json:"tare_g"`
	NetG     int64  `json:"net_g"`
	NetText  string `json:"net_text"`
	Quantity int    `json:"quantity"`

	Prices []priceDTO `json:"prices"`
	// PrimaryCode is the tier printed LARGE, ReferenceCode the one encoded when the
	// payload carries a price. Codes and not copies: two front ends cannot then
	// disagree about which line is the big one.
	PrimaryCode   string `json:"primary_code"`
	ReferenceCode string `json:"reference_code"`
}

// priceDTO is one tier of one label.
type priceDTO struct {
	Code   string `json:"code"`
	Label  string `json:"label"`
	Abbrev string `json:"abbrev"`
	// UnitPriceCents is the DERIVED unit price, the one printed on the label.
	UnitPriceCents int64  `json:"unit_price_cents"`
	UnitPriceText  string `json:"unit_price_text"`
	AmountCents    int64  `json:"amount_cents"`
	AmountText     string `json:"amount_text"`
}

// reprintDTO drives the PERMANENT bottom bar (§8.5, §14.3).
type reprintDTO struct {
	Available bool   `json:"available"`
	JobID     string `json:"job_id"`
	PrintedAt string `json:"printed_at"`
}

// messageDTO is the banner line. Text is FRENCH and already interpolated: nothing
// downstream formats it again.
type messageDTO struct {
	Level     string `json:"level"`
	Code      string `json:"code"`
	Text      string `json:"text"`
	ExpiresAt string `json:"expires_at"`
}

// diagnosticDTO is one safeguard verdict. All of them travel: the administration
// screen displays every one, the machine acts on the first blocking one (§6.4).
type diagnosticDTO struct {
	Code      string `json:"code"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	Blocking  bool   `json:"blocking"`
	ProductID string `json:"product_id"`
}

// scaleDTO is what the station knows about its scale without asking it.
type scaleDTO struct {
	Connected    bool  `json:"connected"`
	MedianMS     int64 `json:"median_ms"`
	Observations int   `json:"observations_count"`
	// Provisional means the declared nominal cadence is standing in, because fewer
	// than eight intervals have been observed.
	Provisional bool `json:"provisional"`
	// TooSlow is the alert of failure test 3 bis: a weight would be considered
	// expired BEFORE the next measurement arrives.
	TooSlow bool `json:"too_slow"`
}

// printerDTO is the last thing the supervisor saw about the printer.
//
// Health is a WORD and not a number: a screen that renders « 2 » teaches nobody
// anything, and the numeric values of ports.PrinterHealth are free to move.
type printerDTO struct {
	Health      string `json:"health"`
	Detail      string `json:"detail"`
	PendingJobs int    `json:"pending_jobs_count"`
	ObservedAt  string `json:"observed_at"`
}

// degradationDTO is why the station is running in a fallback mode, and SINCE WHEN.
//
// The instant is the point of the type: « pourquoi ce poste est-il en saisie
// manuelle ce matin ? » is only decidable if the answer carries a date (§11.4).
type degradationDTO struct {
	Since  string `json:"since"`
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

// --- The conversion --------------------------------------------------------

// stateOf converts one snapshot into the payload the screen reads.
//
// It is total: every field of station.Snapshot is either carried or deliberately
// dropped, and the golden test of this file is what keeps that true.
func (s *Server) stateOf(snap station.Snapshot) stateDTO {
	out := stateDTO{
		Revision:        snap.Revision,
		At:              stamp(snap.At),
		State:           snap.State.String(),
		Station:         snap.Station,
		Weight:          weightOf(snap),
		Product:         productOf(snap.Product),
		Label:           labelOf(snap.Label),
		LastLabel:       labelOf(snap.LastLabel),
		Reprint:         reprintOf(snap),
		Message:         messageOf(snap.Message),
		Sound:           snap.Sound,
		Diagnostics:     diagnosticsOf(snap.Diagnostics),
		FaultCode:       snap.FaultCode,
		ArmingExpiresAt: stamp(snap.ArmingExpiresAt),
		Scale:           scaleOf(snap.Scale),
		Printer:         printerOf(snap.Printer),
		Degraded:        degradationOf(snap.Degraded),
		CatalogCount:    snap.Catalog.WeighableCount(),
		UnloggedCount:   snap.UnloggedWeighings,
		// Read from the Hub and not from the snapshot: the presentation is a
		// configuration that reloads hot (§11.4), and a snapshot describes a plate, a
		// product and a printer -- never a screen setting.
		PresentationDigest: presentationDigest(presentationOf(s.hub.Config().UI)),
	}
	return out
}

// weightOf converts the plate.
func weightOf(snap station.Snapshot) weightDTO {
	return weightDTO{
		Available: snap.HasWeight,
		Expired:   snap.Expired,
		GrossG:    int64(snap.Weight.Gross),
		TareG:     int64(snap.Weight.Tare),
		NetG:      int64(snap.Weight.Net),
		Quantity:  snap.Weight.Quantity,
		NetText:   snap.Weight.Net.Kilos(),
		Stability: snap.Weight.Stability.String(),
		Latched:   snap.Weight.Latched,
		Seq:       snap.Weight.Seq,
		AgeMS:     millis(snap.Weight.Age),
		ExpiryMS:  millis(snap.Weight.Expiry),
	}
}

// productOf converts the selected tile.
func productOf(p *domain.Product) *productDTO {
	if p == nil {
		return nil
	}
	return &productDTO{
		ID: p.ID, Name: p.Name, CategoryCode: p.CategoryCode,
		Mode:           p.Mode.String(),
		UnitPriceCents: int64(p.UnitPrice),
		UnitPriceText:  p.UnitPrice.Euro(),
		PriceSuffix:    p.PriceSuffix,
	}
}

// labelOf converts a label and its price lines.
func labelOf(l *domain.Label) *labelDTO {
	if l == nil {
		return nil
	}
	out := &labelDTO{
		JobID: l.JobID, Barcode: string(l.Barcode),
		ProductID: l.Product.ID, ProductName: l.Product.Name,
		Mode:     l.Mode.String(),
		GrossG:   int64(l.GrossWeight),
		TareG:    int64(l.Tare),
		NetG:     int64(l.NetWeight),
		NetText:  l.NetWeight.Kilos(),
		Quantity: l.Quantity,
		Prices:   make([]priceDTO, 0, len(l.Lines)),
	}
	for _, line := range l.Lines {
		out.Prices = append(out.Prices, priceDTO{
			Code: line.Tier.Code, Label: line.Tier.Label, Abbrev: line.Tier.Abbrev,
			UnitPriceCents: int64(line.UnitPrice), UnitPriceText: line.UnitPrice.Euro(),
			AmountCents: int64(line.Amount), AmountText: line.Amount.Euro(),
		})
	}
	if l.PrimaryLine != nil {
		out.PrimaryCode = l.PrimaryLine.Tier.Code
	}
	if l.ReferenceLine != nil {
		out.ReferenceCode = l.ReferenceLine.Tier.Code
	}
	return out
}

// reprintOf converts the state of the permanent bottom bar.
func reprintOf(snap station.Snapshot) reprintDTO {
	out := reprintDTO{Available: snap.ReprintAvailable, PrintedAt: stamp(snap.LastPrintedAt)}
	if snap.LastLabel != nil {
		out.JobID = snap.LastLabel.JobID
	}
	return out
}

// messageOf converts the banner.
func messageOf(m *station.Message) *messageDTO {
	if m == nil {
		return nil
	}
	return &messageDTO{
		Level: m.Level, Code: m.Code, Text: m.Text, ExpiresAt: stamp(m.ExpiresAt),
	}
}

// diagnosticsOf converts what the safeguards said, in evaluation order.
func diagnosticsOf(in []domain.Diagnostic) []diagnosticDTO {
	out := make([]diagnosticDTO, 0, len(in))
	for _, d := range in {
		out = append(out, diagnosticDTO{
			Code: d.Code, Severity: d.Severity.String(), Message: d.Message,
			Blocking: d.Blocks(), ProductID: d.ProductID,
		})
	}
	return out
}

// scaleOf converts the health of the scale.
func scaleOf(h station.ScaleHealth) scaleDTO {
	return scaleDTO{
		Connected: h.Connected, MedianMS: millis(h.Median),
		Observations: h.Observations, Provisional: h.Provisional, TooSlow: h.TooSlow,
	}
}

// printerOf converts the health of the printer.
func printerOf(h station.PrinterHealth) printerDTO {
	return printerDTO{
		Health: printerHealthName(h.Health), Detail: h.Detail,
		PendingJobs: h.PendingJobs, ObservedAt: stamp(h.ObservedAt),
	}
}

// printerHealthName spells one printer health the way the screen and the journal
// spell it.
//
// « unknown » is the HONEST answer of a one-way transport and not a failure: a
// devfile or a Windows queue in RAW hands the bytes over and never hears back.
func printerHealthName(h ports.PrinterHealth) string {
	switch h {
	case ports.PrinterReady:
		return "ready"
	case ports.PrinterConsumable:
		return "consumable"
	case ports.PrinterFaulted:
		return "faulted"
	case ports.PrinterUnknown:
		return "unknown"
	}
	return "unknown"
}

// degradationOf converts the fallback state, which is nil on a nominal station.
func degradationOf(d *station.Degradation) *degradationDTO {
	if d == nil {
		return nil
	}
	return &degradationDTO{Since: stamp(d.Since), Code: d.Code, Reason: d.Reason}
}
