package station

import (
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// Message is the banner line a customer or a volunteer reads. It is FRENCH and
// already interpolated: nothing downstream formats it again.
type Message struct {
	Level string
	Code  string
	Text  string
	// ExpiresAt is zero when nothing but a state change removes the message. That
	// is the case of the scale loss: a station with no weight does not stop having
	// no weight because five seconds went by.
	ExpiresAt time.Time
}

// Weight is what the top banner says about the plate.
//
// Age is CARRIED but deliberately left out of the change detection of
// sameContentAs: it grows on every tick, and comparing it would make the station
// publish at 10 Hz for ever with nothing to say. What matters — Expired — is a
// boolean and it IS compared, and the screen has its own watchdog on the instant
// the snapshot was built (§14.3).
type Weight struct {
	Gross     domain.Grams
	Tare      domain.Grams
	Net       domain.Grams
	Quantity  int
	Stability domain.Stability
	// Latched reports that the anchor held within tolerance for min_duration_ms.
	// It is an INDICATOR, not a print condition in advisory mode (A3).
	Latched bool
	Seq     int64
	// Age is Now - Measurement.Timestamp, COMPUTED by the Hub (bloquant-1).
	Age time.Duration
	// Expiry is DERIVED from the observed cadence, never a constant (A3).
	Expiry time.Duration
}

// ScaleHealth is what the station knows about its scale, without asking it.
type ScaleHealth struct {
	Connected bool
	// Median is the cadence actually observed between two frames.
	Median time.Duration
	// Observations is how many intervals the rate meter holds, so that the admin
	// screen can say « médiane sur 64 mesures » or « provisoire ».
	Observations int
	// Provisional means fewer than eight intervals are known and the driver's
	// declared nominal rate is standing in.
	Provisional bool
	// TooSlow is the single alert condition of failure test 3 bis: expiry_factor ×
	// median exceeds the ceiling, so a weight is considered expired BEFORE the next
	// measurement arrives.
	TooSlow bool
}

// PrinterHealth is the last thing the supervisor saw about the printer.
//
// It is observed OUT of the Hub goroutine, because ports.Printer.Status talks to a
// device and a device may hang: the loop that must answer a customer in under a
// millisecond never waits on one.
type PrinterHealth struct {
	Health ports.PrinterHealth
	// Detail is FRENCH: it is read by a volunteer on the troubleshooting screen.
	Detail      string
	PendingJobs int
	// ObservedAt is zero until the supervisor has managed to ask once.
	ObservedAt time.Time
}

// Degradation is why the station is running in a fallback mode, and since when.
//
// The instant is the point of the type. « Pourquoi ce poste est-il en saisie
// manuelle ce matin ? » is the question a volunteer actually asks, and it is only
// decidable if the answer carries a date (§11.4).
type Degradation struct {
	Since  time.Time
	Code   string
	Reason string
}

// Snapshot is the whole state of the station at one instant, frozen.
//
// It is built, published and never written again: the pointers it carries — the
// product, the label, the catalog — address values that are themselves frozen at
// construction. That is what lets one snapshot be read by eight SSE handlers while
// the Hub is already building the next one, with no lock anywhere.
type Snapshot struct {
	// Revision changes when, and only when, the CONTENT changes. It is what
	// publish compares to decide whether there is anything to say.
	Revision uint64
	// At is the instant the snapshot was built, on the injected clock.
	At    time.Time
	State domain.State

	Weight Weight
	// HasWeight distinguishes « no frame has ever arrived » from « the plate reads
	// zero ». Without it a station that has just started would publish an expired
	// weight of 0 g, and the screen would hide a weight it never had.
	HasWeight bool
	// Expired is the flag failure test 3 ter asserts: the weight is older than the
	// derived expiry, so the screen hides it and a weigh command is refused.
	Expired bool

	Product   *domain.Product
	Tare      domain.Grams
	Units     int
	Label     *domain.Label
	LastLabel *domain.Label
	// LastPrintedAt and ReprintAvailable drive the PERMANENT bottom bar (§8.5).
	LastPrintedAt    time.Time
	ReprintAvailable bool

	Message     *Message
	Sound       string
	Diagnostics []domain.Diagnostic
	FaultCode   string
	// ArmingExpiresAt is when the bounded wait the station is in runs out, so the
	// screen can show it running out. Zero when nothing is being waited for.
	ArmingExpiresAt time.Time

	Catalog *domain.Catalog
	Scale   ScaleHealth
	Printer PrinterHealth
	// Degraded is nil on a nominal station.
	Degraded *Degradation

	Station int
	// UnloggedWeighings is the counter of ADR-013: labels that came out and could
	// not be journalled. It is a red light on the dashboard and never a refusal.
	UnloggedWeighings int64
}

// sameContentAs reports whether two snapshots say the same thing to a screen.
//
// Revision and At are excluded by construction: the first is what this function
// decides and the second changes on every tick. Weight.Age is excluded for the
// reason given on the field. EVERYTHING ELSE MUST BE COMPARED — a field added to
// Snapshot and forgotten here is a change that would never be published, so the
// test of this function walks the fields one by one.
func (s Snapshot) sameContentAs(other Snapshot) bool {
	if s.State != other.State || s.HasWeight != other.HasWeight || s.Expired != other.Expired {
		return false
	}
	if s.Weight.Gross != other.Weight.Gross || s.Weight.Tare != other.Weight.Tare ||
		s.Weight.Net != other.Weight.Net || s.Weight.Quantity != other.Weight.Quantity ||
		s.Weight.Stability != other.Weight.Stability || s.Weight.Latched != other.Weight.Latched ||
		s.Weight.Seq != other.Weight.Seq || s.Weight.Expiry != other.Weight.Expiry {
		return false
	}
	if s.Product != other.Product || s.Label != other.Label || s.LastLabel != other.LastLabel {
		return false
	}
	if s.Tare != other.Tare || s.Units != other.Units {
		return false
	}
	if !s.LastPrintedAt.Equal(other.LastPrintedAt) || s.ReprintAvailable != other.ReprintAvailable {
		return false
	}
	if !sameMessage(s.Message, other.Message) || s.Sound != other.Sound {
		return false
	}
	if !sameDiagnostics(s.Diagnostics, other.Diagnostics) || s.FaultCode != other.FaultCode {
		return false
	}
	if !s.ArmingExpiresAt.Equal(other.ArmingExpiresAt) {
		return false
	}
	if s.Catalog != other.Catalog || s.Station != other.Station {
		return false
	}
	if s.Scale != other.Scale || s.Printer != other.Printer {
		return false
	}
	if !sameDegradation(s.Degraded, other.Degraded) {
		return false
	}
	return s.UnloggedWeighings == other.UnloggedWeighings
}

// sameMessage compares two banner messages, either of which may be absent.
func sameMessage(a, b *Message) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	}
	return a.Level == b.Level && a.Code == b.Code && a.Text == b.Text &&
		a.ExpiresAt.Equal(b.ExpiresAt)
}

// sameDegradation compares two fallback states, either of which may be absent.
func sameDegradation(a, b *Degradation) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	}
	return a.Code == b.Code && a.Reason == b.Reason && a.Since.Equal(b.Since)
}

// sameDiagnostics compares what the safeguards said, in order.
//
// The code and the severity are enough: the message is derived from the code by a
// pure function, so two diagnostics with the same code and severity cannot show
// different text.
func sameDiagnostics(a, b []domain.Diagnostic) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Code != b[i].Code || a[i].Severity != b[i].Severity {
			return false
		}
	}
	return true
}
