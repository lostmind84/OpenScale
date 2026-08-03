package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"golang.org/x/crypto/argon2"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
)

// What the administration surface ADDS to the bench of servebench_test.go: a session to
// log into, the four verbs its routes are driven with, the reads that come back from
// disk rather than from memory, and the waits that let a catalog arrive. Below them,
// the small helpers every assertion of these screens is written with.

// --- The bench, extended for the administration surface ----------------------

// importAnswer is the inventory as the routes publish it, and the only place a test reads
// those figures from: the DTO of internal/web, not the domain type behind it.
type importAnswer struct {
	OccurredAt     string `json:"occurred_at"`
	Source         string `json:"source"`
	FileName       string `json:"file_name"`
	Result         string `json:"result"`
	RowsRead       int    `json:"rows_read_count"`
	Weighable      int    `json:"weighable_count"`
	NotWeighable   int    `json:"not_weighable_count"`
	Anomalies      int    `json:"anomalies_count"`
	UnitMismatches int    `json:"unit_mismatches_count"`
	ImagesDecoded  int    `json:"images_decoded_count"`
}

// localDropCatalog switches the bench to the local drop, which is the source the
// drag-and-drop and the watched directory both need (§10.1).
//
// The shipped file watches a WebDAV share — the real supply chain of the cooperative — and
// a test cannot reach it. The poll interval goes down to one second because these tests
// wait on the WALL clock: `serve` runs on the system clock by construction, so the only
// honest way to keep them fast is to make the station poll faster, which is a supported
// setting (§11.2).
func localDropCatalog(cfg *domain.Config) {
	cfg.Catalog.Type = domain.CatalogSourceLocalDrop
	cfg.Catalog.Options = stripOptions(cfg.Catalog.Options,
		"url", "username", "password")
	cfg.Catalog.Options = overlayOptions(cfg.Catalog.Options, map[string]any{
		"poll_interval_s": 1,
		"stable_polls":    2,
	})
}

// withPassword puts a REAL argon2id hash in the configuration, so that a test can open a
// session.
//
// The shipped file carries a placeholder on purpose: nobody knows the password of a station
// that has not been installed. This is what `openscale config password` writes, and the
// format is the one internal/web reads back — salt and cost included, so a hash written by
// another binary keeps opening.
func withPassword(cfg *domain.Config) {
	cfg.Admin.PasswordHash = argon2idHash(benchPassword)
}

// benchPassword is the password of the bench. It is long enough to pass the controls of
// §11.3 and it is not a secret: it lives in a test.
const benchPassword = "un-mot-de-passe-de-banc-2026"

// argon2idHash writes one PHC string the session store can verify.
//
// The cost is the lowest argon2id takes, not the one an installed station writes:
// web.VerifySecret reads m, t and p back from the string it is given, so the bench
// pays for the FORMAT — which is what these tests are about — and not for the seconds
// of key derivation that protect a password nobody is attacking here.
func argon2idHash(secret string) string {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		panic("tirage du sel impossible : " + err.Error())
	}
	const (
		memory     = 8
		iterations = 1
		threads    = 1
		keyLength  = 32
	)
	key := argon2.IDKey([]byte(secret), salt, iterations, memory, threads, keyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memory, iterations, threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key))
}

// login opens an administration session and keeps the cookie.
func (b *serveBench) login(t *testing.T) {
	t.Helper()
	response := b.post(t, "/admin/api/session",
		`{"password":`+quoteJSON(benchPassword)+`}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("ouverture de session = %d : %s", response.StatusCode, readBody(t, response))
	}
	cookies := response.Cookies()
	if len(cookies) == 0 {
		t.Fatal("la session n'a pas posé de cookie")
	}
	// A jar, and not a header pasted by hand: the session travels in a cookie, and a test
	// that assembled the header itself would be testing its own helper. Every request of
	// the bench carries it afterwards, GET included.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("bocal à cookies : %v", err)
	}
	base, err := url.Parse("http://" + b.address + "/")
	if err != nil {
		t.Fatalf("adresse du poste : %v", err)
	}
	jar.SetCookies(base, cookies)
	b.client.Jar = jar
	b.cookie = cookies[0]
}

// post issues one POST with a JSON body and the session cookie, if there is one.
func (b *serveBench) post(t *testing.T, path, body string) *http.Response {
	t.Helper()
	return b.request(t, http.MethodPost, path, "application/json", strings.NewReader(body))
}

// do issues one request by whatever method, which is what a table of routes needs.
func (b *serveBench) do(t *testing.T, method, path, body string) *http.Response {
	t.Helper()
	return b.request(t, method, path, "application/json", strings.NewReader(body))
}

// put issues one PUT with a JSON body.
func (b *serveBench) put(t *testing.T, path string, body []byte) *http.Response {
	t.Helper()
	return b.request(t, http.MethodPut, path, "application/json", bytes.NewReader(body))
}

// upload issues one multipart POST, which is what a drag-and-drop really sends.
func (b *serveBench) upload(t *testing.T, path, name string, content []byte) *http.Response {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", name)
	if err != nil {
		t.Fatalf("formulaire multipart : %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("écriture du fichier dans le formulaire : %v", err)
	}
	if err := form.Close(); err != nil {
		t.Fatalf("clôture du formulaire : %v", err)
	}
	return b.request(t, http.MethodPost, path, form.FormDataContentType(), &body)
}

// request issues one request against the running station, carrying the session cookie.
func (b *serveBench) request(t *testing.T, method, path, contentType string, body io.Reader) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, "http://"+b.address+path, body)
	if err != nil {
		t.Fatalf("%s %s : %v", method, path, err)
	}
	request.Header.Set("Content-Type", contentType)
	response, err := b.client.Do(request)
	if err != nil {
		t.Fatalf("%s %s : %v", method, path, err)
	}
	return response
}

// readConfig reads the configuration the STATION is serving, through the route.
func (b *serveBench) readConfig(t *testing.T) domain.Config {
	t.Helper()
	response := b.get("/admin/api/config")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/api/config = %d : %s", response.StatusCode, readBody(t, response))
	}
	var payload struct {
		Config json.RawMessage `json:"config"`
	}
	decodeInto(t, response, &payload)
	var cfg domain.Config
	if err := json.Unmarshal(payload.Config, &cfg); err != nil {
		t.Fatalf("configuration servie illisible : %v", err)
	}
	return cfg
}

// diskConfig reads the file itself, which is what the next start will read.
func (b *serveBench) diskConfig(t *testing.T) domain.Config {
	t.Helper()
	return readConfigFile(t, b.configPath)
}

// configVersion reads one of the rotated backups.
func (b *serveBench) configVersion(t *testing.T, version int) domain.Config {
	t.Helper()
	return readConfigFile(t, fmt.Sprintf("%s.%d", b.configPath, version))
}

// readConfigFile parses one configuration file of the bench.
func readConfigFile(t *testing.T, path string) domain.Config {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lecture de %s : %v", path, err)
	}
	var cfg domain.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("%s illisible : %v", path, err)
	}
	return cfg
}

// dropCatalog puts one fixture in the directory the station watches, BEFORE it starts.
func (b *serveBench) dropCatalog(t *testing.T, fixture string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "catalog", fixture))
	if err != nil {
		t.Fatalf("lecture de la fixture %s : %v", fixture, err)
	}
	path := b.watchedFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("création du répertoire surveillé : %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("dépôt de %s : %v", fixture, err)
	}
}

// watchedFile is the file the local drop watches: flv_<n>.csv, derived from
// station.number and from nothing else (§11.2).
func (b *serveBench) watchedFile() string {
	return filepath.Join(b.dataDir, "catalog", "incoming", "flv_2.csv")
}

// awaitCatalogInventory waits for an import to have taken service and returns its
// inventory, as the dashboard publishes it.
//
// It POLLS a route rather than sleeping: the station runs on the system clock here, and the
// only honest way to wait for a poll interval is to ask until the answer changes. The
// budget is generous and never elapses in a passing run.
func (b *serveBench) awaitCatalogInventory(t *testing.T) importAnswer {
	t.Helper()
	deadline := time.Now().Add(startBudget)
	for time.Now().Before(deadline) {
		response := b.get("/admin/api/health")
		var dashboard struct {
			Catalog *importAnswer `json:"catalog"`
		}
		decodeInto(t, response, &dashboard)
		_ = response.Body.Close()
		if dashboard.Catalog != nil && dashboard.Catalog.Result != "" {
			return *dashboard.Catalog
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("aucun catalogue n'a pris service en %s\n%s", startBudget, b.output())
	return importAnswer{}
}

// technicalAnswer is what the Journal page reads on /admin/api/technical.
type technicalAnswer struct {
	Entries []struct {
		OccurredAt string `json:"occurred_at"`
		Level      string `json:"level"`
		Source     string `json:"source"`
		Message    string `json:"message"`
	} `json:"entries"`
}

// awaitTechnicalLines polls until the start-up lines of the station have REACHED THE
// BASE, and never merely until the acts that produce them have run.
//
// The two are not the same instant, and the gap between them is invisible on a fast
// machine. `Hub.logTechnical` hands the entry to a CHANNEL — a non-blocking send, so that
// journalling can never hold up the one goroutine that decides — and `journalWorker.run`
// drains it on ANOTHER goroutine, which is what finally writes it where this route can
// read it. A socket that answers proves the station is UP; it proves nothing about that
// drain having run.
//
// Read straight after `start`, this was a race the repository won locally and lost on a
// loaded CI runner: « le journal technique est vide : l'adaptateur ne lit pas la base ».
// Delaying the drain by 50 ms reproduces it every time, which is how it was found — the
// same instrumentation, and the same cause, as the three station-side readings of a32a9a2.
func (b *serveBench) awaitTechnicalLines(t *testing.T) technicalAnswer {
	t.Helper()
	deadline := time.Now().Add(startBudget)
	for time.Now().Before(deadline) {
		response := b.get("/admin/api/technical")
		var lines technicalAnswer
		decodeInto(t, response, &lines)
		_ = response.Body.Close()
		if len(lines.Entries) != 0 {
			return lines
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("le journal technique est resté vide %s : l'adaptateur ne lit pas la base\n%s",
		startBudget, b.output())
	return technicalAnswer{}
}

// awaitAcknowledgement waits for the watched file to be gone, which is what an
// acknowledgement IS (ADR-004).
//
// It comes after the transaction on purpose — a crash between reading and applying must
// lose nothing — so the dashboard can already carry the inventory while the file is still
// there for a few milliseconds.
func (b *serveBench) awaitAcknowledgement(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(startBudget)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(b.watchedFile()); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s existe encore après %s : l'acquittement est la suppression du fichier",
		b.watchedFile(), startBudget)
}

// --- Small helpers ----------------------------------------------------------

// readBody reads one response as text without closing it: the caller owns the body.
func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("lecture du corps : %v", err)
	}
	return string(raw)
}

// decodeInto reads one JSON body into a value.
func decodeInto(t *testing.T, response *http.Response, into any) {
	t.Helper()
	raw := readBody(t, response)
	if err := json.Unmarshal([]byte(raw), into); err != nil {
		t.Fatalf("corps illisible (%s) : %v", raw, err)
	}
}

// mustJSON serialises one value, or fails the test.
func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("sérialisation : %v", err)
	}
	return raw
}

// quoteJSON renders one JSON string.
func quoteJSON(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

// hasFrenchSentence reports whether an answer carries something a volunteer can read.
//
// It is deliberately crude: what it catches is an answer with no message at all, which is
// what a route that forgot its wording looks like.
func hasFrenchSentence(body string) bool {
	return strings.Contains(body, `"message"`) || strings.Contains(body, `"health"`) ||
		strings.Contains(body, `"connected"`)
}

// keysOf lists the names of a map, for a failure message.
func keysOf(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	return names
}

// stripOptions removes the keys a source does not accept, so that a configuration switched
// from one source to the other still validates (control 41).
func stripOptions(base domain.DriverOptions, keys ...string) domain.DriverOptions {
	out := make(domain.DriverOptions, len(base))
	for key, value := range base {
		out[key] = value
	}
	for _, key := range keys {
		delete(out, key)
	}
	return out
}

// overlayOptions writes a few driver options over the ones a configuration carries.
func overlayOptions(base domain.DriverOptions, overlay map[string]any) domain.DriverOptions {
	out := make(domain.DriverOptions, len(base)+len(overlay))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range overlay {
		raw, err := json.Marshal(value)
		if err != nil {
			panic("option " + key + " : " + err.Error())
		}
		out[key] = raw
	}
	return out
}
