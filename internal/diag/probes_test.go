package diag

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The platform layer asks the operating system with the very tools §15.2 and §15.3 use to
// WRITE the answer, and hands the output to a pure function. These tests drive those pure
// functions with real output — including a FRENCH Windows, which is what the shop has and
// what every locale-dependent parser fails on.

// --- The service manager ----------------------------------------------------

func TestAWindowsServiceIsReadFromTheEnglishTokensOnly(t *testing.T) {
	// Real `sc query` output on a French Windows: the labels are English, the surrounding
	// prose is not, and the numeric codes are the one thing that must NOT be trusted.
	running := `
SERVICE_NAME: OpenScale
        TYPE               : 10  WIN32_OWN_PROCESS
        STATE              : 4  RUNNING
                                (STOPPABLE, PAUSABLE, ACCEPTS_SHUTDOWN)
        WIN32_EXIT_CODE    : 0  (0x0)
        CHECKPOINT         : 0x0
`
	auto := `
[SC] QueryServiceConfig réussi(e)

SERVICE_NAME: OpenScale
        TYPE               : 10  WIN32_OWN_PROCESS
        START_TYPE         : 2   AUTO_START
        BINARY_PATH_NAME   : "C:\Program Files\OpenScale\openscale.exe" serve
`
	state := parseWindowsService("OpenScale", running, auto)
	if !state.Determined || !state.Known || !state.Running || !state.Automatic {
		t.Fatalf("service démarré et automatique mal lu : %+v", state)
	}
	if !strings.Contains(state.Detail, "RUNNING") || !strings.Contains(state.Detail, "AUTO_START") {
		t.Errorf("le détail doit reprendre les deux mots verbatim : %q", state.Detail)
	}
}

func TestAStoppedServiceIsKnownAndNotRunning(t *testing.T) {
	stopped := "        STATE              : 1  STOPPED\n"
	demand := "        START_TYPE         : 3   DEMAND_START\n"

	state := parseWindowsService("OpenScale", stopped, demand)
	switch {
	case !state.Known:
		t.Error("un service arrêté est installé : le remède n'est pas de l'installer")
	case state.Running:
		t.Error("STOPPED lu comme démarré")
	case state.Automatic:
		t.Error("DEMAND_START lu comme automatique")
	}
}

func TestErrorTenSixtyIsAServiceThatWasNeverInstalled(t *testing.T) {
	// The message is localised; the number is not, and it is the one thing that separates
	// « installed but stopped » from « never installed » — two different remedies.
	absent := `[SC] OpenSevice ÉCHEC 1060 :

Le service spécifié n'existe pas en tant que service installé.
`
	state := parseWindowsService("OpenScale", absent, "")
	if !state.Determined {
		t.Fatal("un service absent est une réponse, pas une absence de réponse")
	}
	if state.Known {
		t.Error("l'erreur 1060 signifie que le service n'existe pas")
	}
}

// TestADelayedAutomaticStartIsStillAutomatic covers the shape the product's OWN installer
// produces, and the one this corpus was missing.
//
// internal/platform/service_windows.go sets DelayedAutoStart on purpose, so that the disks,
// the network stack and the print spooler come up first. `sc qc` then prints
// « START_TYPE : 2   AUTO_START  (DELAYED) », whose LAST word is « (DELAYED) » — and a
// parser reading the last word declared the service not automatic. Every station installed
// with « --start auto » was warned to set the very setting it already had. The two lines
// below are copied from a station installed by install.ps1.
func TestADelayedAutomaticStartIsStillAutomatic(t *testing.T) {
	running := "        STATE              : 4  RUNNING \n"
	delayed := "        START_TYPE         : 2   AUTO_START  (DELAYED)\n"

	state := parseWindowsService("OpenScale", running, delayed)
	if !state.Automatic {
		t.Error("AUTO_START (DELAYED) lu comme non automatique : ce poste redémarre pourtant seul")
	}
	if state.Detail != "RUNNING, AUTO_START (DELAYED)" {
		t.Errorf("le détail doit porter les deux mots, pas la parenthèse seule : %q", state.Detail)
	}
}

// TestAnUnreadableTaskFolderIsNotAnAbsentTask is what v0.1 got wrong on a real station.
//
// schtasks exits 1 for « Erreur : Accès refusé. » as well as for « Erreur : Le fichier
// spécifié est introuvable. », and both messages are localised. Unelevated — which is how a
// volunteer runs `openscale doctor` — the two cannot be told apart, and answering
// « absente » sends somebody reinstalling a station whose task is right there.
func TestAnUnreadableTaskFolderIsNotAnAbsentTask(t *testing.T) {
	state, err := kioskTaskState("OpenScale-Kiosk", "Erreur : Accès refusé.\n", true, false)
	if err == nil {
		t.Fatal("un refus d'accès rendu comme une réponse : doctor conclurait « tâche absente » " +
			"et enverrait relancer install.ps1")
	}
	if state.Determined {
		t.Error("l'état se dit déterminé alors que rien n'a pu être lu")
	}

	// Elevated, the SAME failure IS an answer: we could look, and it is not there.
	state, err = kioskTaskState("OpenScale-Kiosk",
		"Erreur : Le fichier spécifié est introuvable.\n", true, true)
	if err != nil {
		t.Fatalf("en session élevée, un échec de schtasks est une réponse : %v", err)
	}
	if !state.Determined || state.Known {
		t.Errorf("tâche réellement absente mal rendue : %+v", state)
	}

	// And a task that answers is simply there, elevation or not.
	state, err = kioskTaskState("OpenScale-Kiosk", "Dossier: \\\nOpenScale-Kiosk  N/A  Prêt\n", false, false)
	if err != nil || !state.Determined || !state.Known {
		t.Errorf("tâche présente mal rendue : %+v (%v)", state, err)
	}
}

func TestOutputThisParserDoesNotRecogniseIsNotTurnedIntoAVerdict(t *testing.T) {
	state := parseWindowsService("OpenScale", "sc.exe n'est pas reconnu comme commande", "")
	if state.Determined {
		t.Fatal("une sortie incompréhensible doit rendre « je ne sais pas » : annoncer un service " +
			"absent enverrait quelqu'un le réinstaller, annoncer qu'il tourne serait pire")
	}
}

func TestASystemdUnitIsReadFromTheWordAndNotFromTheExitCode(t *testing.T) {
	// systemctl exits non-zero for « inactive » and for « disabled », which are ordinary
	// answers. The word carries everything.
	state := parseSystemdUnit(linuxServiceUnit, "active\n", "enabled\n")
	if !state.Determined || !state.Known || !state.Running || !state.Automatic {
		t.Fatalf("unité active et activée mal lue : %+v", state)
	}

	state = parseSystemdUnit(linuxServiceUnit, "inactive\n", "disabled\n")
	if !state.Known || state.Running || state.Automatic {
		t.Fatalf("unité installée mais arrêtée mal lue : %+v", state)
	}

	state = parseSystemdUnit(linuxServiceUnit, "inactive\n", "not-found\n")
	if state.Known {
		t.Error("« not-found » est la seule réponse qui signifie que l'unité n'a jamais été installée")
	}

	if state := parseSystemdUnit(linuxServiceUnit, "", ""); state.Determined {
		t.Error("systemctl muet n'est pas une unité absente : c'est une question qui n'a pas été posée")
	}
}

// --- The unattended restart -------------------------------------------------

func TestAutoLogonIsReadFromTheValueTypeAndNotFromTheLabel(t *testing.T) {
	enabled := `
HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Winlogon
    AutoAdminLogon    REG_SZ    1
`
	account := `
HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Winlogon
    DefaultUserName    REG_SZ    openscale
`
	state := parseAutoLogon(enabled, account)
	if !state.Determined || !state.Enabled {
		t.Fatalf("AutoAdminLogon = 1 mal lu : %+v", state)
	}
	if state.Account != "openscale" {
		t.Errorf("compte lu %q, attendu openscale", state.Account)
	}
}

func TestAutoLogonSetToZeroIsNotConfigured(t *testing.T) {
	state := parseAutoLogon("    AutoAdminLogon    REG_SZ    0\n", "")
	if !state.Determined {
		t.Fatal("la valeur a été lue : la question a bien été posée")
	}
	if state.Enabled {
		t.Error("AutoAdminLogon = 0 lu comme configuré")
	}
	if !strings.Contains(state.Detail, "0") {
		t.Errorf("le détail doit citer la valeur lue : %q", state.Detail)
	}
}

func TestAnEmptyAutoLogonValueIsReadAsEmptyAndNotAsAbsent(t *testing.T) {
	// reg.exe prints nothing after the type when the data is empty. « AutoAdminLogon vide »
	// is not « AutoAdminLogon introuvable », and the two have different remedies.
	value, found := registryValue("    AutoAdminLogon    REG_SZ\n", "AutoAdminLogon")
	if !found {
		t.Fatal("une valeur vide a bien été trouvée")
	}
	if value != "" {
		t.Errorf("valeur %q, attendue vide", value)
	}
}

func TestAQueryThatReturnedNothingIsNotAnAnswer(t *testing.T) {
	state := parseAutoLogon("", "")
	if state.Determined {
		t.Fatal("la clé Winlogon existe sur tout Windows : une réponse vide signifie que la " +
			"requête n'a pas tourné, pas que l'ouverture de session n'est pas configurée")
	}
}

func TestTheKioskAccountIsReadFromTheTaskXMLBecauseItIsTheOnlyPartNotLocalised(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <Principals>
    <Principal id="Author">
      <UserId>PESEE-2\openscale</UserId>
      <LogonType>InteractiveToken</LogonType>
    </Principal>
  </Principals>
</Task>`
	// The domain prefix is dropped: the registry spells DefaultUserName without it, and
	// comparing the two forms would report a mismatch that does not exist.
	if account := parseTaskUserID(xml); account != "openscale" {
		t.Errorf("compte du kiosque lu %q, attendu openscale", account)
	}
	if account := parseTaskUserID("la tâche n'existe pas"); account != "" {
		t.Errorf("aucune tâche : compte %q, attendu vide", account)
	}
}

func TestTheKioskAccountIsReadFromThePrincipalAndNotFromTheTrigger(t *testing.T) {
	// The XML Windows hands back is NOT the one install.ps1 wrote: the scheduler
	// normalises the trigger's UserId to a SID. Reading the FIRST <UserId> of the document
	// therefore read the trigger — a SID that can never equal « openscale » — and doctor
	// accused a healthy station of opening its session onto the wrong account. Observed on
	// the station, 31/07/2026.
	//
	// The <Principal> is the one that answers the question the control asks: it says under
	// which account the task RUNS. The trigger only says which logon wakes it.
	xml := `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>S-1-5-21-1004336348-1177238915-682003330-1001</UserId>
      <Delay>PT5S</Delay>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>PESEE-2\openscale</UserId>
      <LogonType>InteractiveToken</LogonType>
    </Principal>
  </Principals>
</Task>`
	if account := parseTaskUserID(xml); account != "openscale" {
		t.Errorf("compte du kiosque lu %q, attendu openscale", account)
	}
}

func TestASIDIsNotAnAccountNameAndIsNotComparedToOne(t *testing.T) {
	// The scheduler may normalise the PRINCIPAL to a SID too. There is nothing to compare
	// then: DefaultUserName is spelled « openscale », and a SID is never equal to it. The
	// honest answer is « je ne sais pas », which doctor already handles — its mismatch
	// branch is guarded by Expected != "". Answering the SID instead turns an unknown into
	// an accusation, which is the defect this whole change removes.
	xml := `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <Principals>
    <Principal id="Author">
      <UserId>S-1-5-21-1004336348-1177238915-682003330-1001</UserId>
      <LogonType>InteractiveToken</LogonType>
    </Principal>
  </Principals>
</Task>`
	if account := parseTaskUserID(xml); account != "" {
		t.Errorf("compte du kiosque lu %q, attendu vide", account)
	}
}

func TestLinuxUnattendedRestartDemandsBothUnits(t *testing.T) {
	state := parseLinuxUnattendedRestart("enabled\n", "enabled\n")
	if !state.Determined || !state.Enabled {
		t.Fatalf("les deux unités activées : %+v", state)
	}
	// The service alone is not enough: it weighs, and nothing opens the client screen.
	state = parseLinuxUnattendedRestart("enabled\n", "disabled\n")
	if state.Enabled {
		t.Error("le service seul ne ramène pas le poste sur l'écran client")
	}
	if !strings.Contains(state.Detail, linuxKioskUnit) {
		t.Errorf("le détail doit nommer l'unité fautive : %q", state.Detail)
	}
}

// --- The power plan ---------------------------------------------------------

func TestThePowerIndexIsReadFromARangeSettingWithoutBeingFooledByItsBounds(t *testing.T) {
	// Real `powercfg /query SCHEME_CURRENT SUB_SLEEP STANDBYIDLE` output, French Windows.
	// The bounds are printed FIRST: a parser that took the first 0x value would read the
	// minimum and announce « veille désactivée » on a station that falls asleep.
	output := `
GUID du mode de gestion de l'alimentation : 381b4222-f694-41f0-9685-ff5bb260df2e  (Équilibré)
  GUID de sous-groupe d'alimentation : 238c9fa8-0aad-41ed-83f4-97be242c8f20  (Mise en veille)
    GUID de paramètre d'alimentation : 29f6c1db-86da-48c5-9fdb-f2b67b1f44da  (Mettre en veille après)
      Valeur minimale possible : 0x00000000
      Valeur maximale possible : 0xffffffff
      Incrément possible : 0x00000001
      Unités possibles : Secondes
    Index du paramètre d'alimentation sur secteur actuel : 0x00000384
    Index du paramètre d'alimentation sur batterie actuel : 0x000000f0
`
	value, ok := parsePowerIndex(output)
	if !ok {
		t.Fatal("la sortie porte bien un index sur secteur")
	}
	if value != 0x384 {
		t.Errorf("index sur secteur lu %#x, attendu 0x384 — les bornes de la plage ont été prises "+
			"pour la valeur courante", value)
	}
}

func TestThePowerIndexIsReadFromAnEnumeratedSetting(t *testing.T) {
	// The USB selective suspend, whose possible values are printed with UNPREFIXED indices
	// and are therefore not picked up at all.
	output := `
  GUID de sous-groupe d'alimentation : ` + usbSubgroupGUID + `  (Paramètres USB)
    GUID de paramètre d'alimentation : ` + usbSuspendGUID + `  (Paramètre de la suspension sélective USB)
      Index du paramètre possible : 000
      Nom convivial du paramètre possible : Désactivé
      Index du paramètre possible : 001
      Nom convivial du paramètre possible : Activé
    Index du paramètre d'alimentation sur secteur actuel : 0x00000001
    Index du paramètre d'alimentation sur batterie actuel : 0x00000001
`
	value, ok := parsePowerIndex(output)
	if !ok || value != 1 {
		t.Fatalf("suspension USB active lue %#x / %v", value, ok)
	}
}

func TestASettingWithNoHexadecimalValueIsNotRead(t *testing.T) {
	if _, ok := parsePowerIndex("Le nom de paramètre spécifié est introuvable."); ok {
		t.Fatal("une sortie sans index ne doit pas rendre une valeur : ce serait un chiffre que " +
			"personne n'a mesuré")
	}
}

func TestThePowerControlIsSkippedWhereTheInstallerWritesNoPowerSetting(t *testing.T) {
	state, err := newMachineWith(refusingRunner{}).Power(context.Background())
	if err != nil {
		t.Fatalf("lecture des réglages d'énergie : %v", err)
	}
	if runtime.GOOS != "windows" {
		// §15.3 installs cage, seatd and udev rules, and writes no power setting at all.
		if state.Applicable {
			t.Error("§15.3 n'écrit aucun réglage d'énergie : inventer une exigence serait pire " +
				"que ne rien dire")
		}
		return
	}
	// On Windows the question APPLIES and the command failed, so the honest answer is
	// « applicable, et non établi » — never « tout est désactivé ».
	if !state.Applicable {
		t.Error("§15.2 écrit ces réglages : la question s'applique sous Windows")
	}
	if state.Determined {
		t.Error("powercfg a échoué : le verdict ne peut pas être établi")
	}
}

// --- Small readers ----------------------------------------------------------

func TestTheTokenOfALineIsTheLastWordAndNeverTheCode(t *testing.T) {
	if got := tokenAfter("        STATE              : 4  RUNNING", "STATE"); got != "RUNNING" {
		t.Errorf("token %q, attendu RUNNING", got)
	}
	if got := tokenAfter("        STATE              :", "STATE"); got != "" {
		t.Errorf("une ligne sans valeur doit rendre vide, obtenu %q", got)
	}
	if got := tokenAfter("rien à voir", "STATE"); got != "" {
		t.Errorf("une sortie sans la clé doit rendre vide, obtenu %q", got)
	}
}

func TestTheFirstLineIsTrimmedBecauseWindowsEndsItsLinesWithTwoCharacters(t *testing.T) {
	if got := firstLine("\r\n  active\r\nenabled\r\n"); got != "active" {
		t.Errorf("première ligne %q : un mot comparé contre « active\\r » ne correspond à rien", got)
	}
	if got := firstLine("   \n\n"); got != "" {
		t.Errorf("sortie vide : %q", got)
	}
}

func TestAListeningAddressIsCompletedTheWayABrowserOnTheStationWould(t *testing.T) {
	for address, want := range map[string]string{
		"":               "127.0.0.1",
		":8080":          "127.0.0.1:8080",
		"0.0.0.0:8085":   "127.0.0.1:8085",
		"[::]:8085":      "127.0.0.1:8085",
		"127.0.0.1:80":   "127.0.0.1:80",
		"192.168.1.5:80": "192.168.1.5:80",
	} {
		if got := loopbackHost(address); got != want {
			t.Errorf("%q → %q, attendu %q", address, got, want)
		}
	}
}

func TestADurationIsRenderedToTheUnitThatCarriesTheInformation(t *testing.T) {
	for duration, want := range map[time.Duration]string{
		11 * 24 * time.Hour: "11 jours",
		30 * time.Hour:      "30 h",
		4 * time.Minute:     "4 min",
		45 * time.Second:    "45 s",
	} {
		if got := humanDuration(duration); got != want {
			t.Errorf("%s → %q, attendu %q", duration, got, want)
		}
	}
}

func TestASilentMachineAnswersNothingAndClaimsNothing(t *testing.T) {
	machine := silentMachine{}
	ctx := context.Background()

	if state, _ := machine.Service(ctx); state.Determined {
		t.Error("une machine muette ne peut rien affirmer d'un service")
	}
	if space, _ := machine.FreeSpace("/quelque/part"); space.Determined {
		t.Error("une machine muette ne peut pas mesurer un volume")
	}
	if err := machine.OpenSerialPort(ctx, "COM8"); err == nil {
		t.Error("ouvrir un port sans couche système doit échouer explicitement")
	}
}

// refusingRunner is a runner on which every command fails, which is what a machine without
// sc.exe, systemctl or powercfg looks like.
type refusingRunner struct{}

func (refusingRunner) Run(context.Context, string, ...string) (string, error) {
	return "", errors.New("commande introuvable")
}

// TestTheProfileOfTheStationAccountIsFoundByItsDirectory : c'est le seul chemin de
// « openscale » vers « S-1-5-21-…-1001 » qui ne demande pas un appel Windows que ce paquet
// n'a aucune autre raison de faire — et c'est ce SID qui dit sous quelle ruche relire les
// stratégies du kiosque.
func TestTheProfileOfTheStationAccountIsFoundByItsDirectory(t *testing.T) {
	const listing = `
HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList\S-1-5-18
    ProfileImagePath    REG_EXPAND_SZ    %systemroot%\system32\config\systemprofile

HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList\S-1-5-21-11-22-33-1001
    ProfileImagePath    REG_EXPAND_SZ    C:\Users\Fab

HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList\S-1-5-21-11-22-33-1004
    ProfileImagePath    REG_EXPAND_SZ    C:\Users\openscale
`
	if sid := profileSID(listing, "openscale"); sid != "S-1-5-21-11-22-33-1004" {
		t.Fatalf("SID du compte du poste = %q", sid)
	}
	// Lire le profil d'un autre compte, c'est rendre vert un poste grand ouvert : la
	// stratégie du technicien n'est pas celle du poste.
	if sid := profileSID(listing, "Fab"); sid != "S-1-5-21-11-22-33-1001" {
		t.Fatalf("SID d'un autre compte = %q", sid)
	}
}

// TestAnAccountWithNoProfileYieldsNoSID : un compte créé et jamais ouvert n'a pas de
// profil. Deviner un SID à ce moment-là ferait relire la ruche de quelqu'un d'autre.
func TestAnAccountWithNoProfileYieldsNoSID(t *testing.T) {
	const listing = `
HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList\S-1-5-18
    ProfileImagePath    REG_EXPAND_SZ    %systemroot%\system32\config\systemprofile
`
	if sid := profileSID(listing, "openscale"); sid != "" {
		t.Fatalf("SID %q inventé pour un compte sans profil", sid)
	}
}
