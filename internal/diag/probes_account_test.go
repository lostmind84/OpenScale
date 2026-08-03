package diag

import (
	"strings"
	"testing"
)

// The tests of probes_account.go: the hive of the account that opens the client screen. Two
// traps are held here permanently — the principal of the task is not its trigger, and a SID
// is not an account name — because each of them has already accused a healthy station.

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
