package diag

import (
	"strings"
	"testing"
)

func TestTheTerminalOutputPutsEachRemedyUnderItsOwnControl(t *testing.T) {
	b := newBench(t)
	b.machine.autoLogon.Enabled = false
	b.machine.space.FreeBytes = 32 << 20
	text := reportHead(t, b.run())

	// Not collected at the bottom: nobody would join a remedy back to its cause. The arrow
	// of the failing control must appear BEFORE the next control's line.
	autoLogonAt := strings.Index(text, "Redémarrage sans intervention")
	arrowAt := strings.Index(text[autoLogonAt:], "→")
	nextAt := strings.Index(text[autoLogonAt:], "Droits d'écriture")
	if arrowAt < 0 || nextAt < 0 || arrowAt > nextAt {
		t.Errorf("la consigne du 3ᵉ contrôle doit être sous sa ligne, pas plus loin :\n%s", text)
	}
	if !strings.Contains(text, "ERR-SYS-08") {
		t.Errorf("le code doit être lisible dans la sortie terminal :\n%s", text)
	}
}

func TestTheHeadOfTheReportCarriesWhatASupportCallAsksFirst(t *testing.T) {
	text := reportHead(t, newBench(t).run())
	for _, want := range []string{
		"poste 2 « Poste 2 — fruits » — La Cagette",
		"1.0.0-test", "abc1234",
		"windows/amd64", "hôte PESEE-2", "allumé depuis 30 h",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("l'en-tête ne porte pas « %s » :\n%s", want, text)
		}
	}
}

func TestTheLastLineIsWhatAVolunteerReadsOutOverTheTelephone(t *testing.T) {
	green := newBench(t)
	// The neutral profile leaves two controls without object and one partially checked, so
	// the summary of a « perfect » bench is the « nothing established » one. What matters is
	// that each of the four summaries is reachable and says something different.
	if summary := green.run().summaryLine(); !strings.Contains(summary, "n'ont pas pu être établis") {
		t.Errorf("résumé inattendu : %s", summary)
	}

	warned := newBench(t)
	warned.machine.space.FreeBytes = 32 << 20
	if summary := warned.run().summaryLine(); !strings.Contains(summary, "avertissement") {
		t.Errorf("résumé d'un poste en avertissement : %s", summary)
	}

	failed := newBench(t)
	failed.machine.autoLogon.Enabled = false
	summary := failed.run().summaryLine()
	if !strings.Contains(summary, "en échec") || !strings.Contains(summary, "de haut en bas") {
		t.Errorf("le résumé d'un poste en échec doit dire quoi faire des consignes : %s", summary)
	}
}

func TestValidateRefusesAVerdictThatSaysNothingToDo(t *testing.T) {
	broken := Report{Controls: []Control{
		{ID: "essai", Checked: "Quelque chose", Status: StatusFail, Observed: "c'est cassé"},
	}}
	err := broken.Validate()
	if err == nil {
		t.Fatal("un ÉCHEC sans consigne doit être refusé par le rapport lui-même")
	}
	if !strings.Contains(err.Error(), "n'a rien diagnostiqué") {
		t.Errorf("le message doit dire pourquoi c'est une faute : %v", err)
	}

	silent := Report{Controls: []Control{{ID: "essai", Checked: "Quelque chose", Status: StatusPass}}}
	if silent.Validate() == nil {
		t.Error("un contrôle sans constat ne nomme aucun fait")
	}

	fine := Report{Controls: []Control{
		{ID: "essai", Checked: "Quelque chose", Status: StatusNotApplicable, Observed: "sans objet ici"},
	}}
	if err := fine.Validate(); err != nil {
		t.Errorf("un contrôle sans objet n'a rien à prescrire : %v", err)
	}
}

func TestTheWorstVerdictIsNotTheHighestConstant(t *testing.T) {
	// StatusUnknown is declared AFTER StatusFail and is far less serious. A summary that
	// ranked on the numeric order would announce « inconnu » on a station whose service is
	// dead.
	report := Report{Controls: []Control{
		{Status: StatusUnknown}, {Status: StatusFail}, {Status: StatusPass},
	}}
	if report.Worst() != StatusFail {
		t.Errorf("pire verdict %s, attendu ÉCHEC", report.Worst())
	}

	report = Report{Controls: []Control{{Status: StatusUnknown}, {Status: StatusWarn}}}
	if report.Worst() != StatusWarn {
		t.Errorf("pire verdict %s, attendu ATTENTION", report.Worst())
	}

	report = Report{Controls: []Control{{Status: StatusNotApplicable}, {Status: StatusPass}}}
	if report.Worst() != StatusPass {
		t.Errorf("« sans objet » ne dégrade rien : %s", report.Worst())
	}
}

func TestAVerdictTravelsAsAStableEnglishKeyAndIsShownInFrench(t *testing.T) {
	for status, key := range map[Status]string{
		StatusPass: "pass", StatusWarn: "warn", StatusFail: "fail",
		StatusUnknown: "unknown", StatusNotApplicable: "not_applicable",
	} {
		if status.String() != key {
			t.Errorf("%d → %q, attendu %q dans l'archive", status, status.String(), key)
		}
		var back Status
		if err := back.UnmarshalJSON([]byte(`"` + key + `"`)); err != nil || back != status {
			t.Errorf("%q ne se relit pas : %v / %d", key, err, back)
		}
	}
	if StatusFail.Label() != "ÉCHEC" {
		t.Errorf("le mot affiché est français : %q", StatusFail.Label())
	}
	var unknown Status
	if err := unknown.UnmarshalJSON([]byte(`"quelque-chose"`)); err == nil {
		t.Error("un verdict inconnu doit être refusé plutôt que lu comme un succès")
	}
}

func TestAReportWithoutAConfigurationDoesNotPresentItselfAsStationZero(t *testing.T) {
	empty := Report{}
	if line := empty.stationLine(); !strings.Contains(line, "non identifié") {
		t.Errorf("un rapport sans configuration : %q", line)
	}
	if line := empty.versionLine(); !strings.Contains(line, "inconnue") {
		t.Errorf("un binaire sans version : %q", line)
	}
	if line := (SystemInfo{}).Line(); line != "/" {
		// A system nobody could describe renders as the bare separator rather than as
		// invented words. It is ugly on purpose: it must not be mistaken for a reading.
		t.Errorf("un système non décrit : %q", line)
	}
}
