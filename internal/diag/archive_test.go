package diag

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// What diagnostic.zip has to hold, and above all what it must NEVER carry away. The
// assertions do not read the code and do not trust the redaction: they produce the archive,
// open it, decompress every member and look for the values themselves. The bench, the
// journal double and the archive readers are in archive_harness_test.go.

// The secrets this station carries. They are the ones a real installation has: the two
// argon2id strings of the delivered configuration file, a WebDAV password, and a private
// address with the credentials embedded in it — the form net/http quotes verbatim in an
// error message.
const (
	secretWebDAVPassword = "s3cr3t-du-producteur"
	secretWebDAVURL      = "https://balance:s3cr3t-du-producteur@dav.example.org/depots"
	secretWebDAVHost     = "dav.example.org"
)

// TestTheArchiveCarriesNoSecretAtAll is THE security test of this package.
//
// It does not read the code and it does not trust the redaction: it produces the archive,
// opens it, decompresses every member, and looks for the values themselves. §15.4 gives this
// file « un seul bouton, sans mot de passe » and sends it out of the shop; anything private
// inside it is published.
func TestTheArchiveCarriesNoSecretAtAll(t *testing.T) {
	b := newArchiveBench(t)
	archive := b.build()

	forbidden := map[string]string{
		secretWebDAVPassword: "le mot de passe WebDAV",
		secretWebDAVURL:      "l'URL complète de la source, identifiants compris",
		secretWebDAVHost:     "l'hôte privé de la coopérative",
		benchPasswordHash:    "l'empreinte du mot de passe d'administration",
		benchRecoveryHash:    "l'empreinte du code de secours de la fiche d'installation",
	}

	for _, member := range archive.File {
		content := readMember(t, member)
		for secret, what := range forbidden {
			if bytes.Contains(content, []byte(secret)) {
				t.Errorf("%s a fui dans %s : %s", what, member.Name, quoteAround(content, secret))
			}
		}
		for secret, what := range forbidden {
			if strings.Contains(member.Name, secret) {
				t.Errorf("%s a fui dans le NOM d'un membre : %s (%s)", what, member.Name, secret)
			}
		}
	}
}

// TestTheLeakTestWouldNoticeTheSecretsItLooksFor proves the test above is not vacuous.
//
// A test that searches for values the archive never had any occasion to carry would pass on
// an archive that leaks everything. This one asserts that the station really does carry the
// secrets, in the very places the archive draws from — the configuration, the technical
// journal, and the payload the running service answered.
func TestTheLeakTestWouldNoticeTheSecretsItLooksFor(t *testing.T) {
	b := newArchiveBench(t)

	raw, err := json.Marshal(b.cfg)
	if err != nil {
		t.Fatalf("sérialisation de la configuration : %v", err)
	}
	for _, secret := range []string{secretWebDAVPassword, secretWebDAVURL, benchPasswordHash, benchRecoveryHash} {
		if !bytes.Contains(raw, []byte(secret)) {
			t.Errorf("la configuration du banc ne porte pas %q : le test d'étanchéité ne prouverait rien", secret)
		}
	}
	if !strings.Contains(b.journal.technicalDetail(), secretWebDAVURL) {
		t.Error("le journal technique du banc ne porte pas l'URL : or c'est par là que la fuite arrive")
	}
	if !bytes.Contains(b.service.health.Raw, []byte(secretWebDAVHost)) {
		t.Error("la réponse du service ne porte pas l'hôte : or health.json la recopie telle quelle")
	}
}

// TestTheArchiveIsReadableAndComplete is the second half of the requirement: an archive
// nobody can open, or that is missing the member somebody needs, is not a diagnosis.
func TestTheArchiveIsReadableAndComplete(t *testing.T) {
	b := newArchiveBench(t)
	archive := b.build()

	want := []string{
		"README.txt", "doctor.txt", "doctor.json", "system.json",
		"config.redacted.json", "health.json",
		"weighings.csv", "technical.csv", "imports.csv", "catalog.json",
		"frames.txt", "errors.txt",
	}
	present := map[string]bool{}
	for _, member := range archive.File {
		present[member.Name] = true
		if len(readMember(t, member)) == 0 {
			t.Errorf("%s est vide : un membre vide se lit comme une absence", member.Name)
		}
	}
	for _, name := range want {
		if !present[name] {
			t.Errorf("%s manque à l'archive", name)
		}
	}

	// The report member must decode back into a report with ALL its controls: the
	// archive is read by a support tool, not only by a human.
	var report Report
	if err := json.Unmarshal(readNamed(t, archive, "doctor.json"), &report); err != nil {
		t.Fatalf("doctor.json illisible : %v", err)
	}
	if len(report.Controls) != len(ControlOrder) {
		t.Errorf("doctor.json porte %d contrôles, attendu %d",
			len(report.Controls), len(ControlOrder))
	}

	// The three CSV members must parse with the separator they were written with, and carry
	// their header plus their rows.
	for name, wantRows := range map[string]int{"weighings.csv": 2, "technical.csv": 2, "imports.csv": 1} {
		rows := readCSV(t, archive, name)
		if len(rows) != wantRows+1 {
			t.Errorf("%s : %d lignes, attendu %d plus l'en-tête", name, len(rows), wantRows)
		}
	}

	// errors.txt is written even when nothing failed, so that a reader can tell « rien à
	// signaler » from a truncated archive.
	if notes := string(readNamed(t, archive, "errors.txt")); !strings.Contains(notes, "rien à signaler") {
		t.Errorf("errors.txt devrait dire que tout a été écrit :\n%s", notes)
	}
}

// TestTheRedactedConfigurationKeepsWhatIsNotSecret is the other half of redaction: an
// archive that removed the whole configuration would protect the station and diagnose
// nothing.
func TestTheRedactedConfigurationKeepsWhatIsNotSecret(t *testing.T) {
	b := newArchiveBench(t)
	archive := b.build()
	redacted := string(readNamed(t, archive, "config.redacted.json"))

	for _, want := range []string{`"number": 2`, `"La Cagette"`, `"username": "balance"`, `"webdav"`} {
		if !strings.Contains(redacted, want) {
			t.Errorf("la configuration caviardée a perdu %s, qui n'est pas un secret :\n%s", want, redacted)
		}
	}
	// The scheme survives, and it decides a remedy: an http source on a network somebody
	// believed was TLS-protected is a finding that cannot be seen otherwise.
	if !strings.Contains(redacted, `"https://`+Marker) {
		t.Errorf("le schéma de l'URL doit survivre au caviardage :\n%s", redacted)
	}
	if !strings.Contains(redacted, Marker) {
		t.Errorf("un secret retiré doit se voir : sans marqueur, « pas de mot de passe » et « mot de "+
			"passe retiré » se lisent pareil :\n%s", redacted)
	}
}

// TestTheRedactedConfigurationSaysWhichBlockIsNotTheStations.
//
// A block that will not decode falls back on the neutral profile, so config.redacted.json
// carried the FACTORY grid presented as the shop's own — support read it six months later,
// remotely, with no way of telling. The member stays (a station with 13 readable blocks out
// of 14 is exactly the one support needs to see), it stays valid JSON, and _readme carries
// the warning: that field is the mode d'emploi JSON cannot hold as a comment, and it is out
// of the fingerprint by construction, so it disturbs nothing that gets compared.
func TestTheRedactedConfigurationSaysWhichBlockIsNotTheStations(t *testing.T) {
	b := newArchiveBench(t)
	b.damage = func(path string) { substituteAnUnreadablePricingBlock(t, path) }
	archive := b.build()

	raw := readNamed(t, archive, "config.redacted.json")

	// Valid JSON first: an out-of-band header would have broken every machine reading it.
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("config.redacted.json n'est plus du JSON valide : %v\n%s", err, raw)
	}
	readme, _ := document["_readme"].(string)
	if !strings.Contains(readme, "pricing") {
		t.Errorf("_readme ne nomme pas le bloc qui n'a pas été lu : %q", readme)
	}
	// Case-insensitive: the sentence shouts « D'USINE », and the assertion is on the fact,
	// not on the typography.
	if !strings.Contains(strings.ToLower(readme), "usine") {
		t.Errorf("_readme ne dit pas que ce qui figure à la place est la configuration "+
			"d'usine : %q", readme)
	}
	// The warning reads FIRST: the mode d'emploi the file already carried is kept, behind it.
	if !strings.HasPrefix(readme, "ATTENTION") {
		t.Errorf("l'avertissement ne se lit pas en premier : %q", readme)
	}
	if !strings.Contains(readme, "Modifiable depuis l'écran d'administration") {
		t.Errorf("l'avertissement a écrasé le mode d'emploi que le fichier portait : %q", readme)
	}

	// The station is still described: removing the member would have protected nothing and
	// diagnosed nothing.
	if !strings.Contains(string(raw), `"number": 2`) {
		t.Errorf("la configuration caviardée a perdu les blocs qui, eux, ont été lus :\n%s", raw)
	}

	// THE guarantee of this member, asserted on the damaged file too: the redaction is
	// untouched by any of the above.
	for secret, what := range map[string]string{
		benchPasswordHash:    "l'empreinte du mot de passe d'administration",
		benchRecoveryHash:    "l'empreinte du code de secours",
		secretWebDAVPassword: "le mot de passe WebDAV",
		secretWebDAVURL:      "l'URL complète de la source",
	} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Errorf("%s a fui dans config.redacted.json : %s", what, quoteAround(raw, secret))
		}
	}
}

// TestAnArchiveIsStillProducedWhenEverythingIsBroken is the second rule of the file: the
// mornings somebody presses this button are exactly the mornings something is broken.
func TestAnArchiveIsStillProducedWhenEverythingIsBroken(t *testing.T) {
	b := newArchiveBench(t)
	b.openErr = errors.New("base verrouillée")
	b.journalFails = true
	b.service.silence()
	b.labels = ""

	archive := b.build()
	notes := string(readNamed(t, archive, "errors.txt"))
	for _, want := range []string{"health.json", "weighings.csv", "technical.csv"} {
		if !strings.Contains(notes, want) {
			t.Errorf("errors.txt ne dit pas que %s manque :\n%s", want, notes)
		}
	}
	// The report is still there, and it is the member that says why everything else is not.
	if report := readNamed(t, archive, "doctor.txt"); len(report) == 0 {
		t.Error("le rapport doit être là même quand tout le reste manque")
	}
}

// TestTheArchiveKeepsTheLastLabelsAndNoMore is the count of §15.4: five .sbpl, three PNG.
func TestTheArchiveKeepsTheLastLabelsAndNoMore(t *testing.T) {
	b := newArchiveBench(t)
	archive := b.build()

	var sbpl, png []string
	for _, member := range archive.File {
		switch {
		case strings.HasSuffix(member.Name, ".sbpl"):
			sbpl = append(sbpl, member.Name)
		case strings.HasSuffix(member.Name, ".png"):
			png = append(png, member.Name)
		}
	}
	if len(sbpl) != archivedSBPL {
		t.Errorf("%d fichiers .sbpl, §15.4 en demande %d : %v", len(sbpl), archivedSBPL, sbpl)
	}
	if len(png) != archivedLabelImages {
		t.Errorf("%d PNG, §15.4 en demande %d : %v", len(png), archivedLabelImages, png)
	}
	// The newest, not the first alphabetically: they are the labels of the complaint
	// somebody is calling about.
	if !strings.Contains(strings.Join(sbpl, " "), "label-07.sbpl") {
		t.Errorf("la dernière étiquette capturée manque : %v", sbpl)
	}
}

// TestTheArchiveNamesWhatItDoesNotContain is what makes it sendable without being reread.
func TestTheArchiveNamesWhatItDoesNotContain(t *testing.T) {
	archive := newArchiveBench(t).build()
	readme := string(readNamed(t, archive, "README.txt"))

	for _, want := range []string{"Aucun mot de passe", "adresse privée", "sans le relire"} {
		if !strings.Contains(readme, want) {
			t.Errorf("le README doit porter « %s » :\n%s", want, readme)
		}
	}
}
