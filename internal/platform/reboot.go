package platform

import "errors"

// ErrRebootUnsupported is what Reboot returns on a platform with no reboot of its own.
//
// A sentinel and not a formatted string, for the reason ErrServiceUnsupported gives: the
// caller tells this case apart from a refusal, and answers « ce poste ne sait pas faire »
// rather than « ça n'a pas marché ». The two send a volunteer to two different places.
var ErrRebootUnsupported = errors.New(
	"le redémarrage de l'ordinateur depuis l'écran n'existe que sous Windows et sous Linux")

// Reboot restarts THE MACHINE, and not the station.
//
// The distinction is the whole reason this function exists next to a service that can
// already stop itself: under cage and under Shell Launcher, the client screen has
// nothing to escape to, so a volunteer facing a frozen machine has the power switch and
// nothing else.
//
// It returns as soon as the demand is accepted: what carries it out ends this process.
// THE THIRTY SECONDS A VOLUNTEER HAS TO CHANGE THEIR MIND ARE NOT HERE — they are in
// internal/web, on the injected clock, so that both platforms behave the same way and
// the deadline is provable without restarting a machine.
func Reboot() error { return reboot() }
