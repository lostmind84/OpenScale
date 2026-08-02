package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station"
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

// writeConfig is PUT /admin/api/config — the five steps of §11.4, in order.
//
//  1. json.Unmarshal            → 400 when it is not a document
//  2. Config.Validate           → 422 with EVERY fault at once
//  3. rotation and atomic write → the ConfigStore's business
//  4. Station.Reload            → restarts only the blocks that moved
//  5. hardware or listen moved  → the 60-second countdown, with automatic rollback
//
// # Why the secrets come back from the configuration in force
//
// The screen never receives them, so it cannot send them back. Taking them from the
// running configuration is what lets a save of the edited document leave the password
// alone instead of erasing it.
func (s *Server) writeConfig(w http.ResponseWriter, r *http.Request) {
	if s.configStore == nil || s.controller == nil {
		unavailable(w, "la configuration n'est pas modifiable ici")
		return
	}

	// A second save INSIDE the window is refused, exactly as a confirmation outside it is.
	// The write of step 3 happens before the countdown of step 5, so accepting one would
	// move the file the rollback aims at onto a version nobody confirmed either — and the
	// one somebody really did validate would be the version lost.
	if deadline := s.controller.PendingConfirmation(); !deadline.IsZero() {
		writeProblem(w, http.StatusConflict, "",
			"Une configuration attend encore d'être confirmée. Confirmez-la, ou laissez le "+
				"poste revenir tout seul à la version précédente, puis enregistrez de nouveau.")
		return
	}

	// The document GET serves is the DECODED Go structure, re-marshalled: a retired key
	// never survives that round trip, because encoding/json drops what no field claims
	// (§11.3, `configPayload`). A PUT of exactly what GET served can therefore never
	// re-declare `coef_num` or `coef_den`, and control 20 on the SUBMITTED document --
	// `next`, below -- finds nothing to refuse: the save would silently rewrite the file
	// with MEMBER at 0 %. What is asked here is the FILE itself, read the same way
	// `readConfig` reads it, which still carries whatever nobody repaired. Refusing the
	// write is the same reasoning control 20 already applies to an upload that names a
	// retired key outright, extended to a key already sitting on disk (ADR-034):
	// repairing the file is done IN the file, not by laundering it through this screen.
	//
	// A file only PART of which decoded still answers both questions this block asks: its
	// READ blocks are the shop's own, and a retired key sitting in one of them is still
	// there. Treating it as « unreadable » skipped the guard entirely, and skipped
	// `served` below with it — so the catalog password was taken from the neutral profile,
	// which has none, and a save about the polling interval erased a producer's account.
	onDisk, onDiskErr := s.configStore.Read(r.Context())
	var unreadable *domain.UnreadableBlocksError
	if errors.As(onDiskErr, &unreadable) {
		onDisk, onDiskErr = unreadable.Config, nil
	}
	if onDiskErr == nil {
		if faults := retiredFaultsOf(onDisk, s.registries); len(faults) > 0 {
			writeJSON(w, http.StatusUnprocessableEntity, problem{
				Code: "ERR-CFG-01",
				Message: "Le fichier de configuration en service porte encore une clé que ce " +
					"binaire refuse. L'administration ne peut pas la corriger : changez-la " +
					"dans le fichier de configuration lui-même.",
				Faults: faults,
			})
			return
		}
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "", "Requête illisible : "+err.Error())
		return
	}
	var next domain.Config
	if err := json.Unmarshal(raw, &next); err != nil {
		writeProblem(w, http.StatusBadRequest, "", "Configuration illisible : "+err.Error())
		return
	}

	current := s.hub.Config()
	next.Admin.PasswordHash = current.Admin.PasswordHash
	next.Admin.RecoveryCodeHash = current.Admin.RecoveryCodeHash
	next.ModifiedAt = s.clock.Now()

	// served is the document the screen was GIVEN and edited, which is what a submission
	// has to be compared against: the FILE, as readConfig serves it, and only what is
	// running when no file could be read.
	//
	// The distinction is the difference between repairing a station and destroying it. A
	// station that started out of service runs the NEUTRAL profile, whose catalog carries
	// no password at all (serve.go, fallbackProfile): taking the secret back from what is
	// running would erase the cooperative's WebDAV account on the very save that repaired
	// the file. The two hashes above escape that trap only because the fallback profile
	// copies Admin over by hand.
	served := current
	if onDiskErr == nil {
		served = onDisk
	}
	next.Catalog.Options = carriedOverSecret(served.Catalog, next.Catalog)

	// The drop probe touches the filesystem, so it runs only when the block it is about has
	// MOVED: a save about the weighing thresholds must not fail because a producer's share
	// happens to be down that morning. The decision belongs here, to the layer that holds
	// both versions; the execution stays in the domain (§11.3, control 46).
	registries := s.registries
	if registries.Paths != nil &&
		domain.BlockFingerprint(served.Catalog) == domain.BlockFingerprint(next.Catalog) {
		registries.Paths = readOnlyPaths{inner: registries.Paths}
	}

	if faults := (&next).Validate(registries); len(faults) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, problem{
			Code:    "ERR-CFG-01",
			Message: "Cette configuration ne peut pas être appliquée.",
			Faults:  faultsOf(faults),
		})
		return
	}
	if err := s.configStore.Save(r.Context(), next); err != nil {
		writeProblem(w, http.StatusInternalServerError, "",
			"Configuration non écrite : "+err.Error())
		return
	}

	// The rollback of §11.4 puts THE FILE AS IT WAS back, and `onDisk` is that document: it
	// was read above, before the Save, and it is the same variable `served` already stands
	// for. Handing it over is what keeps a station whose file is faulty — and which
	// therefore RUNS the neutral profile — from having the factory settings written over
	// its own file sixty seconds after a volunteer repaired it.
	//
	// A file that could not be read hands over nothing, and the station falls back on what
	// it is running: that is all such a station has left.
	reload := station.ReloadRequest{Next: next}
	if onDiskErr == nil {
		reload.FileBefore = &onDisk
	}
	outcome, err := s.controller.Reload(reload)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "",
			"Configuration écrite mais non appliquée : "+err.Error())
		return
	}
	s.technical.Technical(domain.LevelInfo, "config", "",
		"Configuration enregistrée.", next.Fingerprint())

	s.moveListener(current, next, outcome.ConfirmBefore)
	writeJSON(w, http.StatusOK, s.configPayload(next,
		s.confirmationOf(outcome.Changed, outcome.ConfirmBefore)))
}

// carriedOverSecret puts the catalog password back when the submitted document carries
// none, and leaves a typed one alone.
//
// The screen never received it — configPayload blanks it — so it cannot send it back. This
// is the same reasoning as the two hashes of writeConfig and the same repair: without it, a
// save about the polling interval would take the catalog down at the next poll, silently.
//
// A password can therefore not be EMPTIED from this screen. That is the price of a
// write-only field, and it is paid where every other irreversible repair is paid: in the
// file itself (ADR-034).
//
// # What says « this share really has no password », and what does not
//
// It is the SOURCE that says it, never the shape of the key. A blank value and an absent
// key are two spellings of the same silence, and a browser produces the second without
// anybody meaning to: the Station page copies an imported file into the draft, the export
// it came from carries no password at all (Config.Export deletes it whatever `hardware`
// says), and JSON.stringify drops a property whose value is undefined. Reading that as a
// deletion erased the cooperative's WebDAV account through Importer → Recopier →
// Enregistrer, on a save about something else entirely.
func carriedOverSecret(served, submitted domain.CatalogConfig) domain.DriverOptions {
	// Changing the SOURCE is the one gesture that legitimately drops the account: the
	// Catalogue screen deletes the url, the user and the password when somebody moves the
	// station to a local directory, because control 39 refuses their mere presence there.
	// Writing the secret back would answer that move with a refusal on a field the screen no
	// longer shows, and no screen could ever repair it.
	if submitted.Type != served.Type {
		return submitted.Options
	}
	if typed, ok := submitted.Options.Text(catalogPasswordOption); ok && typed != "" {
		return submitted.Options
	}
	inForce, ok := served.Options.Text(catalogPasswordOption)
	if !ok || inForce == "" {
		return submitted.Options
	}
	return submitted.Options.WithText(catalogPasswordOption, inForce)
}

// readOnlyPaths answers every DROP question with "nothing to check".
//
// It is what says « this save is not about the catalog ». The READ question of control 44
// still travels, because it is about another key entirely and costs one stat.
//
// inner is never nil: writeConfig wraps a probe that exists, and wraps nothing otherwise —
// a nil PathChecker already means « we cannot know » to every control.
type readOnlyPaths struct{ inner domain.PathChecker }

func (p readOnlyPaths) Readable(path string) error { return p.inner.Readable(path) }

func (readOnlyPaths) Droppable(string) error { return nil }

// confirmationOf renders the countdown, or nothing when there is none.
func (s *Server) confirmationOf(changed []string, before time.Time) *confirmationDTO {
	if before.IsZero() {
		return nil
	}
	left := before.Sub(s.clock.Now())
	if left < 0 {
		left = 0
	}
	return &confirmationDTO{
		Changed: changed, ConfirmBefore: stamp(before),
		SecondsLeft: int(left.Seconds()),
	}
}

// confirmConfig is POST /admin/api/config/confirm: the branch is not being cut.
//
// It confirms BOTH halves — the station stops its countdown, and the socket stops
// waiting to be put back. They are two objects and one decision.
func (s *Server) confirmConfig(w http.ResponseWriter, _ *http.Request) {
	if s.controller == nil {
		unavailable(w, "la configuration n'est pas modifiable ici")
		return
	}
	if err := s.controller.Confirm(); err != nil {
		writeProblem(w, http.StatusConflict, "", "Aucune confirmation n'est attendue.")
		return
	}
	if s.binder != nil {
		s.binder.Confirm()
	}
	writeJSON(w, http.StatusOK, actionDTO{
		Done: true, Message: "La configuration est confirmée."})
}

// exportConfig is GET /admin/api/config/export?hardware=0 — what §11.5 clones.
func (s *Server) exportConfig(w http.ResponseWriter, r *http.Request) {
	includeHardware := r.URL.Query().Get("hardware") != "0"
	cfg := s.hub.Config()
	exported := cfg.Export(includeHardware)
	// Config.Export drops the password hash always and the RECOVERY code hash only
	// when the hardware is excluded. That asymmetry has no reason to exist here: the
	// recovery code is printed on the installation sheet OF THIS STATION, and carrying
	// it into a clone is precisely the « four stations sharing one secret nobody
	// chose » that the same function refuses for the password. It is signalled to the
	// domain; until then, this route redacts it.
	exported.Admin.RecoveryCodeHash = ""

	raw, err := json.MarshalIndent(exported, "", "  ")
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "", "Export impossible : "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="config-export.json"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

// importConfig is POST /admin/api/config/import: it VALIDATES and returns the diff,
// and applies nothing.
//
// Applying is PUT, which the screen calls once a human has read the field-by-field
// diff of §14.4. An import that applied itself would be a station reconfigured by a
// file somebody double-clicked.
func (s *Server) importConfig(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "", "Requête illisible : "+err.Error())
		return
	}
	var candidate domain.Config
	if err := json.Unmarshal(raw, &candidate); err != nil {
		writeProblem(w, http.StatusBadRequest, "", "Configuration illisible : "+err.Error())
		return
	}
	current := s.hub.Config()
	// The two secrets and the station number are NOT imported: a clone must not
	// inherit the password of the station it was cloned from, nor its number (§11.5).
	candidate.Admin = current.Admin
	candidate.Station.Number = current.Station.Number

	body := struct {
		configDTO
		Faults  []faultDTO `json:"faults"`
		Changed []string   `json:"changed_blocks"`
	}{
		configDTO: s.configPayload(candidate, nil),
		Faults:    faultsOf((&candidate).Validate(s.registries)),
		Changed:   changedBlocks(current, candidate),
	}
	writeJSON(w, http.StatusOK, body)
}

// configVersions is GET /admin/api/config/versions.
func (s *Server) configVersions(w http.ResponseWriter, r *http.Request) {
	if s.configStore == nil {
		unavailable(w, "la configuration n'est pas versionnée ici")
		return
	}
	versions, err := s.configStore.Versions(r.Context())
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	out := make([]configVersionDTO, 0, len(versions))
	for _, v := range versions {
		out = append(out, configVersionDTO{
			Version: v.Version, ModifiedAt: stamp(v.ModifiedAt), Fingerprint: v.Fingerprint,
		})
	}
	writeJSON(w, http.StatusOK, struct {
		Versions []configVersionDTO `json:"versions"`
	}{out})
}

// restoreRequest is the body of POST /admin/api/config/restore.
type restoreRequest struct {
	Version int `json:"version"`
}

// restoreConfig is POST /admin/api/config/restore: one of the five backups comes back
// into service, through the SAME path as any other save.
func (s *Server) restoreConfig(w http.ResponseWriter, r *http.Request) {
	var body restoreRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if s.configStore == nil || s.controller == nil {
		unavailable(w, "la configuration n'est pas versionnée ici")
		return
	}
	// A restoration is a save like any other and it arms the same countdown, so it is
	// refused inside the window for the same reason writeConfig is.
	if deadline := s.controller.PendingConfirmation(); !deadline.IsZero() {
		writeProblem(w, http.StatusConflict, "",
			"Une configuration attend encore d'être confirmée. Confirmez-la, ou laissez le "+
				"poste revenir tout seul à la version précédente, puis restaurez de nouveau.")
		return
	}
	restored, err := s.configStore.Restore(r.Context(), body.Version)
	// A backup one of whose blocks did not decode EXISTS, and « introuvable » would send a
	// volunteer looking for a file that is right there beside config.json — listed by
	// Versions, on the screen, one line above the button they just pressed. It cannot be
	// applied as it stands, which is the answer the Validate branch below already gives.
	var unreadable *domain.UnreadableBlocksError
	if errors.As(err, &unreadable) {
		writeJSON(w, http.StatusUnprocessableEntity, problem{
			Code: "ERR-CFG-01",
			Message: "La version " + strconv.Itoa(body.Version) + " existe, mais " +
				unreadable.BlockPhrase() + " " + unreadable.NotRead() + " : la restaurer " +
				"poserait la configuration d'usine " + unreadable.InTheirPlace() + ".",
			Faults: faultsOf(unreadable.Faults),
		})
		return
	}
	if err != nil {
		writeProblem(w, http.StatusNotFound, "",
			"Version "+strconv.Itoa(body.Version)+" introuvable : "+err.Error())
		return
	}
	current := s.hub.Config()
	restored.Admin = current.Admin

	if faults := (&restored).Validate(s.registries); len(faults) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, problem{
			Code:    "ERR-CFG-01",
			Message: "Cette version ne peut plus être appliquée telle quelle.",
			Faults:  faultsOf(faults),
		})
		return
	}
	// READ BEFORE THE WRITE, and for the reason writeConfig reads it: restoring a version
	// arms the same countdown, and a rollback that put the RUNNING configuration back would
	// write the factory profile onto the file of a station that started out of service.
	// This is the route that reaches it — writeConfig already refuses a station whose file
	// carries a retired key, and this one does not.
	//
	// A file only PART of which decoded is still a better rollback target than memory: its
	// read blocks are the shop's. Leaving FileBefore nil there is what made the countdown
	// write the neutral profile onto the file sixty seconds later, unattended.
	fileBefore, fileErr := s.configStore.Read(r.Context())
	var unreadableBefore *domain.UnreadableBlocksError
	if errors.As(fileErr, &unreadableBefore) {
		fileBefore, fileErr = unreadableBefore.Config, nil
	}

	if err := s.configStore.Save(r.Context(), restored); err != nil {
		writeProblem(w, http.StatusInternalServerError, "",
			"Configuration non écrite : "+err.Error())
		return
	}
	reload := station.ReloadRequest{Next: restored}
	if fileErr == nil {
		reload.FileBefore = &fileBefore
	}
	outcome, err := s.controller.Reload(reload)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "",
			"Configuration écrite mais non appliquée : "+err.Error())
		return
	}
	s.moveListener(current, restored, outcome.ConfirmBefore)
	writeJSON(w, http.StatusOK, s.configPayload(restored,
		s.confirmationOf(outcome.Changed, outcome.ConfirmBefore)))
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

// changedBlocks names the blocks two configurations disagree about, by the SAME
// normalized fingerprint the station compares (§11.4).
//
// Normalized, and not a byte comparison: two documents that differ only by the order
// of their keys describe the same station, and cutting a serial port over a key order
// is the failure mode this comparison exists to avoid.
func changedBlocks(previous, next domain.Config) []string {
	var changed []string
	blocks := []struct {
		name          string
		before, after any
	}{
		{"station", previous.Station, next.Station},
		{"network", previous.Network, next.Network},
		{"ui", previous.UI, next.UI},
		{"scale", previous.Scale, next.Scale},
		{"printer", previous.Printer, next.Printer},
		{"pricing", previous.Pricing, next.Pricing},
		{"barcode", previous.Barcode, next.Barcode},
		{"limits", previous.Limits, next.Limits},
		{"stability", previous.Stability, next.Stability},
		{"catalog", previous.Catalog, next.Catalog},
		{"journal", previous.Journal, next.Journal},
		{"maintenance", previous.Maintenance, next.Maintenance},
	}
	for _, b := range blocks {
		if domain.BlockFingerprint(b.before) != domain.BlockFingerprint(b.after) {
			changed = append(changed, b.name)
		}
	}
	return changed
}

// moveListener applies a change of network.listen to the socket (§11.4, ADR-027).
//
// A net.Listener closes and reopens in three lines: there has never been a technical
// reason to demand a process restart for it. It goes through the same three-step
// window as the hardware — apply, count down, roll back if nobody confirms — and the
// station's own countdown puts the CONFIGURATION back at the same instant this puts
// the SOCKET back.
func (s *Server) moveListener(previous, next domain.Config, confirmBefore time.Time) {
	if s.binder == nil || previous.Network.Listen == next.Network.Listen {
		return
	}
	if err := s.binder.Rebind(next.Network.Listen, confirmBefore); err != nil {
		s.technical.Technical(domain.LevelError, "http", "ERR-SYS-02",
			"Nouvelle adresse d'écoute refusée : l'ancienne reste en service.", err.Error())
		return
	}
	s.technical.Technical(domain.LevelWarn, "http", "",
		"Adresse d'écoute changée : confirmez sous 60 s ou elle reviendra.",
		previous.Network.Listen+" → "+next.Network.Listen)
}
