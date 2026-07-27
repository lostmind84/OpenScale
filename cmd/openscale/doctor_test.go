package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openscale/internal/diag"
	"openscale/internal/domain"
	"openscale/internal/platform"
	"openscale/internal/web"
)

// These tests run the REAL doctor against a REAL station, over HTTP, on the composition
// root's own wiring. They are what internal/diag cannot prove on its own:
//
//   - the health client of internal/diag reads the fields internal/web writes. The two
//     declare their own structures on purpose (§14.5, « le DTO est découplé du noyau »), so
//     the only thing that keeps them agreeing is a round trip through the real route;
//   - the configuration control turns GREEN, which needs the registries of this binary and
//     not a test double of them;
//   - diagnostic.zip is served by the station, and it carries no secret.

// TestDoctorReadsARunningStationThroughItsOwnHealthRoute is the contract of §14.5, exercised
// end to end.
func TestDoctorReadsARunningStationThroughItsOwnHealthRoute(t *testing.T) {
	b := newServeBench(t)
	b.start()
	defer func() { _ = b.stop() }()

	report := doctorAgainst(t, b, "")

	// The three controls that can only be answered by the running service (important-11).
	for _, id := range []string{diag.ControlPrintQueue, diag.ControlCatalogSource} {
		control := controlOf(t, report, id)
		if control.Status == diag.StatusUnknown {
			t.Errorf("%s : le service tourne et répond, ce contrôle ne devrait pas être INCONNU — %s",
				id, control.Observed)
		}
	}
	// The listening address is held by the station itself, which the socket-as-lock of §13.4
	// makes the nominal case rather than a fault.
	address := controlOf(t, report, diag.ControlListenAddress)
	if address.Status != diag.StatusPass {
		t.Errorf("adresse d'écoute : %s — %s", address.Status, address.Observed)
	}
	if !strings.Contains(address.Observed, "tenue par ce poste") {
		t.Errorf("le constat devrait dire que l'adresse est tenue par le poste : %s", address.Observed)
	}
	// The station of the bench declares no scale, which is a supported deployment and not an
	// illness (§11.2).
	if scale := controlOf(t, report, diag.ControlScaleRate); scale.Status != diag.StatusNotApplicable {
		t.Errorf("cadence sur un poste sans balance : %s — %s", scale.Status, scale.Observed)
	}
	// The report identifies the station from the FILE, so it works when the service does not.
	if report.Station == 0 || report.Fingerprint == "" {
		t.Errorf("le rapport n'identifie pas le poste : %+v", report.Station)
	}
}

// TestTheConfigurationControlIsGreenOnTheDeliveredFile is why this test lives here and not in
// internal/diag: « configuration valide » means the drivers were checked against the
// registries THIS binary carries (§11.3), and only the composition root has them.
func TestTheConfigurationControlIsGreenOnTheDeliveredFile(t *testing.T) {
	// Le mot de passe est posé, comme sur un poste installé : la configuration LIVRÉE, elle,
	// n'en porte aucun (§14.4), et son absence est un avertissement que le test d'à côté
	// exige. Ce test-ci porte sur autre chose — que les drivers soient vérifiés contre les
	// registres de CE binaire.
	hash, err := web.HashSecret("mot-de-passe-d-administration")
	if err != nil {
		t.Fatalf("empreinte : %v", err)
	}
	b := newServeBench(t, func(cfg *domain.Config) { cfg.Admin.PasswordHash = hash })
	b.start()
	defer func() { _ = b.stop() }()

	control := controlOf(t, doctorAgainst(t, b, ""), diag.ControlConfiguration)
	if control.Status != diag.StatusPass {
		t.Fatalf("configuration livrée : %s — %s\n%s", control.Status, control.Observed, control.Remedy)
	}
	if !strings.Contains(control.Observed, "empreinte") {
		t.Errorf("le constat doit porter l'empreinte, qui est ce qu'on compare entre quatre postes : %s",
			control.Observed)
	}
}

// TestAStationWithNoPasswordSaysSoWithoutRefusingToWeigh.
//
// Un poste livré n'en porte aucun (§14.4), et ce n'est PLUS une faute : une faute le
// mettait hors service, donc il ne pesait pas — alors qu'il ne lui manquait qu'un secret
// d'administration. Reste qu'il faut le dire, et le dire là où va un bénévole coincé :
// « rien ne le disait » est exactement ce qui a enfermé un poste dehors.
func TestAStationWithNoPasswordSaysSoWithoutRefusingToWeigh(t *testing.T) {
	b := newServeBench(t)
	b.start()
	defer func() { _ = b.stop() }()

	control := controlOf(t, doctorAgainst(t, b, ""), diag.ControlConfiguration)
	if control.Status != diag.StatusWarn {
		t.Fatalf("poste sans mot de passe : %s, attendu un avertissement — %s",
			control.Status, control.Observed)
	}
	if !strings.Contains(control.Remedy, "code de secours") {
		t.Errorf("la consigne ne dit pas par où l'on entre :\n%s", control.Remedy)
	}
}

// TestTheDatabaseControlsAreGreenOnTheStationsOwnBase exercises the borrowed handle: the
// controls read the live base and must NOT close it.
func TestTheDatabaseControlsAreGreenOnTheStationsOwnBase(t *testing.T) {
	b := newServeBench(t)
	b.start()
	defer func() { _ = b.stop() }()

	report := doctorAgainst(t, b, "")
	for _, id := range []string{diag.ControlDatabase, diag.ControlMigrations} {
		control := controlOf(t, report, id)
		if control.Status != diag.StatusPass {
			t.Errorf("%s : %s — %s", id, control.Status, control.Observed)
		}
	}
	// Now the archive, which is the path that BORROWS the station's own handle instead of
	// opening a second one. Running it is what exercises borrowedDatabase.Close.
	archive := b.get("/admin/api/diagnostic.zip")
	_, _ = io.Copy(io.Discard, archive.Body)
	archive.Body.Close()

	// The station can still READ ITS BASE afterwards, which is what proves the handle was
	// borrowed and not closed. /healthz would not prove it — it submits one event to the Hub
	// and touches no database — so the assertion goes through the dashboard, whose journal row
	// count comes from a real query and is -1 when there is no readable base.
	response := b.get("/admin/api/health")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("/admin/api/health après le diagnostic = %d", response.StatusCode)
	}
	var health struct {
		Counters struct {
			Journal int `json:"journal_rows_count"`
		} `json:"counters"`
	}
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		t.Fatalf("tableau de bord illisible : %v", err)
	}
	if health.Counters.Journal < 0 {
		t.Fatal("le poste ne peut plus lire son journal : le diagnostic a refermé la base " +
			"sous lui, alors que c'est la STATION qui la possède (§13.4)")
	}
}

// TestDoctorSaysWhyAStationThatIsNotRunningIsNotRunning is the criterion of §18 for lot L8.
func TestDoctorSaysWhyAStationThatIsNotRunningIsNotRunning(t *testing.T) {
	b := newServeBench(t)
	// Nothing is started: this is the station a volunteer runs the command on.
	report := doctorAgainst(t, b, "")

	silent := 0
	for _, id := range []string{diag.ControlPrintQueue, diag.ControlScaleRate, diag.ControlCatalogSource} {
		control := controlOf(t, report, id)
		if control.Status == diag.StatusUnknown {
			silent++
			if control.Remedy == "" {
				t.Errorf("%s : INCONNU sans consigne", id)
			}
		}
	}
	if silent == 0 {
		t.Error("aucun contrôle n'a remarqué que le service ne répond pas")
	}
	// The address is free, and saying so is the finding: the service is not holding it.
	address := controlOf(t, report, diag.ControlListenAddress)
	if address.Status != diag.StatusPass || !strings.Contains(address.Observed, "libre") {
		t.Errorf("adresse d'écoute sur un poste arrêté : %s — %s", address.Status, address.Observed)
	}
	if err := report.Validate(); err != nil {
		t.Fatalf("le rapport se contredit :\n%v", err)
	}
}

// TestTheStationServesAnArchiveWithNoSecretInIt is the security requirement, checked on the
// route a volunteer actually presses — unauthenticated, one button (§15.4, ADR-018).
func TestTheStationServesAnArchiveWithNoSecretInIt(t *testing.T) {
	const password = "mot-de-passe-du-producteur-2026"
	const address = "https://dav.example.org/depots"

	// Les empreintes sont posées ICI et non lues dans le fichier livré : celui-ci ne
	// porte plus aucun secret (§14.4), et un test qui chercherait une chaîne vide dans
	// l'archive ne prouverait rien. Ce sont de VRAIES empreintes, comme un poste installé
	// en porte.
	adminHash, err := web.HashSecret("mot-de-passe-d-administration")
	if err != nil {
		t.Fatalf("empreinte du mot de passe : %v", err)
	}
	recoveryHash, err := web.HashSecret("ABCDEFGH")
	if err != nil {
		t.Fatalf("empreinte du code de secours : %v", err)
	}

	b := newServeBench(t, func(cfg *domain.Config) {
		cfg.Admin.PasswordHash = adminHash
		cfg.Admin.RecoveryCodeHash = recoveryHash
		cfg.Catalog.Type = domain.CatalogSourceWebDAV
		cfg.Catalog.Options = mustOptions(t, cfg.Catalog.Options, map[string]any{
			"url": address, "username": "balance", "password": password,
		})
	})
	b.start()
	defer func() { _ = b.stop() }()

	response := b.get("/admin/api/diagnostic.zip")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/api/diagnostic.zip = %d, attendu 200 sans mot de passe (§15.4)",
			response.StatusCode)
	}
	if disposition := response.Header.Get("Content-Disposition"); !strings.Contains(disposition, "diagnostic.zip") {
		t.Errorf("l'archive doit se télécharger sous son nom : %q", disposition)
	}

	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("lecture de l'archive : %v", err)
	}
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("archive illisible : %v", err)
	}

	secrets := map[string]string{
		password:          "le mot de passe WebDAV",
		address:           "l'adresse privée de la source",
		"dav.example.org": "l'hôte privé de la coopérative",
		adminHash:         "l'empreinte du mot de passe d'administration",
		recoveryHash:      "l'empreinte du code de secours",
	}
	names := make([]string, 0, len(archive.File))
	for _, member := range archive.File {
		names = append(names, member.Name)
		content := readZipMember(t, member)
		for secret, what := range secrets {
			if secret == "" {
				t.Fatal("la configuration livrée ne porte plus d'empreinte : ce test ne prouverait rien")
			}
			if bytes.Contains(content, []byte(secret)) {
				t.Errorf("%s a fui dans %s", what, member.Name)
			}
		}
	}
	for _, want := range []string{"doctor.txt", "doctor.json", "config.redacted.json", "health.json"} {
		if !containsName(names, want) {
			t.Errorf("%s manque à l'archive servie : %v", want, names)
		}
	}
}

// TestTheZipFlagWritesTheFileItAnnounces is the command-line half of the same archive.
func TestTheZipFlagWritesTheFileItAnnounces(t *testing.T) {
	b := newServeBench(t)
	b.start()
	defer func() { _ = b.stop() }()

	target := filepath.Join(t.TempDir(), diagnosticFileName)
	out := &bytes.Buffer{}
	err := runDoctor(context.Background(), []string{
		"--config", b.configPath, "--data", b.dataDir, "--listen", b.address,
		"--output", target,
	}, out)
	// A station whose controls all pass returns nil; one with a failure returns a one-line
	// refusal. Either is acceptable here — what is asserted is the FILE.
	if err != nil && !strings.Contains(err.Error(), "contrôle(s) en échec") {
		t.Fatalf("openscale doctor --output : %v\n%s", err, out.String())
	}
	if _, statErr := os.Stat(target); statErr != nil {
		t.Fatalf("l'archive annoncée n'existe pas : %v\n%s", statErr, out.String())
	}
	if !strings.Contains(out.String(), target) {
		t.Errorf("la sortie doit dire où le fichier a été écrit :\n%s", out.String())
	}
	if !strings.Contains(out.String(), "sans le relire") {
		t.Errorf("la sortie doit dire qu'on peut l'envoyer sans le relire :\n%s", out.String())
	}
}

// TestDoctorRefusesAnUnexpectedArgument keeps the command from silently ignoring a typo.
func TestDoctorRefusesAnUnexpectedArgument(t *testing.T) {
	out := &bytes.Buffer{}
	if err := runDoctor(context.Background(), []string{"zip"}, out); err == nil {
		t.Fatal("« openscale doctor zip » doit être refusé : c'est --zip")
	}
}

// --- Helpers ----------------------------------------------------------------

// doctorAgainst runs the real doctor against the bench's station.
func doctorAgainst(t *testing.T, b *serveBench, listen string) diag.Report {
	t.Helper()
	if listen == "" {
		listen = b.address
	}
	o := doctorOptions{configPath: b.configPath, dataDir: b.dataDir, listen: listen}
	doctor, err := diag.New(doctorSettings(o, platform.NewSystemClock()))
	if err != nil {
		t.Fatalf("construction du doctor : %v", err)
	}
	report := doctor.Run(context.Background())
	if err := report.Validate(); err != nil {
		t.Fatalf("le rapport se contredit :\n%v", err)
	}
	return report
}

// controlOf returns one control of a report.
func controlOf(t *testing.T, report diag.Report, id string) diag.Control {
	t.Helper()
	control, ok := report.Control(id)
	if !ok {
		t.Fatalf("le rapport ne porte aucun contrôle %q", id)
	}
	return control
}

// readZipMember decompresses one member of the served archive.
func readZipMember(t *testing.T, member *zip.File) []byte {
	t.Helper()
	reader, err := member.Open()
	if err != nil {
		t.Fatalf("%s : %v", member.Name, err)
	}
	defer reader.Close()
	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("%s : %v", member.Name, err)
	}
	return content
}

// containsName reports whether the archive carries that member.
func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}
