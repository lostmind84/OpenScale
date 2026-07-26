package platform

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"openscale/internal/station/ports"
)

// This file is the Windows SCM, and it is the reason §15.2 can promise « le poste
// démarre avant toute ouverture de session ». A service runs in session 0, isolated
// from the interactive desktop since Vista: it cannot start the browser on the
// physical screen — but the converse is false, and nothing forces the SERVER to live
// in a session.
//
// It uses golang.org/x/sys/windows/svc, already a dependency of this module and
// already listed in THIRD-PARTY.md. §17.1 and THIRD-PARTY.md also name
// github.com/kardianos/service for this job; it is NOT used, for the same reason
// internal/printing/transport writes to the spooler with syscall rather than pulling
// github.com/alexbrainman/printer — the wrapper adds a module to maintain for ten
// years around an API we call four times.

// InstallService registers the service, or brings an already registered one back in
// line with this specification.
//
// It is IDEMPOTENT because an installation script is run twice: once on a new
// station, and once six months later after somebody « redid the install » to fix
// something else. A second run must converge, never fail.
func InstallService(spec ServiceSpec) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("le gestionnaire de services est inaccessible (êtes-vous administrateur ?) : %w", err)
	}
	defer func() { _ = m.Disconnect() }()

	config := mgr.Config{
		DisplayName:    spec.DisplayName,
		Description:    spec.Description,
		StartType:      mgr.StartManual,
		ServiceType:    windows.SERVICE_WIN32_OWN_PROCESS,
		ErrorControl:   mgr.ErrorNormal,
		BinaryPathName: quoteCommand(spec.Executable, spec.Arguments),
	}
	if spec.AutoStart {
		config.StartType = mgr.StartAutomatic
		// DelayedAutoStart lets the disks, the network stack and the print spooler come
		// up first. The station is not in a hurry — a customer is not waiting at 6 a.m.
		// — and a service that starts before the spooler answers is a station whose
		// first label of the day fails for a reason nobody can reproduce at 10 a.m.
		config.DelayedAutoStart = true
	}

	existing, err := m.OpenService(spec.Name)
	if err == nil {
		defer func() { _ = existing.Close() }()
		if err := existing.UpdateConfig(config); err != nil {
			return fmt.Errorf("la configuration du service %s n'a pas pu être mise à jour : %w", spec.Name, err)
		}
		return setRecovery(existing, spec)
	}

	created, err := m.CreateService(spec.Name, spec.Executable, config, spec.Arguments...)
	if err != nil {
		return fmt.Errorf("le service %s n'a pas pu être créé : %w", spec.Name, err)
	}
	defer func() { _ = created.Close() }()
	return setRecovery(created, spec)
}

// setRecovery applies the automatic restarts of §15.2 — what `sc failure` writes.
func setRecovery(service *mgr.Service, spec ServiceSpec) error {
	if len(spec.RecoveryDelays) == 0 {
		return nil
	}
	actions := make([]mgr.RecoveryAction, 0, len(spec.RecoveryDelays))
	for _, delay := range spec.RecoveryDelays {
		actions = append(actions, mgr.RecoveryAction{Type: mgr.ServiceRestart, Delay: delay})
	}
	reset := uint32(spec.RecoveryReset / time.Second)
	if err := service.SetRecoveryActions(actions, reset); err != nil {
		return fmt.Errorf("les actions de reprise du service %s n'ont pas pu être posées : %w", spec.Name, err)
	}
	return nil
}

// quoteCommand builds the command line the SCM stores.
//
// The path is quoted even when it holds no space, and that is not belt and braces: an
// unquoted C:\Program Files\… is read by the SCM as the program C:\Program with
// « Files\… » as its first argument, which is the oldest unquoted-service-path bug
// there is — and the default installation directory of §15.2 contains a space.
func quoteCommand(executable string, arguments []string) string {
	command := `"` + executable + `"`
	for _, argument := range arguments {
		command += ` "` + argument + `"`
	}
	return command
}

// RemoveService stops the service if it is running and unregisters it.
//
// It leaves the configuration and the database alone: they live in ProgramData, not
// beside the binary, and important-15 makes « laisser les données intactes » the
// difference between an uninstall and a burnt bridge.
func RemoveService(clk ports.Clock, name string, budget time.Duration) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("le gestionnaire de services est inaccessible (êtes-vous administrateur ?) : %w", err)
	}
	defer func() { _ = m.Disconnect() }()

	service, err := m.OpenService(name)
	if err != nil {
		// Nothing registered under that name is the state the caller asked for.
		return nil
	}
	defer func() { _ = service.Close() }()

	if err := stopAndWait(clk, service, budget); err != nil {
		return err
	}
	if err := service.Delete(); err != nil {
		return fmt.Errorf("le service %s n'a pas pu être supprimé : %w", name, err)
	}
	return nil
}

// StopService stops the service and waits, BOUNDED, for it to have stopped.
func StopService(clk ports.Clock, name string, budget time.Duration) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("le gestionnaire de services est inaccessible (êtes-vous administrateur ?) : %w", err)
	}
	defer func() { _ = m.Disconnect() }()
	service, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("le service %s n'est pas installé sur ce poste", name)
	}
	defer func() { _ = service.Close() }()
	return stopAndWait(clk, service, budget)
}

// stopAndWait asks for the ordered shutdown of §13.4 and waits for it.
//
// The wait is what `update.ps1` used to get wrong: a stop that is not waited for is a
// binary copied over a process that still holds it, and the copy fails with « file in
// use » on a station where nothing is wrong.
func stopAndWait(clk ports.Clock, service *mgr.Service, budget time.Duration) error {
	status, err := service.Query()
	if err != nil {
		return fmt.Errorf("l'état du service n'a pas pu être lu : %w", err)
	}
	if status.State == svc.Stopped {
		return nil
	}
	if _, err := service.Control(svc.Stop); err != nil {
		return fmt.Errorf("l'arrêt du service a été refusé : %w", err)
	}

	deadline := clk.After(budget)
	// The poll interval is the injected clock's, so a test drives it; a real stop
	// answers in under three seconds (§13.4) and this loop then runs a handful of times.
	for {
		select {
		case <-deadline:
			return fmt.Errorf("le service ne s'est pas arrêté en %s : ne copiez pas le binaire "+
				"par-dessus un processus encore en vie, appelez openscale doctor", budget)
		case <-clk.After(250 * time.Millisecond):
		}
		status, err := service.Query()
		if err != nil {
			return fmt.Errorf("l'état du service n'a pas pu être lu : %w", err)
		}
		if status.State == svc.Stopped {
			return nil
		}
	}
}

// StartService starts an installed service.
func StartService(name string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("le gestionnaire de services est inaccessible (êtes-vous administrateur ?) : %w", err)
	}
	defer func() { _ = m.Disconnect() }()
	service, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("le service %s n'est pas installé sur ce poste", name)
	}
	defer func() { _ = service.Close() }()
	if err := service.Start(); err != nil {
		return fmt.Errorf("le service %s n'a pas démarré : %w", name, err)
	}
	return nil
}

// QueryService reports what the SCM knows about the service.
//
// It opens the service manager READ-ONLY, and that is not a detail of style: mgr.Connect
// asks for full control, which needs an elevated session, so a `service status` built on
// it would answer « accès refusé » to the volunteer who is following TROUBLESHOOTING.md
// on a station that shows nothing. Reading a state is not administering.
func QueryService(name string) (ServiceState, error) {
	manager, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return ServiceState{}, fmt.Errorf("le gestionnaire de services est inaccessible : %w", err)
	}
	m := &mgr.Mgr{Handle: manager}
	defer func() { _ = m.Disconnect() }()

	unicodeName, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return ServiceState{}, fmt.Errorf("nom de service illisible %q : %w", name, err)
	}
	handle, err := windows.OpenService(manager, unicodeName,
		windows.SERVICE_QUERY_STATUS|windows.SERVICE_QUERY_CONFIG)
	if err != nil {
		return ServiceState{Detail: fmt.Sprintf(
			"le service %s n'est pas installé sur ce poste : lancez install.ps1 en administrateur", name)}, nil
	}
	service := &mgr.Service{Name: name, Handle: handle}
	defer func() { _ = service.Close() }()

	status, err := service.Query()
	if err != nil {
		return ServiceState{Installed: true}, fmt.Errorf("l'état du service n'a pas pu être lu : %w", err)
	}
	config, err := service.Config()
	if err != nil {
		return ServiceState{Installed: true}, fmt.Errorf("la configuration du service n'a pas pu être lue : %w", err)
	}

	state := ServiceState{
		Installed: true,
		Running:   status.State == svc.Running,
		StartMode: startModeName(config.StartType),
	}
	state.Detail = fmt.Sprintf("service installé, démarrage %s, état %s",
		state.StartMode, serviceStateName(status.State))
	return state, nil
}

// startModeName is the start mode in French, as the services console shows it.
func startModeName(startType uint32) string {
	switch startType {
	case mgr.StartAutomatic:
		return "automatique"
	case mgr.StartManual:
		return "manuel"
	case mgr.StartDisabled:
		return "désactivé"
	}
	return "inconnu"
}

// serviceStateName is the run state in French.
func serviceStateName(state svc.State) string {
	switch state {
	case svc.Stopped:
		return "arrêté"
	case svc.StartPending:
		return "en cours de démarrage"
	case svc.StopPending:
		return "en cours d'arrêt"
	case svc.Running:
		return "démarré"
	case svc.Paused:
		return "suspendu"
	}
	return "inconnu"
}

// StartedByServiceManager reports whether this process was launched by the SCM
// rather than typed into a terminal.
//
// It is what lets ONE subcommand serve both: `openscale serve` typed by hand must
// keep writing to stderr and stop on Ctrl+C, and the same `openscale serve` started
// by the SCM must speak the service control protocol — a binary that answered the SCM
// only when an extra flag was passed would fail to start for whoever forgets it,
// which is the failure mode the installation sheet cannot fix.
func StartedByServiceManager() bool {
	inService, err := svc.IsWindowsService()
	if err != nil {
		return false
	}
	return inService
}

// RunAsService runs work under the Windows service control protocol.
//
// The context it hands over is cancelled when the SCM asks to stop or when Windows
// shuts down. Both lead to the SAME ordered shutdown as a SIGTERM or a Ctrl+C: a
// station that stopped differently depending on who asked would have two shutdowns,
// and one of them would be untested.
func RunAsService(name string, stopBudget time.Duration, work func(context.Context) error) error {
	return svc.Run(name, &serviceHandler{work: work, stopBudget: stopBudget})
}

// serviceHandler is the SCM side of `openscale serve`.
type serviceHandler struct {
	work       func(context.Context) error
	stopBudget time.Duration
	// failure is the error the work returned, kept so that the caller reports it on
	// stderr and in the event log rather than the SCM's bare exit code.
	failure error
}

// Execute is the service control loop.
func (h *serviceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	status <- svc.Status{State: svc.StartPending, WaitHint: h.waitHint()}
	done := make(chan error, 1)
	go func() { done <- h.work(ctx) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for {
		select {
		case err := <-done:
			// The station stopped on its own: ERR-SYS-03, or a configuration it cannot
			// read. Reporting a non-zero exit code is what makes the SCM apply the
			// recovery actions of §15.2 instead of recording a clean stop.
			h.failure = err
			if err != nil {
				return false, 1
			}
			return false, 0
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				status <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				// THE WAIT HINT IS THE POINT. The SCM decides on its own that a service
				// hung once StopPending outlives it, and §13.4 is the story of what that
				// costs: the budget was written 20 s against a real budget of 20 s, and
				// update.ps1 failed intermittently on a healthy station.
				status <- svc.Status{State: svc.StopPending, WaitHint: h.waitHint()}
				cancel()
				h.failure = <-done
				if h.failure != nil {
					return false, 1
				}
				return false, 0
			}
		}
	}
}

// waitHint is the stop budget in the milliseconds the SCM expects, with a floor: a
// caller that passed nothing must not tell Windows « I stop instantly ».
func (h *serviceHandler) waitHint() uint32 {
	budget := h.stopBudget
	if budget <= 0 {
		budget = 30 * time.Second
	}
	return uint32(budget / time.Millisecond)
}
