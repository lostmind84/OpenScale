//go:build !windows

package platform

import (
	"fmt"
	"os/exec"
	"runtime"
)

// reboot asks logind to restart the machine.
//
// It goes through systemctl and not through a D-Bus call written here: the call has to
// pass polkit either way, systemctl reports the refusal in a sentence a volunteer can
// forward, and a D-Bus client would be a dependency taken on for one call.
//
// THE SERVICE RUNS AS `openscale`, NOT AS ROOT. Without the polkit rule that
// deploy/linux/install.sh poses, this is refused — and that is the nominal state of any
// station installed before the rule existed, so the message carries its own remedy
// rather than leaving a volunteer with « Access denied ».
func reboot() error {
	if runtime.GOOS != "linux" {
		return ErrRebootUnsupported
	}
	output, err := exec.Command("systemctl", "reboot").CombinedOutput()
	if err != nil {
		return fmt.Errorf("l'ordinateur n'a pas pu être redémarré : %w (%s). Si le message "+
			"parle d'autorisation, la règle polkit du poste manque : relancez "+
			"« sudo ./install.sh » depuis deploy/linux", err, output)
	}
	return nil
}
