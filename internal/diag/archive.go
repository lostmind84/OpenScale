package diag

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"openscale/internal/domain"
)

// diagnostic.zip is what a volunteer sends when they call for help (§15.4). One button, no
// password, and everything a diagnosis needs FROM A DISTANCE — which is the only realistic
// remote support mechanism for a team of volunteers.
//
// The two rules of this file:
//
//  1. NO SECRET LEAVES. Every text member goes through the scrubber of redact.go, and the
//     configuration is redacted by key name on top of that. It is a security requirement,
//     and a test looks for the values inside the produced archive.
//  2. A MEMBER THAT CANNOT BE BUILT IS NOT A FAILURE. An archive is worth having when the
//     base is corrupt, when the service is down and when the label directory does not
//     exist — those are the mornings somebody presses the button. Each member records its
//     own failure in errors.txt and the archive is still valid, still readable, still
//     complete enough to work from.

// The quantities of §15.4, quoted from the document.
const (
	// archivedWeighings is « 200 dernières pesées ».
	archivedWeighings = 200
	// archivedTechnical is « 500 derniers événements techniques ».
	archivedTechnical = 500
	// archivedFrames is « 30 dernières trames brutes ».
	archivedFrames = 30
	// archivedSBPL is « 5 derniers .sbpl ».
	archivedSBPL = 5
	// archivedLabelImages is « 3 derniers PNG d'étiquette ».
	archivedLabelImages = 3
)

// maxLabelBytes bounds one captured label copied into the archive.
//
// A 40 × 25 mm raster at 203 dpi is a few tens of kilobytes; two megabytes is a ceiling that
// no real label reaches and that stops a stray file in the label directory from turning a
// support archive into something nobody can email.
const maxLabelBytes = 2 << 20

// TechnicalEntry is one line of the technical journal as the archive reads it.
//
// Declared HERE and not imported: internal/diag names no storage package (§5.2, cut 3). The
// composition root converts, which is a handful of lines and the price of the cut.
type TechnicalEntry struct {
	OccurredAt time.Time
	Level      string
	Source     string
	Code       string
	Message    string
	Detail     string
}

// CatalogCounts is the inventory of the catalog IN SERVICE, read from the base.
//
// It is not the inventory of the last IMPORT, which §14.4 publishes on the dashboard and
// which imports.csv carries: this one answers « what is in the grid right now », and it
// answers it even when the service is down.
type CatalogCounts struct {
	Products   int            `json:"products_count"`
	Weighable  int            `json:"weighable_count"`
	Withdrawn  int            `json:"withdrawn_count"`
	ByCategory map[string]int `json:"by_category"`
}

// Journal is what diagnostic.zip reads out of the station base.
//
// Declared HERE, on the consumer's side. Every method may fail, and a failure is written
// into the archive rather than aborting it.
type Journal interface {
	// Weighings returns the most recent weighings, newest first.
	Weighings(ctx context.Context, limit int) ([]domain.Weighing, error)
	// TechnicalEntries returns the most recent technical lines, newest first.
	TechnicalEntries(ctx context.Context, limit int) ([]TechnicalEntry, error)
	// Imports returns the import history, most recent first.
	Imports(ctx context.Context, limit int) ([]domain.Import, error)
	// CatalogCounts reports what the catalog in service holds.
	CatalogCounts(ctx context.Context) (CatalogCounts, error)
}

// Bundle builds diagnostic.zip. It is built once and is safe for concurrent use.
type Bundle struct {
	doctor  *Doctor
	journal Journal
	// labels is <data>/labels, where the `file` transport drops one copy per label
	// (§11.1). Empty means this station keeps none, which is the default.
	labels string
}

// NewBundle wires the archive over a doctor.
//
// journal may be nil — a station whose base will not open still produces an archive, and
// that archive is exactly the one somebody needs.
func NewBundle(d *Doctor, journal Journal, labelsDir string) (*Bundle, error) {
	if d == nil {
		return nil, errors.New("diag.NewBundle: pas de doctor ; le rapport est le premier membre de l'archive")
	}
	return &Bundle{doctor: d, journal: journal, labels: labelsDir}, nil
}

// Diagnostic writes the archive into w.
//
// It is what GET /admin/api/diagnostic.zip serves and what `openscale doctor --zip` writes
// to a file. The signature is the one internal/web asks of a diagnostician.
func (b *Bundle) Diagnostic(ctx context.Context, w io.Writer) error {
	report := b.doctor.Run(ctx)
	health, healthErr := b.doctor.Health(ctx)
	loaded := b.doctor.readConfiguration()
	// The scrubber is built from the configuration ON DISK, which is the only place the
	// literal secrets of this station are known. A station whose configuration cannot be
	// read has no secret to protect that this archive could carry.
	clean := newScrubber(loaded.Config)

	archive := zip.NewWriter(w)
	writer := &memberWriter{zip: archive, clean: clean, clock: b.doctor.o.Clock}

	writer.text("README.txt", readme(report))
	writer.text("doctor.txt", reportText(report))
	writer.json("doctor.json", report)
	writer.json("system.json", systemMember(report, health, healthErr))

	if loaded.Present && loaded.Parsed {
		redacted, err := Redact(loaded.Config)
		if err != nil {
			writer.fail("config.redacted.json", err)
		} else {
			writer.bytes("config.redacted.json", redacted)
		}
	} else {
		writer.fail("config.redacted.json", configFailure(loaded))
	}

	if healthErr != nil {
		writer.fail("health.json", healthErr)
	} else {
		writer.bytes("health.json", health.Raw)
	}

	b.writeJournalMembers(ctx, writer)
	b.writeLabels(writer)
	writer.errorsMember()

	if err := archive.Close(); err != nil {
		return fmt.Errorf("archive de diagnostic non refermée : %w", err)
	}
	return nil
}

// writeJournalMembers writes everything that comes out of the base.
func (b *Bundle) writeJournalMembers(ctx context.Context, writer *memberWriter) {
	if b.journal == nil {
		writer.fail("journal", errors.New("la base du poste n'a pas pu être ouverte : "+
			"ni les pesées, ni le journal technique, ni les imports ne sont dans cette archive"))
		return
	}

	weighings, err := b.journal.Weighings(ctx, archivedWeighings)
	if err != nil {
		writer.fail("weighings.csv", err)
	} else {
		writer.csv("weighings.csv", weighingHeader, weighingRows(weighings))
		writer.text("frames.txt", framesMember(weighings))
	}

	if lines, err := b.journal.TechnicalEntries(ctx, archivedTechnical); err != nil {
		writer.fail("technical.csv", err)
	} else {
		writer.csv("technical.csv", technicalHeader, technicalRows(lines))
	}

	if imports, err := b.journal.Imports(ctx, archivedImports); err != nil {
		writer.fail("imports.csv", err)
	} else {
		writer.csv("imports.csv", importHeader, importRows(imports))
	}

	if counts, err := b.journal.CatalogCounts(ctx); err != nil {
		writer.fail("catalog.json", err)
	} else {
		writer.json("catalog.json", counts)
	}
}

// archivedImports is how many imports the archive carries. Twenty, which is what §14.4 puts
// on the expert catalog page: enough to see a source that has been failing for a fortnight.
const archivedImports = 20

// writeLabels copies the last captured labels, which is what makes a printing complaint
// diagnosable without travelling to the shop.
func (b *Bundle) writeLabels(writer *memberWriter) {
	if b.labels == "" {
		return
	}
	entries, err := os.ReadDir(b.labels)
	if err != nil {
		// A station that keeps no label copies is the DEFAULT: the `file` transport is a
		// development and support transport, not the production one. Saying so is enough.
		writer.note("labels", fmt.Sprintf("aucune copie d'étiquette : %v", err))
		return
	}

	for _, group := range []struct {
		suffix string
		keep   int
	}{{".sbpl", archivedSBPL}, {".png", archivedLabelImages}} {
		for _, name := range lastFiles(entries, group.suffix, group.keep) {
			path := filepath.Join(b.labels, name)
			raw, err := readBounded(path, maxLabelBytes)
			if err != nil {
				writer.fail("labels/"+name, err)
				continue
			}
			// NOT scrubbed, and deliberately: a captured label carries a product name, a
			// weight and a barcode, and nothing from the configuration. Passing a raster
			// through a text substitution would corrupt it.
			writer.raw("labels/"+name, raw)
		}
	}
}

// lastFiles reports the newest n names with that suffix, newest first.
//
// The order is taken from the MODIFICATION TIME and not from the name: the file transport
// names its copies after the job identifier, which is sortable, but a station whose clock
// jumped — the very thing ERR-SYS-07 watches for — would have names that sort against the
// order the labels came out in.
func lastFiles(entries []os.DirEntry, suffix string, n int) []string {
	type candidate struct {
		name     string
		modified time.Time
	}
	var found []candidate
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), suffix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		found = append(found, candidate{name: entry.Name(), modified: info.ModTime()})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].modified.After(found[j].modified) })
	if len(found) > n {
		found = found[:n]
	}
	names := make([]string, 0, len(found))
	for _, c := range found {
		names = append(names, c.name)
	}
	return names
}

// readBounded reads at most limit bytes of a file.
func readBounded(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, limit))
}

// --- The members ------------------------------------------------------------

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
		"doctor.txt · doctor.json  les quinze contrôles, avec la consigne de chacun",
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

// --- The writer -------------------------------------------------------------

// memberWriter adds members to the archive, scrubbing every text one, and collects what
// went wrong instead of giving up.
type memberWriter struct {
	zip   *zip.Writer
	clean *scrubber
	clock interface{ Now() time.Time }
	notes []string
}

// text adds one text member, scrubbed.
func (m *memberWriter) text(name, content string) {
	m.raw(name, []byte(m.clean.Clean(content)))
}

// bytes adds one text member that is already a byte slice, scrubbed.
func (m *memberWriter) bytes(name string, content []byte) {
	m.raw(name, m.clean.CleanBytes(content))
}

// json adds one member as indented JSON, scrubbed.
func (m *memberWriter) json(name string, value any) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		m.fail(name, err)
		return
	}
	m.bytes(name, raw)
}

// csv adds one member as a semicolon-separated CSV with a UTF-8 BOM, scrubbed.
//
// A semicolon and a BOM for the reason internal/web already gives: this file is opened in
// the spreadsheet of a French Windows, where a comma-separated file lands in one column. It
// is the same trade-off the producer's own export makes (§10.2).
func (m *memberWriter) csv(name string, header []string, rows [][]string) {
	out := &strings.Builder{}
	out.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(out)
	writer.Comma = ';'
	_ = writer.Write(header)
	for _, row := range rows {
		_ = writer.Write(row)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		m.fail(name, err)
		return
	}
	m.text(name, out.String())
}

// raw adds one member verbatim, WITHOUT scrubbing. It is for binary content only.
func (m *memberWriter) raw(name string, content []byte) {
	entry, err := m.zip.CreateHeader(&zip.FileHeader{
		Name:     name,
		Method:   zip.Deflate,
		Modified: m.now(),
	})
	if err != nil {
		m.note(name, "membre non créé : "+err.Error())
		return
	}
	if _, err := entry.Write(content); err != nil {
		m.note(name, "membre incomplet : "+err.Error())
	}
}

// fail records that one member could not be built, and writes the reason where the reader
// will find it.
func (m *memberWriter) fail(name string, err error) {
	m.note(name, err.Error())
}

// note records one line for errors.txt.
func (m *memberWriter) note(name, message string) {
	m.notes = append(m.notes, name+" : "+message)
}

// errorsMember writes what could not be gathered.
//
// It is written EVEN WHEN EMPTY, and that is the point: a reader who finds errors.txt saying
// « rien à signaler » knows the archive is complete, whereas a missing file could mean
// either « nothing failed » or « the archive was truncated ».
func (m *memberWriter) errorsMember() {
	out := &strings.Builder{}
	fmt.Fprintf(out, "# Ce qui n'a pas pu être rassemblé dans cette archive.\n")
	fmt.Fprintf(out, "# Une archive incomplète reste utile : c'est justement les matins où quelque\n")
	fmt.Fprintf(out, "# chose est cassé qu'on appuie sur ce bouton.\n\n")
	if len(m.notes) == 0 {
		fmt.Fprintf(out, "rien à signaler : tous les membres ont été écrits.\n")
	}
	for _, note := range m.notes {
		fmt.Fprintf(out, "%s\n", note)
	}
	// Written through raw and scrubbed by hand: adding a member from inside the member
	// writer must not be able to append to m.notes while it is being rendered.
	m.raw("errors.txt", m.clean.CleanBytes([]byte(out.String())))
}

// now is the instant stamped on every member, read from the INJECTED clock.
//
// Every member carries the SAME instant, which is what makes an archive reproducible in a
// test: a member stamped from the wall clock would make two archives of one frozen station
// differ.
func (m *memberWriter) now() time.Time {
	if m.clock == nil {
		return time.Time{}
	}
	return m.clock.Now()
}
