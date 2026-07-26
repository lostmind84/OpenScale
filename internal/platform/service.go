package platform

import (
	"errors"
	"time"
)

// ServiceName is the name the Windows SCM records, and the name `sc query` takes.
//
// The product is `openscale` and no longer `Balance` (docs/03-glossaire.md): the
// service, the database and the journals all carry the product name.
const ServiceName = "OpenScale"

// ServiceSpec is what the service manager has to be told once, at installation.
type ServiceSpec struct {
	// Name is the key the service manager stores it under.
	Name string
	// DisplayName and Description are FRENCH: they are what a volunteer reads in the
	// services console of a station that will not start.
	DisplayName string
	Description string
	// Executable is the absolute path of the binary, and Arguments what it is started
	// with — `serve`, plus the two paths of §11.1 when they are not the defaults.
	Executable string
	Arguments  []string
	// AutoStart false records the service in « demand » mode, which is what the pilot
	// period of L9 asks for: the Access application stays relaunchable in under two
	// minutes, so the new station must not take the port at every boot.
	AutoStart bool
	// StopBudget is how long the service manager must allow for the ordered shutdown of
	// §13.4 before concluding that the process hung. The caller computes it from
	// station.ShutdownBudget: this package deliberately does not know it.
	StopBudget time.Duration
	// RecoveryDelays are the successive waits before each automatic restart, and
	// RecoveryReset how long a service has to stay up for the count to start over. They
	// are the `sc failure` of §15.2, set here so that one guarded call replaces two
	// native ones that can fail silently.
	RecoveryDelays []time.Duration
	RecoveryReset  time.Duration
}

// ServiceState is what the service manager says about the service right now.
type ServiceState struct {
	// Installed is false when nothing is registered under that name, which is a state
	// and not an error: it is the answer `openscale doctor` needs for its first control.
	Installed bool
	// Running reports whether the process is up.
	Running bool
	// StartMode is « automatique », « manuel » or « désactivé », in French, because it
	// is displayed as such.
	StartMode string
	// Detail is FRENCH and says what a volunteer should conclude.
	Detail string
}

// ErrServiceUnsupported is what every entry point of this file returns on a platform
// that has no Windows SCM.
//
// It is a sentinel and not a formatted string because the CLI tells this case apart
// from a refusal by the SCM: on Linux the remedy is a systemd unit, and printing
// « accès refusé » there would send a volunteer looking for an administrator account
// that does not exist.
var ErrServiceUnsupported = errors.New(
	"l'installation en service n'existe que sous Windows. Sous Linux, ce travail est " +
		"celui de l'unité systemd livrée dans deploy/linux : « sudo ./install.sh » puis " +
		"« systemctl status openscale »")
