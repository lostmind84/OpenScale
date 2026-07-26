package kiosk

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
)

// ExecLauncher starts a real browser. It is the only impure line of the supervision
// loop, and it is three statements long on purpose.
func ExecLauncher(ctx context.Context, browser Browser, arguments []string) (Process, error) {
	// CommandContext and not Command: when the supervisor is stopped — a logout, a
	// SIGTERM at shutdown — the browser must go with it. A browser left behind holds the
	// profile directory, and the next start finds it locked.
	command := exec.CommandContext(ctx, browser.Path, arguments...)
	// The browser's own noise goes where the supervisor's lines go: one stream to read
	// when a station displays nothing.
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Start(); err != nil {
		return nil, err
	}
	return &execProcess{command: command}, nil
}

// execProcess is one running browser.
type execProcess struct {
	command *exec.Cmd
}

// Wait blocks until the browser has exited.
func (p *execProcess) Wait() error { return p.command.Wait() }

// Kill ends the browser.
//
// It kills the process WE started, which for a Chromium family browser is the parent
// of a handful of renderers. Those exit with their parent in the ordinary case; the one
// where they do not is a browser that was already wedged, and the profile is wiped at
// the next start of the supervisor, which is what makes an orphan harmless here.
func (p *execProcess) Kill() error {
	if p.command.Process == nil {
		return nil
	}
	return p.command.Process.Kill()
}

// LookBrowser is the production lookup of Find: the PATH first, then the two Windows
// program directories where Edge and Chrome hide from a service account's PATH.
func LookBrowser(programDirectories []string) func(string) (string, bool) {
	return func(candidate string) (string, bool) {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, true
		}
		for _, root := range programDirectories {
			for _, relative := range WindowsProgramDirectories {
				if filepath.Base(relative) != candidate {
					continue
				}
				path := filepath.Join(root, relative)
				if info, err := os.Stat(path); err == nil && !info.IsDir() {
					return path, true
				}
			}
		}
		return "", false
	}
}

// AliveProbe is one GET on /healthz, and it is deliberately not /readyz.
//
// §15.3 makes this the most important rule of the section: device failures DEGRADE,
// they never restart anything. A printer with no paper answers /readyz with 503, and a
// kiosk that read it would put « Le poste redémarre… » in front of a customer whose
// weighing is about to print perfectly.
func AliveProbe(address string) func(context.Context) bool {
	client := &http.Client{
		Timeout: ProbeBudget,
		// No proxy, no keep-alive pool to leak: this is a loopback call to a socket on
		// this very machine, and a station is offline by design (contrainte 4).
		Transport: &http.Transport{
			Proxy:                 nil,
			DisableKeepAlives:     true,
			DialContext:           (&net.Dialer{Timeout: ProbeBudget}).DialContext,
			ResponseHeaderTimeout: ProbeBudget,
		},
	}
	url := address + "/healthz"
	return func(ctx context.Context) bool {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return false
		}
		response, err := client.Do(request)
		if err != nil {
			return false
		}
		defer func() { _ = response.Body.Close() }()
		return response.StatusCode == http.StatusOK
	}
}

// DefaultProfileDir is the dedicated browser profile of §15.2, under the user's own
// temporary directory.
//
// Under the USER's, and not under the data directory of the station: the kiosk runs as
// the unprivileged local account, the profile is disposable by definition — it is wiped
// at every start — and putting a browser cache next to the journal of the weighings
// would put the two in the same disk-space alert (§10.4).
func DefaultProfileDir() string {
	return filepath.Join(os.TempDir(), "openscale-kiosk-profile")
}
