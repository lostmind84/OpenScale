package deploy

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// What is actually DELIVERED: the scheduled task that reopens the client screen on its
// own, the archive of §17.2 with everything it has to carry, and the documentation a
// volunteer will have in front of them. Three artefacts nobody ever reads again until one
// of them is missing.

// --- The scheduled task -------------------------------------------------------------

// scheduledTask is the part of the task XML this test reads.
type scheduledTask struct {
	Triggers struct {
		Logon struct {
			UserID string `xml:"UserId"`
		} `xml:"LogonTrigger"`
	} `xml:"Triggers"`
	Principals struct {
		Principal struct {
			UserID    string `xml:"UserId"`
			LogonType string `xml:"LogonType"`
			RunLevel  string `xml:"RunLevel"`
		} `xml:"Principal"`
	} `xml:"Principals"`
	Settings struct {
		ExecutionTimeLimit         string `xml:"ExecutionTimeLimit"`
		MultipleInstancesPolicy    string `xml:"MultipleInstancesPolicy"`
		Enabled                    string `xml:"Enabled"`
		DisallowStartIfOnBatteries string `xml:"DisallowStartIfOnBatteries"`
	} `xml:"Settings"`
	Actions struct {
		Exec struct {
			Command   string `xml:"Command"`
			Arguments string `xml:"Arguments"`
		} `xml:"Exec"`
	} `xml:"Actions"`
}

// TestTheKioskTaskIsWhatMakesTheScreenComeBackAlone reads the scheduled task the way
// Windows will.
//
// Every assertion below is one way the criterion of §18 fails silently: a task that needs
// a password stops working the day it changes, a task with the default three-day execution
// limit closes the client screen on the fourth day of continuous opening, and a task that
// runs elevated makes a self-service station an administrator session.
func TestTheKioskTaskIsWhatMakesTheScreenComeBackAlone(t *testing.T) {
	raw := readFile(t, filepath.Join("windows", "openscale-kiosk.xml"))
	var task scheduledTask
	if err := xml.Unmarshal([]byte(raw), &task); err != nil {
		t.Fatalf("openscale-kiosk.xml n'est pas un XML exploitable : %v", err)
	}

	if task.Principals.Principal.LogonType != "InteractiveToken" {
		t.Fatalf("LogonType=%q : InteractiveToken est ce qui évite de fournir un mot de passe "+
			"à schtasks — une tâche enregistrée avec un mot de passe cesse de démarrer le jour "+
			"où il change", task.Principals.Principal.LogonType)
	}
	if task.Principals.Principal.RunLevel == "HighestAvailable" {
		t.Error("RunLevel=HighestAvailable : le kiosque tourne sans privilèges")
	}
	if task.Settings.ExecutionTimeLimit != "PT0S" {
		t.Fatalf("ExecutionTimeLimit=%q : la valeur par défaut de Windows arrêterait l'écran "+
			"client au bout de trois jours d'ouverture continue", task.Settings.ExecutionTimeLimit)
	}
	if task.Settings.MultipleInstancesPolicy != "IgnoreNew" {
		t.Errorf("MultipleInstancesPolicy=%q : deux superviseurs, c'est deux navigateurs qui se "+
			"relancent l'un l'autre", task.Settings.MultipleInstancesPolicy)
	}
	if task.Settings.DisallowStartIfOnBatteries != "false" {
		t.Errorf("DisallowStartIfOnBatteries=%q : sur un poste derrière un onduleur, Windows "+
			"refuserait de lancer l'écran client", task.Settings.DisallowStartIfOnBatteries)
	}
	if task.Actions.Exec.Arguments != "kiosk" {
		t.Fatalf("la tâche lance %q, attendu la sous-commande kiosk", task.Actions.Exec.Arguments)
	}
	if task.Triggers.Logon.UserID == "" {
		t.Error("aucun déclencheur d'ouverture de session : le poste ne reviendrait pas seul sur l'écran client")
	}
}

// TestEveryPlaceholderOfTheTaskIsSubstitutedByTheInstaller catches the drift that leaves a
// station launching a program called « %OPENSCALE_BINARY% ».
func TestEveryPlaceholderOfTheTaskIsSubstitutedByTheInstaller(t *testing.T) {
	raw := readFile(t, filepath.Join("windows", "openscale-kiosk.xml"))
	installer := readFile(t, filepath.Join("windows", "install.ps1"))

	placeholders := regexp.MustCompile(`%OPENSCALE_[A-Z_]+%`).FindAllString(raw, -1)
	if len(placeholders) == 0 {
		t.Fatal("le XML ne porte aucun marqueur : il contient donc un chemin en dur")
	}
	for _, placeholder := range placeholders {
		if !strings.Contains(installer, placeholder) {
			t.Errorf("le marqueur %s du XML n'est substitué par aucune ligne de install.ps1 : "+
				"la tâche lancerait un programme de ce nom", placeholder)
		}
	}
}

// TestTheDeliveredArchiveHasEverythingSection17_2Lists is the packaging half: a volunteer
// copies one archive, and every file §17.2 names has to be in it.
//
// The archive is built by `make dist`, which this test does not run — building three
// targets takes a minute. What it checks is the SOURCE of each member: the file exists in
// the repository, or the Makefile knows how to produce it.
func TestTheDeliveredArchiveHasEverythingSection17_2Lists(t *testing.T) {
	makefile := readFile(t, filepath.Join("..", "Makefile"))
	for what, needle := range map[string]string{
		"les scripts et les unités de deploy/": "deploy/",
		"la notice d'installation":             "INSTALLATION.md",
		"le guide de dépannage":                "TROUBLESHOOTING.md",
		"les empreintes des fichiers":          "SHA256SUMS",
		"la configuration livrée":              "config-lacagette.json",
		"la licence et les composants tiers":   "THIRD-PARTY.md",
	} {
		if !strings.Contains(makefile, needle) {
			t.Errorf("la cible release du Makefile n'emporte pas %s (« %s » absent), que §17.2 liste",
				what, needle)
		}
	}
	// The delivered configuration is PRODUCED by the binary and not copied: §17.2 says
	// « SANS le bloc matériel », and a straight copy of the development file would ship
	// the COM8 and the SATO WS408_2 of this machine — two values no station of the fleet
	// may inherit, and which would break the fingerprint comparison of §15.5.
	if !strings.Contains(makefile, "config export") {
		t.Error("la cible release recopie config-lacagette.json au lieu de l'EXPORTER : " +
			"l'archive emporterait le port série et la file d'impression du poste de développement")
	}
	for _, path := range []string{
		filepath.Join("windows", "install.ps1"),
		filepath.Join("windows", "uninstall.ps1"),
		filepath.Join("windows", "update.ps1"),
		filepath.Join("windows", "harden.ps1"),
		filepath.Join("windows", "openscale-kiosk.xml"),
		filepath.Join("windows", "start.bat"),
		filepath.Join("windows", "common.ps1"),
		filepath.Join("linux", "openscale.service"),
		filepath.Join("linux", "openscale-kiosk.service"),
		filepath.Join("linux", "99-openscale.rules"),
		filepath.Join("linux", "49-openscale-reboot.rules"),
		filepath.Join("linux", "install.sh"),
		filepath.Join("linux", "update.sh"),
		filepath.Join("linux", "uninstall.sh"),
		filepath.Join("linux", "bootstrap.sh"),
		filepath.Join("..", "INSTALLATION.md"),
		filepath.Join("..", "TROUBLESHOOTING.md"),
		filepath.Join("..", "testdata", "config-lacagette.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s manque au livrable : %v", path, err)
		}
	}
}

// TestTheDocumentationIsWrittenForAVolunteer checks what can be checked about prose: that
// the two documents start from what somebody SEES.
//
// TROUBLESHOOTING.md has to be navigable by symptom — « l'écran est noir », « ça
// n'imprime plus », « les prix sont faux » — because a volunteer does not know the codes.
// The codes come after, as a way of confirming.
func TestTheDocumentationIsWrittenForAVolunteer(t *testing.T) {
	troubleshooting := readFile(t, filepath.Join("..", "TROUBLESHOOTING.md"))
	for _, symptom := range []string{
		"écran est noir", "n'imprime plus", "prix", "balance", "catalogue",
	} {
		if !strings.Contains(strings.ToLower(troubleshooting), strings.ToLower(symptom)) {
			t.Errorf("TROUBLESHOOTING.md ne parle pas du symptôme « %s »", symptom)
		}
	}
	// The first heading a reader meets must be a symptom, not a code: a document that
	// opens on ERR-SCL-02 is a document written for whoever wrote the code.
	firstHeading := ""
	for _, line := range strings.Split(troubleshooting, "\n") {
		if strings.HasPrefix(line, "## ") {
			firstHeading = line
			break
		}
	}
	if regexp.MustCompile(`ERR-[A-Z]+-\d+`).MatchString(firstHeading) {
		t.Errorf("le premier titre de TROUBLESHOOTING.md est un code (%q) : un bénévole voit un "+
			"symptôme, pas un code", firstHeading)
	}

	installation := readFile(t, filepath.Join("..", "INSTALLATION.md"))
	for _, step := range []string{
		"install.ps1", "redémarr", "empreinte", "15 minutes", "SmartScreen",
	} {
		if !strings.Contains(strings.ToLower(installation), strings.ToLower(step)) {
			t.Errorf("INSTALLATION.md ne parle pas de « %s »", step)
		}
	}
}

// TestTheFifteenMinutesAreCountedAndNotClaimed keeps the promise of §15.5 measurable.
//
// « Un bénévole installe un poste seul en 15 minutes » is the criterion of L8. A document
// that asserted it without counting would be a document that discovers on site that it
// takes forty. INSTALLATION.md therefore carries a table of steps with a duration each,
// and the sum has to be stated.
func TestTheFifteenMinutesAreCountedAndNotClaimed(t *testing.T) {
	installation := readFile(t, filepath.Join("..", "INSTALLATION.md"))
	minutes := regexp.MustCompile(`(?m)^\|.*?\|\s*(\d+)\s*(?:min|minutes?)\b`).FindAllStringSubmatch(installation, -1)
	if len(minutes) < 5 {
		t.Fatalf("INSTALLATION.md ne chiffre que %d étapes : les 15 minutes seraient une "+
			"affirmation, pas un compte", len(minutes))
	}
	total := 0
	for _, match := range minutes {
		value, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("durée illisible %q", match[1])
		}
		total += value
	}
	if !strings.Contains(installation, fmt.Sprintf("%d minutes", total)) &&
		!strings.Contains(installation, fmt.Sprintf("%d min", total)) {
		t.Errorf("les étapes totalisent %d minutes, et ce total n'est écrit nulle part dans "+
			"INSTALLATION.md : le lecteur ne peut pas vérifier la promesse", total)
	}
	t.Logf("les étapes chiffrées de INSTALLATION.md totalisent %d minutes", total)
}
