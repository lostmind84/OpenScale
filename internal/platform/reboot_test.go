package platform

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

// ⚠ AUCUN TEST DE CE FICHIER N'APPELLE Reboot() SOUS WINDOWS NI SOUS LINUX, et ce n'est
// pas une précaution de style : l'appel réussirait, et la machine du développeur — ou le
// coureur de la CI — redémarrerait au milieu de la suite. Ce qui se prouve ici est ce
// qu'un appel REFUSÉ raconte ; ce que fait un appel accepté se prouve sur le poste, par
// la recette de §21.

// TestRebootIsHonestAboutPlatformsThatCannot.
//
// A sentinel and not a formatted string, for the reason ErrServiceUnsupported gives: the
// HTTP layer tells this case from a refusal, and answers 501 rather than 500 — « ce
// poste ne sait pas » and « ça n'a pas marché » send a volunteer to two different places.
func TestRebootIsHonestAboutPlatformsThatCannot(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "linux" {
		t.Skip("cette plateforme sait redémarrer : l'appeler ici arrêterait la machine du test")
	}
	if err := Reboot(); !errors.Is(err, ErrRebootUnsupported) {
		t.Fatalf("Reboot() = %v, attendu ErrRebootUnsupported", err)
	}
}

// TestTheUnsupportedSentenceIsFrenchAndNamesThePlatforms: it is displayed as it is, to
// somebody who has to decide whether their station is one of those.
func TestTheUnsupportedSentenceIsFrenchAndNamesThePlatforms(t *testing.T) {
	sentence := ErrRebootUnsupported.Error()
	for _, named := range []string{"Windows", "Linux"} {
		if !strings.Contains(sentence, named) {
			t.Errorf("la phrase ne nomme pas %s : %q", named, sentence)
		}
	}
}
