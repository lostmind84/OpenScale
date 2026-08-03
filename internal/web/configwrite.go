// This file holds THE THREE-STEP WINDOW of §11.4: a configuration is written, the
// blocks that really changed are reloaded, and the operator has to CONFIRM before
// the old one is dropped.
//
// Without that window, saving a listening address from a browser would cut the very
// connection that has to confirm it. moveListener is where that is handled, and it
// is the reason the window exists at all.

package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"openscale/internal/domain"
	"openscale/internal/station"
	"time"
)

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
