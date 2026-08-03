package diag

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"openscale/internal/domain"
)

// This file renders the members of diagnostic.zip: the two a human opens first — README.txt
// and the identity of the station — and the four tables that come out of the base. Nothing
// here writes into the archive; it hands strings and rows to the writer, which is what makes
// every one of these testable without a zip.

// systemInfoMember is the « version + OS + uptime » of §15.4, plus the fingerprint a
// support call compares across the four stations of a fleet.
type systemInfoMember struct {
	Version     string     `json:"version"`
	Commit      string     `json:"commit"`
	BuildDate   string     `json:"build_date"`
	System      SystemInfo `json:"system"`
	Station     int        `json:"station"`
	StationName string     `json:"station_name"`
	Coop        string     `json:"coop"`
	Fingerprint string     `json:"config_fingerprint"`
	ConfigPath  string     `json:"config_path"`
	DataDir     string     `json:"data_dir"`
	// ServiceReached says whether the running station answered, so that a reader knows
	// whether the device state in this archive is current or absent.
	ServiceReached bool   `json:"service_reached"`
	ServiceDetail  string `json:"service_detail,omitempty"`
	// ServiceVersion is the version the RUNNING station reports, which may differ from the
	// binary that produced this archive — §14.5 designs for exactly that.
	ServiceVersion string `json:"service_version,omitempty"`
}

// systemMember builds the identity member.
func systemMember(report Report, health Health, healthErr error) systemInfoMember {
	out := systemInfoMember{
		Version: report.Version, Commit: report.Commit, BuildDate: report.BuildDate,
		System: report.System, Station: report.Station, StationName: report.StationName,
		Coop: report.Coop, Fingerprint: report.Fingerprint,
		ConfigPath: report.ConfigPath, DataDir: report.DataDir,
		ServiceReached: healthErr == nil,
	}
	if healthErr != nil {
		out.ServiceDetail = healthErr.Error()
		return out
	}
	out.ServiceVersion = health.Version
	return out
}

// reportText renders the doctor report, and never fails: an archive whose first member is
// missing because a writer returned an error is an archive nobody can start reading.
func reportText(report Report) string {
	out := &strings.Builder{}
	if err := report.WriteText(out); err != nil {
		return "le rapport n'a pas pu être rendu : " + err.Error()
	}
	return out.String()
}

// readme says what this archive is and, above all, what it does NOT contain.
//
// The second half is the important one: whoever receives it must not go looking for a
// password in it, and whoever sends it must be able to read, in French, that they are not
// publishing their cooperative's WebDAV address.
func readme(report Report) string {
	out := &strings.Builder{}
	fmt.Fprintf(out, "Fichier de diagnostic OpenScale\n")
	fmt.Fprintf(out, "%s\n\n", strings.Repeat("=", 30))
	fmt.Fprintf(out, "Produit le %s pour le %s.\n", report.At.Format(clockLayout), report.stationLine())
	fmt.Fprintf(out, "Version %s.\n\n", report.versionLine())
	fmt.Fprintf(out, "%s\n\n", report.summaryLine())

	fmt.Fprintf(out, "Ce que contient cette archive\n")
	for _, line := range []string{
		"doctor.txt · doctor.json  les contrôles, avec la consigne de chacun",
		"system.json               version, système, temps de fonctionnement, empreinte",
		"config.redacted.json      la configuration du poste, SANS AUCUN SECRET",
		"health.json               ce que le service répondait au moment de l'archive",
		"weighings.csv             les 200 dernières pesées",
		"technical.csv             les 500 derniers événements techniques",
		"imports.csv               les 20 derniers imports de catalogue",
		"catalog.json              l'inventaire du catalogue en service",
		"frames.txt                les 30 dernières trames de balance",
		"labels/                   les dernières étiquettes capturées, s'il y en a",
		"errors.txt                ce qui n'a pas pu être rassemblé, et pourquoi",
	} {
		fmt.Fprintf(out, "  %s\n", line)
	}

	fmt.Fprintf(out, "\nCe qu'elle ne contient pas, et ne contiendra jamais\n")
	fmt.Fprintf(out, "  Aucun mot de passe, aucune empreinte de mot de passe, aucun code de\n")
	fmt.Fprintf(out, "  secours, et aucune adresse privée. Les valeurs concernées sont\n")
	fmt.Fprintf(out, "  remplacées par %s, y compris dans les journaux : un message d'erreur\n", Marker)
	fmt.Fprintf(out, "  qui citait une adresse a été nettoyé lui aussi.\n")
	fmt.Fprintf(out, "  Vous pouvez envoyer ce fichier sans le relire.\n")
	return out.String()
}

// configFailure names why the configuration is not in the archive.
func configFailure(loaded loadedConfig) error {
	switch {
	case !loaded.Present:
		return fmt.Errorf("le fichier de configuration n'a pas pu être lu : %w", loaded.Err)
	case !loaded.Parsed:
		return fmt.Errorf("le fichier de configuration n'est pas un JSON exploitable : %w", loaded.Err)
	}
	return errors.New("la configuration n'a pas pu être caviardée")
}

// weighingHeader is the header of weighings.csv.
//
// It carries the RAW FRAME, which is the living corpus of §15.4: any frame that caused an
// unexplained refusal becomes a permanent test, and it can only do so if it left the shop.
var weighingHeader = []string{
	"occurred_at", "station", "job_id", "product_id", "product_name", "reference", "mode",
	"gross_g", "tare_g", "net_g", "quantity", "barcode", "source", "stability", "rate_ms",
	"result", "detail", "duration_ms", "frame",
}

// weighingRows renders the journal page.
func weighingRows(page []domain.Weighing) [][]string {
	out := make([][]string, 0, len(page))
	for _, row := range page {
		out = append(out, []string{
			stamp(row.OccurredAt), strconv.Itoa(row.Station), row.JobID,
			row.ProductID, row.ProductName, string(row.Reference), row.Mode.String(),
			strconv.FormatInt(int64(row.GrossWeight), 10),
			strconv.FormatInt(int64(row.Tare), 10),
			strconv.FormatInt(int64(row.NetWeight), 10),
			strconv.Itoa(row.Quantity), string(row.Barcode), row.Source,
			row.Stability.String(), strconv.Itoa(row.RateMS),
			row.Result, row.Detail, strconv.Itoa(row.DurationMS), row.Frame,
		})
	}
	return out
}

// technicalHeader is the header of technical.csv.
var technicalHeader = []string{"occurred_at", "level", "source", "code", "message", "detail"}

// technicalRows renders the technical journal.
func technicalRows(lines []TechnicalEntry) [][]string {
	out := make([][]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, []string{
			stamp(line.OccurredAt), line.Level, line.Source, line.Code, line.Message, line.Detail,
		})
	}
	return out
}

// importHeader is the header of imports.csv, and it is written the way §14.4 reads the
// inventory out loud: received, weighable, not weighable, anomalies. Never « en erreur ».
var importHeader = []string{
	"occurred_at", "source", "file_name", "result", "code", "reason",
	"rows_read", "unreadable_rows", "weighable", "not_weighable", "anomalies",
	"unit_mismatches", "images_decoded", "images_rejected", "products_withdrawn", "duration_ms",
}

// importRows renders the import history.
func importRows(list []domain.Import) [][]string {
	out := make([][]string, 0, len(list))
	for _, record := range list {
		out = append(out, []string{
			stamp(record.OccurredAt), record.Source, record.FileName,
			record.Result, record.Code, record.Reason,
			strconv.Itoa(record.RowsRead), strconv.Itoa(record.UnreadableRows),
			strconv.Itoa(record.Weighable), strconv.Itoa(record.NotWeighable),
			strconv.Itoa(record.Anomalies), strconv.Itoa(record.UnitMismatches),
			strconv.Itoa(record.ImagesDecoded), strconv.Itoa(record.ImagesRejected),
			strconv.Itoa(record.ProductsWithdrawn), strconv.Itoa(record.DurationMS),
		})
	}
	return out
}

// framesMember is the last raw frames, newest first, one per line.
//
// They come from the journal and not from a second capture: a frame that produced a weighing
// is a frame the station really received, timestamped, and joinable back to the weighing it
// explains. An empty frame is skipped rather than written as a blank line — a manual entry
// has no frame, and a blank line in this file would look like a frame nobody decoded.
func framesMember(page []domain.Weighing) string {
	out := &strings.Builder{}
	fmt.Fprintf(out, "# Les %d dernières trames de balance, la plus récente d'abord (§15.4).\n", archivedFrames)
	fmt.Fprintf(out, "# Format : horodate · identifiant de pesée · trame brute.\n")
	fmt.Fprintf(out, "# Toute trame ayant provoqué un refus inexpliqué devient un test permanent :\n")
	fmt.Fprintf(out, "# elle se rejoue avec `openscale replay`, sans balance et sans se déplacer.\n\n")

	written := 0
	for _, row := range page {
		if row.Frame == "" || written >= archivedFrames {
			continue
		}
		fmt.Fprintf(out, "%s\t%s\t%s\n", stamp(row.OccurredAt), row.JobID, row.Frame)
		written++
	}
	if written == 0 {
		fmt.Fprintf(out, "(aucune trame : ce poste n'a pesé qu'à la main, ou n'a pas encore pesé)\n")
	}
	return out.String()
}

// stamp is how the archive spells an instant: UTC, RFC 3339, fixed width.
//
// UTC and not local time, unlike the terminal report: a CSV is opened in a spreadsheet
// months later, possibly in another timezone, and an instant with an offset that varies
// between summer and winter cannot be sorted as text.
func stamp(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(time.RFC3339Nano)
}
