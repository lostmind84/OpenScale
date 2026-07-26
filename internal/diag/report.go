package diag

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// The identifiers of the fifteen controls of §15.4, in the order the document
// enumerates them.
//
// They are ENGLISH and STABLE: a support call quotes them, diagnostic.zip carries them,
// and the wording of the French sentence beside them is free to improve without
// invalidating a note somebody wrote in the shop binder. The third one is the autologon,
// and it is third because bloquant-7 counts it as « énuméré en 3ᵉ position sur les 15
// contrôles ».
const (
	ControlService           = "service"
	ControlKioskTask         = "kiosk-task"
	ControlUnattendedRestart = "unattended-restart"
	ControlDataDirectory     = "data-directory"
	ControlDiskSpace         = "disk-space"
	ControlListenAddress     = "listen-address"
	ControlConfiguration     = "configuration"
	ControlDatabase          = "database"
	ControlMigrations        = "migrations"
	ControlSerialPort        = "serial-port"
	ControlPrintQueue        = "print-queue"
	ControlScaleRate         = "scale-rate"
	ControlCatalogSource     = "catalog-source"
	ControlSystemClock       = "system-clock"
	ControlPowerSettings     = "power-settings"
)

// ControlOrder is the fifteen identifiers in the order §15.4 lists them, and the
// authority on « how many controls are there ».
//
// A slice and not a count: a test asserts that a report carries exactly these fifteen,
// once each, in this order — which is what keeps a fourteenth from being dropped in a
// refactor and nobody noticing.
var ControlOrder = []string{
	ControlService, ControlKioskTask, ControlUnattendedRestart,
	ControlDataDirectory, ControlDiskSpace, ControlListenAddress,
	ControlConfiguration, ControlDatabase, ControlMigrations,
	ControlSerialPort, ControlPrintQueue, ControlScaleRate,
	ControlCatalogSource, ControlSystemClock, ControlPowerSettings,
}

// Control is what one of the fifteen controls reports: what was checked, how it came
// out, and what to do about it.
type Control struct {
	// ID is one of the fifteen stable identifiers above.
	ID string `json:"id"`
	// Rank is the position of §15.4, 1 to 15.
	Rank int `json:"rank"`
	// Checked is FRENCH and names WHAT WAS VERIFIED, in the words of §15.4. It is
	// written in the past tense of a report, not as a question.
	Checked string `json:"checked"`
	Status  Status `json:"status"`
	// Observed is FRENCH and says what was actually found, WITH the figures: « 412 Mo
	// libres sur 118 Go » and never « espace insuffisant ».
	Observed string `json:"observed"`
	// Remedy is FRENCH and says what to DO. Never empty when Status.NeedsRemedy.
	Remedy string `json:"remedy,omitempty"`
	// Code is an ERR-xxx-nn when one is allocated for this failure, and EMPTY
	// otherwise. An invented code is worse than none: somebody would look it up.
	Code string `json:"code,omitempty"`
}

// Report is the whole of `openscale doctor`, and the first file of diagnostic.zip.
type Report struct {
	At time.Time `json:"at"`

	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`

	System SystemInfo `json:"system"`

	// Station, StationName and Coop come from the configuration file, so they are
	// present even on a station whose service will not start — which is the one case
	// this command exists for.
	Station     int    `json:"station"`
	StationName string `json:"station_name"`
	Coop        string `json:"coop"`
	// Fingerprint is the eight characters of §11.5, so that « les quatre postes
	// affichent-ils la même empreinte ? » is answerable from an archive.
	Fingerprint string `json:"config_fingerprint"`

	ConfigPath string `json:"config_path"`
	DataDir    string `json:"data_dir"`

	Controls []Control `json:"controls"`
}

// Worst reports the most serious verdict of the report, which is what the exit code and
// the last line of the terminal output rest on.
func (r Report) Worst() Status {
	out := StatusPass
	for _, control := range r.Controls {
		out = worse(out, control.Status)
	}
	return out
}

// Count reports how many controls came out with one verdict.
func (r Report) Count(s Status) int {
	n := 0
	for _, control := range r.Controls {
		if control.Status == s {
			n++
		}
	}
	return n
}

// Control returns the control with that identifier.
func (r Report) Control(id string) (Control, bool) {
	for _, control := range r.Controls {
		if control.ID == id {
			return control, true
		}
	}
	return Control{}, false
}

// Validate reports what is wrong with the report ITSELF, and it exists for exactly one
// rule: a verdict that is not green must say what to do.
//
// It is called by the doctor before it hands the report back, so that the rule cannot be
// broken by adding a control and forgetting its remedy. It also catches an empty
// Observed, because « ÉCHEC » with nothing observed names no fact.
func (r Report) Validate() error {
	var faults []error
	for _, control := range r.Controls {
		switch {
		case control.Checked == "":
			faults = append(faults, fmt.Errorf("contrôle %q : rien n'est nommé comme vérifié", control.ID))
		case control.Observed == "":
			faults = append(faults, fmt.Errorf("contrôle %q : aucun constat n'est rapporté", control.ID))
		case control.Status.NeedsRemedy() && control.Remedy == "":
			faults = append(faults, fmt.Errorf(
				"contrôle %q en %s sans consigne : un diagnostic qui ne dit pas quoi faire "+
					"n'a rien diagnostiqué (§15.4)", control.ID, control.Status.Label()))
		}
	}
	return errors.Join(faults...)
}

// WriteText renders the report the way the terminal shows it, in French.
//
// The layout is what makes it readable at a glance on a bad morning: one line per
// control, the verdict in a fixed column, and the remedy indented under the line it
// belongs to — never collected at the bottom, where nobody would join it back to its
// cause.
func (r Report) WriteText(w io.Writer) error {
	out := &strings.Builder{}

	fmt.Fprintf(out, "openscale doctor — %s\n", r.stationLine())
	fmt.Fprintf(out, "  version   %s\n", r.versionLine())
	fmt.Fprintf(out, "  système   %s\n", r.System.Line())
	fmt.Fprintf(out, "  config    %s\n", r.ConfigPath)
	fmt.Fprintf(out, "  données   %s\n", r.DataDir)
	fmt.Fprintf(out, "  empreinte %s\n\n", or(r.Fingerprint, "inconnue"))

	for _, control := range r.Controls {
		fmt.Fprintf(out, "%2d. %-10s %s\n", control.Rank, control.Status.Label(), control.Checked)
		fmt.Fprintf(out, "                %s\n", control.Observed)
		if control.Remedy != "" {
			fmt.Fprintf(out, "             →  %s\n", prefixContinuation(control.Remedy))
		}
		if control.Code != "" {
			fmt.Fprintf(out, "                %s\n", control.Code)
		}
	}

	fmt.Fprintf(out, "\n%s\n", r.summaryLine())
	_, err := io.WriteString(w, out.String())
	return err
}

// stationLine names the station, and says so honestly when the configuration could not
// be read: a report headed « poste 0 » would look like a station numbered zero.
func (r Report) stationLine() string {
	if r.Station == 0 {
		return "poste non identifié (configuration illisible ou vide)"
	}
	name := or(r.StationName, "sans nom")
	if r.Coop == "" {
		return fmt.Sprintf("poste %d « %s »", r.Station, name)
	}
	return fmt.Sprintf("poste %d « %s » — %s", r.Station, name, r.Coop)
}

// versionLine is what a support call asks for first.
func (r Report) versionLine() string {
	return fmt.Sprintf("%s (commit %s, compilé le %s)",
		or(r.Version, "inconnue"), or(r.Commit, "inconnu"), or(r.BuildDate, "date inconnue"))
}

// summaryLine is the last line, and the one a volunteer reads out over the telephone.
func (r Report) summaryLine() string {
	switch r.Worst() {
	case StatusFail:
		return fmt.Sprintf("%d contrôle(s) en échec, %d avertissement(s) : ce poste ne peut pas "+
			"fonctionner en l'état. Appliquez les consignes ci-dessus, de haut en bas.",
			r.Count(StatusFail), r.Count(StatusWarn))
	case StatusWarn:
		return fmt.Sprintf("%d avertissement(s) : le poste fonctionne, et quelque chose demande "+
			"une intervention avant que cela ne devienne une panne.", r.Count(StatusWarn))
	case StatusUnknown:
		return fmt.Sprintf("%d contrôle(s) n'ont pas pu être établis d'ici. Rien d'anormal n'a été "+
			"constaté par ailleurs.", r.Count(StatusUnknown))
	}
	return "Les quinze contrôles sont au vert."
}

// prefixContinuation indents the continuation lines of a remedy that carries several,
// so that a multi-line instruction keeps its left edge under the arrow.
func prefixContinuation(text string) string {
	return strings.ReplaceAll(text, "\n", "\n                ")
}

// or reports value, or fallback when value is empty. It is what keeps an empty field
// from rendering as a blank a reader would take for a missing line.
func or(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
