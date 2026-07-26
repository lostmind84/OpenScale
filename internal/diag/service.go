package diag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"openscale/internal/station/ports"
)

// ServiceProbe is the RUNNING station, asked over HTTP.
//
// Three of the fifteen controls have no other honest source. « File d'impression visible
// depuis le contexte du service », « cadence balance observée » and « répertoire
// catalogue accessible tel que le service le voit » are questions about the SERVICE's
// rights and the SERVICE's observations; a doctor run by an administrator has the
// administrator's rights and the administrator's HKCU, so answering them locally would
// answer a different question and would answer it wrong (important-11, §15.4).
//
// Declared HERE, on the consumer's side: HTTPProbe satisfies it, and a test drives the
// three controls with a double that opens no socket.
type ServiceProbe interface {
	// Health reads GET /admin/api/health — unauthenticated on purpose (ADR-018).
	Health(ctx context.Context) (Health, error)
	// Liveness reads GET /healthz — VIVACITY ONLY (§14.5): it says the Hub answered an
	// event, and NOTHING about the devices. It is used here to tell « the address is
	// held by us » from « the address is held by something else », and for nothing else.
	Liveness(ctx context.Context) (Liveness, error)
}

// Health is what GET /admin/api/health answered.
//
// The field names are the JSON contract of §14.5, not internal/web's Go types: the
// station this doctor interrogates may be running a DIFFERENT build, which is exactly
// the case §14.5 designs the DTO decoupling for — « un bénévole met à jour 3 postes sur
// 4 ». Everything is therefore optional and an absent field is reported as absent.
type Health struct {
	// Raw is the body exactly as it was served. diagnostic.zip carries it verbatim: a
	// field this build does not know about is still a field the support call may need.
	Raw []byte `json:"-"`

	Version     string `json:"version"`
	Fingerprint string `json:"config_fingerprint"`
	Station     int    `json:"station"`
	StationName string `json:"station_name"`
	Coop        string `json:"coop"`
	Alive       bool   `json:"alive"`

	State    healthState    `json:"state"`
	Counters healthCounters `json:"counters"`
	// Catalog is the one-line inventory of the LAST import, as §14.4 reads it out loud.
	// Nil means the station has never applied one, which is a different sentence from
	// « the last one was refused ».
	Catalog *HealthImport `json:"catalog"`
}

// HealthImport is the inventory of one import as the dashboard publishes it (§14.4).
//
// It is deliberately NOT « 46 produits en erreur »: a prepackaged boulgour is not an
// error, it is not the scale's business, and the only figure that deserves the eye is the
// one somebody can fix.
type HealthImport struct {
	OccurredAt string `json:"occurred_at"`
	Source     string `json:"source"`
	FileName   string `json:"file_name"`
	// Result is applied, unchanged, rejected or failed (§10.5).
	Result string `json:"result"`
	Code   string `json:"code"`
	Reason string `json:"reason"`

	RowsRead     int `json:"rows_read_count"`
	Weighable    int `json:"weighable_count"`
	NotWeighable int `json:"not_weighable_count"`
	Anomalies    int `json:"anomalies_count"`
}

// healthState is the part of the station snapshot the controls read.
type healthState struct {
	State        string        `json:"state"`
	CatalogCount int           `json:"catalog_count"`
	Scale        healthScale   `json:"scale"`
	Printer      healthPrinter `json:"printer"`
}

// healthScale is the observed cadence of A3, as the station measured it — not as
// anybody declared it.
type healthScale struct {
	Connected    bool  `json:"connected"`
	MedianMS     int64 `json:"median_ms"`
	Observations int   `json:"observations_count"`
	// Provisional means fewer than eight intervals have been observed and the driver's
	// declared nominal rate is standing in. A provisional figure must never be presented
	// as a measurement.
	Provisional bool `json:"provisional"`
	// TooSlow is the SINGLE alert condition of §6.5, computed by the station itself:
	// expiry_factor × median exceeds the ceiling. It is read and never recomputed here —
	// two implementations of one rule is how the two of them come to disagree.
	TooSlow bool `json:"too_slow"`
}

// healthPrinter is what the supervisor last saw about the printer, from the SERVICE's
// context — which is the whole point of the print queue control.
type healthPrinter struct {
	Health      string `json:"health"`
	Detail      string `json:"detail"`
	PendingJobs int    `json:"pending_jobs_count"`
	ObservedAt  string `json:"observed_at"`
}

// healthCounters is what the station counts about itself.
type healthCounters struct {
	Unlogged int64 `json:"unlogged_weighings_count"`
	Journal  int   `json:"journal_rows_count"`
}

// Liveness is what GET /healthz answered. It carries the budget so that the probe can
// tell OUR answer from any other program that happens to hold the port and reply JSON.
type Liveness struct {
	Alive     bool  `json:"alive"`
	BudgetMS  int64 `json:"budget_ms"`
	ElapsedMS int64 `json:"elapsed_ms"`
}

// IsOpenScale reports that the thing answering on this address is an OpenScale station.
//
// The criterion is the BUDGET and not the alive flag: a station in « configuration
// d'usine » answers, one wedged mid-shutdown may answer false, and both are us. A
// non-zero budget in a body that decoded is the signature of the route of §14.5.
func (l Liveness) IsOpenScale() bool { return l.BudgetMS > 0 }

// ErrServiceSilent reports that the station's HTTP layer did not answer.
//
// It is a NAMED error and not a formatted string because three controls branch on it,
// and each of them turns it into StatusUnknown with the same remedy: start the service,
// then run the command again.
var ErrServiceSilent = errors.New("diag: le service ne répond pas")

// HTTPProbe asks a running station over HTTP.
type HTTPProbe struct {
	// BaseURL is « http://host:port », derived from network.listen. It is a field and
	// not a constant because a station may listen elsewhere, and because a support call
	// sometimes asks the neighbouring station.
	BaseURL string
	Client  *http.Client
	Clock   ports.Clock
}

// NewHTTPProbe builds the probe for the address a configuration declares.
//
// address is a host:port as network.listen spells it. A bare port or an empty host is
// completed with 127.0.0.1, because that is what the shipped configuration listens on
// and because « :8080 » is not a URL.
func NewHTTPProbe(address string, clk ports.Clock, client *http.Client) *HTTPProbe {
	if client == nil {
		client = &http.Client{}
	}
	return &HTTPProbe{BaseURL: "http://" + loopbackHost(address), Client: client, Clock: clk}
}

// loopbackHost completes an address the way a browser on the station would.
func loopbackHost(address string) string {
	if address == "" {
		return "127.0.0.1"
	}
	if strings.HasPrefix(address, ":") {
		return "127.0.0.1" + address
	}
	// « 0.0.0.0 » and « [::] » are what a station listening on every interface declares.
	// They are addresses to BIND, never addresses to dial.
	for _, wildcard := range []string{"0.0.0.0:", "[::]:"} {
		if rest, found := strings.CutPrefix(address, wildcard); found {
			return "127.0.0.1:" + rest
		}
	}
	return address
}

// Health reads GET /admin/api/health.
func (p *HTTPProbe) Health(ctx context.Context) (Health, error) {
	body, err := p.get(ctx, "/admin/api/health")
	if err != nil {
		return Health{}, err
	}
	out := Health{Raw: body}
	if err := json.Unmarshal(body, &out); err != nil {
		return Health{Raw: body}, fmt.Errorf("réponse de /admin/api/health illisible : %w", err)
	}
	return out, nil
}

// Liveness reads GET /healthz.
//
// A 503 is NOT an error here: /healthz answers 503 with a body when the Hub missed its
// budget, and that body is the answer. Only a transport failure is an error.
func (p *HTTPProbe) Liveness(ctx context.Context) (Liveness, error) {
	body, err := p.get(ctx, "/healthz")
	if err != nil {
		return Liveness{}, err
	}
	var out Liveness
	if err := json.Unmarshal(body, &out); err != nil {
		return Liveness{}, fmt.Errorf("réponse de /healthz illisible : %w", err)
	}
	return out, nil
}

// probeBudget is what one question to a running station is given.
//
// It is spent on the INJECTED clock, so a test never waits for it. Two seconds: the
// routes it calls read a published snapshot and, for /healthz, submit one event with a
// 500 ms budget of its own — a station that needs longer than two seconds to answer them
// has a problem the other fourteen controls will name.
const probeBudget = 2 * time.Second

// maxHealthBytes bounds the body a probe will read.
//
// The dashboard payload is a few kilobytes. The bound is not paranoia about our own
// service: this probe dials an address that may be held by SOMETHING ELSE, and an
// unbounded read from an unknown program is an unbounded allocation.
const maxHealthBytes = 1 << 20

// get performs one bounded GET and hands back the body.
func (p *HTTPProbe) get(ctx context.Context, path string) ([]byte, error) {
	if p.Clock != nil {
		var cancel context.CancelFunc
		ctx, cancel = ports.WithBudget(ctx, p.Clock, probeBudget)
		defer cancel()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("%w : %s est injoignable (%v)", ErrServiceSilent, p.BaseURL+path, err)
	}
	response, err := p.Client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w : %s est injoignable (%v)", ErrServiceSilent, p.BaseURL+path, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxHealthBytes))
	if err != nil {
		return nil, fmt.Errorf("%w : réponse tronquée de %s (%v)", ErrServiceSilent, path, err)
	}
	return body, nil
}
