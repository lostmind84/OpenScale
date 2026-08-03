package diag

import (
	"context"
	"errors"
	"runtime"
	"testing"
)

// The tests of probes_power.go: powercfg localises every one of its labels, so the index is
// read off the SHAPE — hexadecimal values, the last two of the block, mains first. Reading
// the first one would report every station as « veille désactivée », in the dangerous
// direction.

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

// refusingRunner is a runner on which every command fails, which is what a machine without
// sc.exe, systemctl or powercfg looks like.
type refusingRunner struct{}

func (refusingRunner) Run(context.Context, string, ...string) (string, error) {
	return "", errors.New("commande introuvable")
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
