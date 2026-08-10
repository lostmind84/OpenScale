package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openscale/internal/web"
)

// The three `config` actions that REWRITE the station's own file — migrate, password
// and recovery-code. What they share is the store: five rotating versions, an atomic
// landing, and a station that does not see the change before it restarts.

// TestTheCommandLineOpensAStationNobodyCanLogInTo is the hole this command closes.
//
// The delivered configuration carries no password — §11.5 ships the values of the site,
// not the secrets of one station — so a station straight out of install.ps1 answers 409 on
// its login form, 409 on its recovery form and 401 on every route that writes. It was
// locked out of its own administration, and §14.4 names the way back in.
func TestTheCommandLineOpensAStationNobodyCanLogInTo(t *testing.T) {
	path := copyDelivered(t)
	if before := readJSONConfig(t, path).Admin.PasswordHash; before != "" {
		t.Fatalf("la configuration livrée porte déjà un mot de passe : %q", before)
	}

	out := &strings.Builder{}
	if err := runConfig([]string{"password", path}, strings.NewReader("mot-de-passe-du-poste\n"), out); err != nil {
		t.Fatalf("config password : %v", err)
	}

	after := readJSONConfig(t, path)
	if !web.VerifySecret(after.Admin.PasswordHash, "mot-de-passe-du-poste") {
		t.Fatalf("empreinte écrite = %q : elle ne vérifie pas le mot de passe tapé",
			after.Admin.PasswordHash)
	}
	// The station is asked to restart, because nothing re-reads config.json while the
	// service runs. A command that stayed silent about it would be read as « c'est fait ».
	if !strings.Contains(out.String(), "Redémarrez le service") {
		t.Errorf("la commande ne dit pas qu'il faut redémarrer : %q", out.String())
	}
	// And it touched ONE field: everything the delivered file carried is still there.
	before := readJSONConfig(t, copyDelivered(t))
	after.Admin.PasswordHash, after.ModifiedAt = "", before.ModifiedAt
	if after.Fingerprint() != before.Fingerprint() {
		t.Error("la commande a changé autre chose que le mot de passe")
	}
}

// TestAPasswordTooShortIsRefusedByBOTHDoors: the floor is the same on the terminal and on
// the recovery form, or the rule would depend on which door somebody came through.
//
// The length is DERIVED from web.MinPasswordLength and never spelled here. This bench used
// to feed a five-character literal and to call it « cinq caractères » in its own failure
// message: the day the owner moved the floor, it went red on a command that was doing
// exactly what it had been asked to do. What has to hold is not a figure — it is that a
// floor exists, that it is the one both doors read, and that it lets the floor itself
// through.
func TestAPasswordTooShortIsRefusedByBOTHDoors(t *testing.T) {
	path := copyDelivered(t)
	tooShort := strings.Repeat("a", web.MinPasswordLength-1)
	err := runConfig([]string{"password", path}, strings.NewReader(tooShort+"\n"), &strings.Builder{})
	if err == nil {
		t.Fatalf("%q, un caractère sous le plancher, a été accepté", tooShort)
	}
	if hash := readJSONConfig(t, path).Admin.PasswordHash; hash != "" {
		t.Fatal("un mot de passe refusé a tout de même été écrit")
	}

	// And the floor EXACTLY goes through: a bench that only knew what is refused would stay
	// green on a command that refuses everything.
	exact := strings.Repeat("a", web.MinPasswordLength)
	if err := runConfig([]string{"password", path}, strings.NewReader(exact+"\n"),
		&strings.Builder{}); err != nil {
		t.Fatalf("%q, le plancher exactement : %v", exact, err)
	}
	if !web.VerifySecret(readJSONConfig(t, path).Admin.PasswordHash, exact) {
		t.Fatalf("le mot de passe %q, au plancher exact, n'a pas été écrit", exact)
	}
}

// TestTheTerminalCountsCHARACTERSAndNotBYTES is the terminal half of the counting unit.
//
// The screen's half is TestTheRecoveryFormCountsCODEPOINTSAndNotBYTES, in internal/web,
// where the door that used to count bytes lives. Neither door can drive the other from its
// own package, so what makes « la même valeur aux deux endroits » true is those two benches
// plus TestTheTwoDoorsNAMETheFloorInsteadOfSpellingIt, which holds that both compare
// against the one constant.
func TestTheTerminalCountsCHARACTERSAndNotBYTES(t *testing.T) {
	// « é » is ONE character and TWO bytes in UTF-8, so a password one character short of
	// the floor is already longer than the floor in bytes. A door counting bytes lets it in;
	// a door counting characters does not; and the same password then opens one and not the
	// other.
	tooShort := strings.Repeat("é", web.MinPasswordLength-1)
	if len(tooShort) < web.MinPasswordLength {
		t.Fatalf("prémisse fausse : %q fait %d octets, il n'atteint pas le plancher et ne "+
			"prouve donc rien de l'unité comptée", tooShort, len(tooShort))
	}

	path := copyDelivered(t)
	if err := runConfig([]string{"password", path}, strings.NewReader(tooShort+"\n"),
		&strings.Builder{}); err == nil {
		t.Fatalf("%q, sous le plancher en caractères, a été accepté : les octets ont été comptés",
			tooShort)
	}

	exact := strings.Repeat("é", web.MinPasswordLength)
	if err := runConfig([]string{"password", path}, strings.NewReader(exact+"\n"),
		&strings.Builder{}); err != nil {
		t.Fatalf("%q, le plancher exactement en caractères accentués : %v", exact, err)
	}
	if !web.VerifySecret(readJSONConfig(t, path).Admin.PasswordHash, exact) {
		t.Fatalf("le mot de passe accentué %q n'a pas été écrit", exact)
	}
}

// TestAByteOrderMarkIsNotPartOfThePassword.
//
// A pipe into a native process, from a PowerShell console in chcp 65001, puts EF BB BF in
// front of the standard input — which is how this command is fed when a responsible person
// scripts an installation. Hashing that mark walls the station off: the password typed back
// afterwards, at the screen or at this same console, does not carry it and never verifies,
// and neither end shows a reason.
func TestAByteOrderMarkIsNotPartOfThePassword(t *testing.T) {
	path := copyDelivered(t)
	password := strings.Repeat("a", web.MinPasswordLength)
	if err := runConfig([]string{"password", path},
		strings.NewReader("\ufeff"+password+"\r\n"), &strings.Builder{}); err != nil {
		t.Fatalf("config password : %v", err)
	}

	hash := readJSONConfig(t, path).Admin.PasswordHash
	if !web.VerifySecret(hash, password) {
		t.Fatalf("l'empreinte écrite ne vérifie pas %q : la marque d'ordre des octets a été "+
			"hachée avec le mot de passe", password)
	}
	if web.VerifySecret(hash, "\ufeff"+password) {
		t.Fatal("la marque d'ordre des octets fait partie du mot de passe écrit")
	}
}

// TestTheRecoveryCodeIsPrintedOnceAndStoredHashed (§14.4, important-10).
//
// It is generated AT INSTALLATION, and install.ps1 has no way to produce an argon2id
// hash: this command is where the eight characters of the installation sheet come from.
func TestTheRecoveryCodeIsPrintedOnceAndStoredHashed(t *testing.T) {
	path := copyDelivered(t)
	out := &strings.Builder{}
	if err := runConfig([]string{"recovery-code", path}, nil, out); err != nil {
		t.Fatalf("config recovery-code : %v", err)
	}

	code := codePrintedBy(t, out.String())
	if len(code) != web.RecoveryCodeLength {
		t.Fatalf("code affiché = %q, attendu %d caractères", code, web.RecoveryCodeLength)
	}
	hash := readJSONConfig(t, path).Admin.RecoveryCodeHash
	if !web.VerifySecret(hash, code) {
		t.Fatalf("l'empreinte écrite ne vérifie pas le code affiché %q", code)
	}
	// The clear code is nowhere in the file: the only copy is the printed sheet.
	if raw, err := os.ReadFile(path); err != nil || strings.Contains(string(raw), code) {
		t.Fatal("le code de secours est écrit en clair dans la configuration")
	}

	// Drawn a second time, it says what it costs: the sheet already in the folder is wrong.
	second := &strings.Builder{}
	if err := runConfig([]string{"recovery-code", path}, nil, second); err != nil {
		t.Fatalf("second tirage : %v", err)
	}
	if !strings.Contains(second.String(), "ne fonctionne plus") {
		t.Errorf("un second tirage ne prévient pas que l'ancien code est mort : %q", second.String())
	}
	if web.VerifySecret(readJSONConfig(t, path).Admin.RecoveryCodeHash, code) {
		t.Error("le premier code de secours ouvre encore la porte")
	}
}

// codePrintedBy reads the eight characters out of what the command said.
func codePrintedBy(t *testing.T, printed string) string {
	t.Helper()
	const marker = "Code de secours de ce poste : "
	index := strings.Index(printed, marker)
	if index < 0 {
		t.Fatalf("le code de secours n'est pas affiché : %q", printed)
	}
	return strings.TrimSpace(strings.SplitN(printed[index+len(marker):], "\n", 2)[0])
}

// TestMigrateWritesOnceAndSaysSoTheSecondTime: the command is what update.ps1 and update.sh
// call, so running it twice on the same station -- two updates in a row -- must be a
// no-operation the second time, and must not rotate config.json.1 over a version that
// mattered.
func TestMigrateWritesOnceAndSaysSoTheSecondTime(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	if err := os.WriteFile(path, []byte(
		`{"version":1,"station":{"number":2},"ui":{"tile_size":"large"}}`), 0o644); err != nil {
		t.Fatalf("écriture : %v", err)
	}

	var first bytes.Buffer
	if err := runConfig([]string{"migrate", path}, nil, &first); err != nil {
		t.Fatalf("première migration : %v", err)
	}
	if !strings.Contains(first.String(), "tile_size") {
		t.Errorf("la première migration ne dit pas ce qu'elle a changé :\n%s", first.String())
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf("la version d'avant n'a pas été gardée : %v", err)
	}

	migrated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("relecture : %v", err)
	}

	var second bytes.Buffer
	if err := runConfig([]string{"migrate", path}, nil, &second); err != nil {
		t.Fatalf("seconde migration : %v", err)
	}
	if !strings.Contains(second.String(), "rien à faire") {
		t.Errorf("la seconde migration n'annonce pas qu'elle ne fait rien :\n%s", second.String())
	}
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("relecture : %v", err)
	}
	if string(again) != string(migrated) {
		t.Errorf("la seconde migration a réécrit le fichier :\n%s\n%s", migrated, again)
	}
}

// TestMigrateLeavesARefusedKeyInPlace: what this binary will not guess stays where it is,
// and the command says so with a non-zero status so update.ps1 can show it.
func TestMigrateLeavesARefusedKeyInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(
		`{"version":1,"barcode":{"weight_decimals":3}}`), 0o644); err != nil {
		t.Fatalf("écriture : %v", err)
	}

	var out bytes.Buffer
	err := runConfig([]string{"migrate", path}, nil, &out)
	if err == nil {
		t.Fatal("une clé refusée doit donner un code de retour non nul")
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("relecture : %v", readErr)
	}
	if !strings.Contains(string(after), "weight_decimals") {
		t.Errorf("la clé refusée a été retirée quand même : %s", after)
	}
}

// TestMigrateMixesTwoKindsOfRefusalWithoutDuplicating watches a COINCIDENCE, not a rule:
// migrateConfig's deduplication between Migrate's notes and cfg.Retired() works because
// carryCoefficientToDiscount (configmigration.go) and scanRetired (config.go) build the
// SAME dotted path for the same field -- two independent functions that never cite one
// another. If either one changes its template one day without the other following, the
// deduplication breaks IN SILENCE: a key would show up twice, under two different
// messages, with no test noticing. This one mixes both families of refusal in the same
// file to catch that.
//
// pricing.tiers[0].coef_num AND pricing.tiers[0].coef_den both stay in the document
// (carryCoefficientToDiscount only removes them on the branch that succeeds), so
// scanRetired finds both of them -- but Migrate only ever wrote ONE note, under
// "pricing.tiers[0].coef_num". The deduplication therefore cancels only that one: coef_den
// gets its own line, with its own reason (ADR-034, "there is no more denominator"). That is
// NOT a duplicated message -- it is a third point, on a third JSON key that genuinely still
// stands -- and this test pins that too, so a future reader does not mistake it for an
// unfixed bug.
func TestMigrateMixesTwoKindsOfRefusalWithoutDuplicating(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	before := []byte(`{"version":1,"pricing":{"tiers":[
		{"code":"X","label":"X","abbrev":"X","coef_num":3,"coef_den":7,"rank":1}
	]},"barcode":{"weight_decimals":3}}`)
	if err := os.WriteFile(path, before, 0o644); err != nil {
		t.Fatalf("écriture : %v", err)
	}

	var out bytes.Buffer
	err := runConfig([]string{"migrate", path}, nil, &out)
	if err == nil {
		t.Fatal("un fichier portant deux familles de clés refusées doit rendre un code non nul")
	}

	printed := out.String()
	for _, key := range []string{
		"pricing.tiers[0].coef_num", "pricing.tiers[0].coef_den", "barcode.weight_decimals",
	} {
		if count := strings.Count(printed, key); count != 1 {
			t.Errorf("%s apparaît %d fois dans la sortie, attendu 1 :\n%s", key, count, printed)
		}
	}
	if !strings.Contains(printed, "3 changement(s)") {
		t.Errorf("le compte de points refusés n'est pas 3 (coef_num, coef_den, weight_decimals) :\n%s", printed)
	}
	if !strings.Contains(err.Error(), "3 point(s)") {
		t.Errorf("l'erreur ne nomme pas les 3 points refusés : %v", err)
	}

	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("relecture : %v", readErr)
	}
	if string(after) != string(before) {
		t.Errorf("le fichier a été modifié alors qu'un point reste refusé :\navant : %s\naprès : %s",
			before, after)
	}
}
