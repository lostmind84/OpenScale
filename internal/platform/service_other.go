//go:build !windows

package platform

import (
	"context"
	"time"

	"openscale/internal/station/ports"
)

// This is the twin cut 5 of §5.2 requires, and here it really does refuse: there is
// no service manager to register with on this platform, and the systemd unit of §15.3
// does the same job by being a FILE — installed by deploy/linux/install.sh, enabled by
// `systemctl enable`, and readable by whoever has to debug it three years from now.
//
// Refusing rather than shelling out to systemctl is deliberate. A `service install`
// that wrote a unit file behind the operator's back would give Linux a second
// installation path, invisible in the repository, competing with the unit the tests of
// deploy/ check directive by directive.

// InstallService refuses: on this platform the unit file is the installation.
func InstallService(ServiceSpec) error { return ErrServiceUnsupported }

// RemoveService refuses: `systemctl disable --now openscale` removes the service.
func RemoveService(ports.Clock, string, time.Duration) error { return ErrServiceUnsupported }

// StopService refuses: `systemctl stop openscale` stops it.
func StopService(ports.Clock, string, time.Duration) error { return ErrServiceUnsupported }

// StartService refuses: `systemctl start openscale` starts it.
func StartService(string) error { return ErrServiceUnsupported }

// QueryService refuses: `systemctl status openscale` answers it.
func QueryService(string) (ServiceState, error) { return ServiceState{}, ErrServiceUnsupported }

// StartedByServiceManager reports false, and it is not a stub.
//
// systemd starts a service as an ORDINARY process — stdout to the journal, SIGTERM to
// stop — which is exactly what `openscale serve` already is. There is no protocol to
// switch into, and `Type=notify` adds a datagram sent by ServiceNotifier, not a
// different way of running.
func StartedByServiceManager() bool { return false }

// RunAsService refuses: nothing on this platform asks a process to speak a service
// control protocol.
func RunAsService(string, time.Duration, func(context.Context) error) error {
	return ErrServiceUnsupported
}
