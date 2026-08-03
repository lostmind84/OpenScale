// This file holds WHAT A CONFIGURATION LOOKS LIKE FROM THE OUTSIDE: the DTO the
// administration screen edits, and the read that serves it.
//
// Reading is open and writing is not (ADR-033), and the reason is here: configPayload
// REDACTS both hashes before the payload leaves, so there is nothing on this route a
// password would be keeping.

package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"openscale/internal/domain"
	"time"
)

// faultDTO is one configuration control that failed (§11.3).
//
// ALL of them come back at once, and that is the whole design of the validation: a
// screen that fixes one fault, saves, and discovers the second is a screen somebody
// gives up on.
type faultDTO struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	// Allowed lists the values that WOULD work, when the control knows them: the
	// queues that exist, the ports that answer, the templates this binary carries.
	Allowed []string `json:"allowed,omitempty"`
}

// ConfigVersion is one restorable version of the file (§11.4, five of them).
type ConfigVersion struct {
	// Version is 1 for the most recent backup, up to 5 for the oldest.
	Version int
	// ModifiedAt is when that version was in force.
	ModifiedAt time.Time
	// Fingerprint is the eight-character digest the administration screen shows.
	Fingerprint string
}

// configVersionDTO is one restorable version, as the screen reads it.
type configVersionDTO struct {
	Version     int    `json:"version"`
	ModifiedAt  string `json:"modified_at"`
	Fingerprint string `json:"config_fingerprint"`
}

// catalogPasswordOption is the credential a WebDAV share asks for.
//
// Spelled here rather than imported from internal/catalog/webdav: this layer must not
// depend on a driver to know that a password is a secret. The name is the one control 39
// of the domain already refuses on local_drop, and the one the webdav descriptor
// declares — three spellings of a key that has never had a second one.
const catalogPasswordOption = "password"

// configDTO is GET /admin/api/config.
//
// The configuration travels AS THE FILE, because that is what it is: the screen edits
// a document and saves it back. What does NOT travel is the two secrets — a hash is
// still a credential, and nothing on a screen has any use for one.
type configDTO struct {
	Config      json.RawMessage `json:"config"`
	Fingerprint string          `json:"config_fingerprint"`
	// Retired names the keys this file still carries and this binary refuses (§11.3,
	// control 20), so that the screen can offer to drop them.
	Retired []string `json:"retired_keys"`
	// Unreadable names the blocks of the file that did NOT decode, with the reason. The
	// values shown for those blocks are the factory ones, and a screen that did not say so
	// would invite a volunteer to save them over the shop's own.
	Unreadable []faultDTO `json:"unreadable_blocks,omitempty"`
	// Pending is the confirmation still expected, when there is one.
	Pending *confirmationDTO `json:"pending_confirmation"`
}

// confirmationDTO is the 60-second countdown of §11.4.
type confirmationDTO struct {
	// Changed names the blocks that actually moved.
	Changed []string `json:"changed_blocks"`
	// ConfirmBefore is when the station goes back to the previous version on its own.
	ConfirmBefore string `json:"confirm_before"`
	SecondsLeft   int    `json:"seconds_left"`
}

// readConfig is GET /admin/api/config.
//
// It serves the FILE and not the configuration in force, and on one station that is the
// difference between repairing it and destroying it: a station that started out of
// service runs the neutral profile, so a screen fed from memory would show a volunteer
// the factory tariffs, the factory safeguards and the factory categories — and the save
// that followed would write them over the cooperative's own (§11.3, §11.4).
//
// A file that cannot be read AT ALL falls back on what is running, which is all a station
// with no readable file has left to show.
//
// A file only PART of which decoded is served as it was READ — the shop's own blocks, not
// what memory holds — with the substituted ones named in `unreadable_blocks`. Serving them
// with nothing to distinguish them would put the factory tariffs on the screen under the
// shop's station name, and the save that followed would write them back: the very
// « détruire » of the paragraph above, arrived by another road.
//
// The field is SERVED, and no screen reads it yet: the banner that would show it is
// declared not done in SUIVI.md, and `openscale doctor` is what names the block in the
// meantime. Said plainly here because a comment that implied otherwise would be the only
// thing standing between a reader and the belief that this case is handled end to end.
func (s *Server) readConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.hub.Config()
	var substituted []faultDTO
	if s.configStore != nil {
		onDisk, err := s.configStore.Read(r.Context())
		var unreadable *domain.UnreadableBlocksError
		switch {
		case err == nil:
			cfg = onDisk
		case errors.As(err, &unreadable):
			cfg = unreadable.Config
			substituted = faultsOf(unreadable.Faults)
		}
	}
	payload := s.configPayload(cfg, nil)
	payload.Unreadable = substituted
	writeJSON(w, http.StatusOK, payload)
}

// configPayload renders one configuration without its secrets.
func (s *Server) configPayload(cfg domain.Config, pending *confirmationDTO) configDTO {
	redacted := cfg
	// A hash is a credential. It is not exported, not displayed and not sent to a
	// screen: §11.2 says so of the export, and a GET is an export with fewer steps.
	redacted.Admin.PasswordHash = ""
	redacted.Admin.RecoveryCodeHash = ""

	// The catalog password is a credential like those two, and this route asks for no
	// password of its own (ADR-033): the pages of settings open in READ, deliberately.
	// Anything served here is therefore readable by whoever reaches the station, and a
	// producer's WebDAV account is not something a shop chose to publish.
	//
	// BLANKED and not removed, exactly like the two hashes above: this document is what the
	// screen edits and saves back, so a key deleted here would come back as a `catalog`
	// block that moved and nobody touched. A source that carries no password keeps none.
	if redacted.Catalog.Options.Has(catalogPasswordOption) {
		redacted.Catalog.Options = redacted.Catalog.Options.WithText(catalogPasswordOption, "")
	}

	// The three option maps are deliberately left as they are, `null` included. Turning
	// them into `{}` here looked like the same repair as `retired_keys` and is not: this
	// document is what the screen EDITS AND SAVES BACK, so an empty map written where the
	// file had none is a block that moved — the station then asked for a sixty-second
	// confirmation on a `scale` block nobody had touched. The screen reads them by path
	// and answers `undefined` for a missing one, which is what a screen should do.
	raw, err := json.Marshal(redacted)
	if err != nil {
		raw = []byte(`{}`)
	}
	return configDTO{
		Config: raw, Fingerprint: cfg.Fingerprint(),
		Retired: retiredOrEmpty(cfg.Retired()), Pending: pending,
	}
}

// retiredOrEmpty returns a list that marshals to `[]` and never to `null`.
//
// This is the defect that made the administration unreachable: `Config.Retired()` returns
// nil when the file carries no retired key — which is the nominal case — the field went
// out as `null`, and `draft.retired.length` threw on the very first render after a
// successful login. The ERR-UI-01 net then showed a sentence with no detail and reloaded
// the page, so the operator read it as a password that would not work.
func retiredOrEmpty(keys []string) []string {
	if keys == nil {
		return []string{}
	}
	return keys
}

// retiredFaultsOf reports what control 20 says about each retired key a configuration
// still carries, or nothing when it carries none.
//
// It runs the FULL Validate and keeps only the faults named by Retired(), rather than
// duplicating the wording of retiredKeys here: a volunteer then reads the exact same
// sentence whether the key arrived on an upload or was already sitting in the file
// (§11.3, control 20).
func retiredFaultsOf(cfg domain.Config, reg domain.Registries) []faultDTO {
	paths := cfg.Retired()
	if len(paths) == 0 {
		return nil
	}
	named := make(map[string]bool, len(paths))
	for _, path := range paths {
		named[path] = true
	}
	var out []faultDTO
	for _, fault := range (&cfg).Validate(reg) {
		if named[fault.Field] {
			out = append(out, faultDTO{Field: fault.Field, Message: fault.Message})
		}
	}
	return out
}

// faultsOf converts what the 47 controls said.
func faultsOf(faults []domain.Fault) []faultDTO {
	out := make([]faultDTO, 0, len(faults))
	for _, f := range faults {
		out = append(out, faultDTO{Field: f.Field, Message: f.Message, Allowed: f.Values})
	}
	return out
}
