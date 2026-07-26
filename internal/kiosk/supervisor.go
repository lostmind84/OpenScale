package kiosk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"openscale/internal/station/ports"
)

// The two periods of §15.2.
const (
	// RelaunchDelay is what the supervisor waits before starting the browser again.
	// §15.2 says « relance en < 2 s »: the number below is what makes that sentence
	// true, and a test holds it to it.
	RelaunchDelay = 1 * time.Second
	// AwakePeriod is how often the sleep inhibition is renewed — belt over the braces
	// of powercfg (§15.2).
	AwakePeriod = 30 * time.Second
	// StationRecheck is how often the rescue page asks whether the station is back.
	// One second: a customer watching « Le poste redémarre… » is watching a station
	// that is about to work, and the wait must not be added to the service's own.
	StationRecheck = 1 * time.Second
	// ProbeBudget bounds one liveness question. It is a network deadline, spent in the
	// kernel's TCP stack, and no business decision rests on it.
	ProbeBudget = 2 * time.Second
)

// Process is the browser, seen from here: something that ends, and that can be ended.
//
// It is an interface because the whole supervision loop — relaunch delay, crash
// counting, rescue page, switch back to the client screen — has to be provable without
// a browser on the machine running the tests, and there is no browser at all on the CI.
type Process interface {
	// Wait blocks until the browser has exited, and reports how.
	Wait() error
	// Kill ends the browser now. It is called when the station comes back while the
	// rescue page is showing, and when the supervisor itself is stopped.
	Kill() error
}

// Options is everything the supervisor needs, and every seam a test drives.
type Options struct {
	// URL is the client screen — http://127.0.0.1:8085/ on a station that kept the
	// delivered configuration.
	URL string
	// Browser is what Find resolved.
	Browser Browser
	// ProfileDir is the dedicated browser profile, wiped at every start of this
	// supervisor: a profile that accumulates state is a station that behaves
	// differently in month six.
	ProfileDir string
	// Launch starts the browser. In production it is ExecLauncher.
	Launch func(ctx context.Context, browser Browser, arguments []string) (Process, error)
	// Alive answers « does the station serve? ». In production it is one GET on
	// /healthz, and it is NEVER /readyz: a printer with no paper must not put the
	// rescue page in front of a customer (§15.3).
	Alive func(ctx context.Context) bool
	// Awake renews the sleep inhibition. Nil is legitimate — it is what a Linux
	// station passes (§15.2 note on platform.KeepAwake).
	Awake func() error
	// Clock is injected like everywhere else: the twenty-first crash inside one hour
	// is a rule this package proves in microseconds.
	Clock ports.Clock
	// Out carries the French log lines. They go to stdout, where the scheduled task
	// and the systemd journal pick them up.
	Out io.Writer
}

// Supervisor keeps the client screen on the display.
type Supervisor struct {
	options Options
	crashes CrashCounter
	// rescue is the file:// URL of the local page, written at start and rewritten when
	// the crash-loop threshold is crossed.
	rescue string
	// rescueMode is true once §15.2's twenty-first quick death has happened. Nothing
	// clears it: the page says « prévenez un responsable », and a supervisor that
	// silently went back to a screen it had just declared broken would make that
	// sentence a lie. Relaunching the kiosk task is the human gesture that resets it.
	rescueMode bool
}

// New checks the options and prepares the local rescue page.
func New(options Options) (*Supervisor, error) {
	switch {
	case options.URL == "":
		return nil, errors.New("le superviseur a besoin de l'adresse de l'écran client")
	case options.Browser.Path == "":
		return nil, errors.New("le superviseur a besoin du chemin du navigateur")
	case options.ProfileDir == "":
		return nil, errors.New("le superviseur a besoin d'un répertoire de profil")
	case options.Launch == nil:
		return nil, errors.New("le superviseur a besoin d'un lanceur")
	case options.Clock == nil:
		return nil, errors.New("le superviseur a besoin d'une horloge")
	}
	if options.Alive == nil {
		// A supervisor with no way to ask launches the client screen and lets the
		// browser say what it finds. It is a degraded mode, never a refusal to start.
		options.Alive = func(context.Context) bool { return true }
	}
	if options.Out == nil {
		options.Out = io.Discard
	}
	return &Supervisor{options: options}, nil
}

// Run keeps the browser up until ctx is done.
//
// It returns nil on a normal stop — a logout, a SIGTERM, the machine shutting down —
// because none of those is a failure of the kiosk. What it does report is the one thing
// that makes the station unusable and that no relaunch fixes: no browser at all, which
// New refuses before we get here.
func (s *Supervisor) Run(ctx context.Context) error {
	if err := s.wipeProfile(); err != nil {
		// A profile that cannot be wiped is not worth stopping for: the browser will
		// reuse the old one, --disable-session-crashed-bubble covers the visible
		// consequence, and the line below is what a volunteer reads afterwards.
		s.logf("le profil du navigateur n'a pas pu être effacé : %v", err)
	}
	rescue, err := WriteRescuePage(s.options.ProfileDir, RescueWaiting, s.options.URL, 0)
	if err != nil {
		return err
	}
	s.rescue = rescue

	go s.keepAwake(ctx)

	s.logf("superviseur démarré : %s sur %s", s.options.Browser.Name, s.options.URL)
	first := true
	for ctx.Err() == nil {
		if !first && !s.pause(ctx, RelaunchDelay) {
			return nil
		}
		first = false
		s.showOnce(ctx)
	}
	return nil
}

// showOnce launches the browser once and returns when it has died — or when the
// station came back while the rescue page was showing.
func (s *Supervisor) showOnce(ctx context.Context) {
	target, onRescue := s.target(ctx)
	process, err := s.options.Launch(ctx, s.options.Browser,
		Arguments(s.options.Browser, target, s.options.ProfileDir))
	if err != nil {
		s.logf("le navigateur n'a pas pu être lancé : %v", err)
		return
	}

	started := s.options.Clock.Now()
	died := make(chan struct{})
	go func() {
		defer close(died)
		_ = process.Wait()
	}()

	switch s.watch(ctx, process, died, onRescue) {
	case stopped, switched:
		// Neither is a crash: one is the supervisor being stopped, the other is a
		// browser WE killed because the station came back. Counting either would walk a
		// station that recovered normally into the rescue page.
		return
	}

	lifetime := s.options.Clock.Now().Sub(started)
	if s.crashes.Record(s.options.Clock.Now(), lifetime) {
		s.enterRescue()
		return
	}
	s.logf("navigateur arrêté après %s, relance dans %s", lifetime, RelaunchDelay)
}

// outcome is how one showing of the browser ended.
type outcome int

const (
	// died is the browser exiting on its own — a crash, an Alt+F4, a customer's child.
	died outcome = iota
	// stopped is the supervisor itself being asked to stop.
	stopped
	// switched is us killing the browser because the station started answering while
	// the rescue page was showing.
	switched
)

// watch waits for the browser to die, for the station to come back, or for the
// supervisor to be stopped.
func (s *Supervisor) watch(ctx context.Context, process Process, exited <-chan struct{}, onRescue bool) outcome {
	// The ticker only exists while the rescue page is showing. On the client screen
	// there is nothing to recheck, and a ticker nobody reads is a timer that leaks.
	var recheck <-chan time.Time
	if onRescue {
		ticks, stop := s.options.Clock.Ticker(StationRecheck)
		defer stop()
		recheck = ticks
	}

	for {
		select {
		case <-exited:
			return died
		case <-ctx.Done():
			_ = process.Kill()
			<-exited
			s.logf("superviseur arrêté")
			return stopped
		case <-recheck:
			if s.options.Alive(ctx) {
				s.logf("le poste répond de nouveau : retour à l'écran client")
				_ = process.Kill()
				<-exited
				return switched
			}
		}
	}
}

// target decides what the browser opens, and reports whether the supervisor should
// watch for the station coming back while it is open.
//
// The answer is no in crash-loop mode, and that is the whole reason the two rescue
// reasons are not one: on the WAITING page, the station coming back is what ends the
// wait; on the ERR-KSK-02 page, the station is answering perfectly well and it is the
// page that kills the browser — switching back to it every second is the flickering
// §15.2 opened this page to stop.
func (s *Supervisor) target(ctx context.Context) (string, bool) {
	if s.rescueMode {
		return s.rescue, false
	}
	probeCtx, cancel := ports.WithBudget(ctx, s.options.Clock, ProbeBudget)
	defer cancel()
	if s.options.Alive(probeCtx) {
		return s.options.URL, false
	}
	s.logf("le poste ne répond pas encore sur %s : page de secours", s.options.URL)
	return s.rescue, true
}

// enterRescue rewrites the local page with the crash-loop wording of §15.2 and points
// the supervisor at it.
//
// From here on the browser is still relaunched — a station whose browser is closed by
// hand must come back — but it comes back on a STILL page carrying ERR-KSK-02 instead
// of flickering in front of the queue.
//
// The honest limit of the mechanism, said here rather than discovered on site: a local
// page only helps when what kills the browser is the PAGE — a fault loop on the client
// screen, a canvas the graphics driver refuses. A browser that dies on start whatever
// it is given cannot display this page either, and what names that case is control 2 of
// `openscale doctor` plus the log lines above.
func (s *Supervisor) enterRescue() {
	s.rescueMode = true
	s.logf("%s : %d arrêts de moins de %s dans la dernière heure — page de secours",
		CodeCrashLoop, s.crashes.ShortLives(), ShortLife)
	page, err := WriteRescuePage(s.options.ProfileDir, RescueCrashLoop,
		s.options.URL, s.crashes.ShortLives())
	if err != nil {
		s.logf("la page de secours n'a pas pu être écrite : %v", err)
		return
	}
	s.rescue = page
}

// wipeProfile removes the dedicated browser profile.
func (s *Supervisor) wipeProfile() error {
	if err := os.RemoveAll(s.options.ProfileDir); err != nil {
		return err
	}
	return os.MkdirAll(s.options.ProfileDir, 0o755)
}

// keepAwake renews the sleep inhibition every AwakePeriod.
func (s *Supervisor) keepAwake(ctx context.Context) {
	if s.options.Awake == nil {
		return
	}
	// SetThreadExecutionState belongs to the THREAD that called it (§15.2,
	// platform.KeepAwake): a goroutine the scheduler moves would leave the request on
	// whatever thread it happened to be on. Locking costs one OS thread for the life of
	// the kiosk process, which is the cheapest correct answer.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ticks, stop := s.options.Clock.Ticker(AwakePeriod)
	defer stop()
	// Once at start, then every period: the first thirty seconds of a station that just
	// booted are the ones a screen-blanking timer is most likely to fall into.
	if err := s.options.Awake(); err != nil {
		s.logf("%v", err)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			if err := s.options.Awake(); err != nil {
				s.logf("%v", err)
			}
		}
	}
}

// pause waits, and reports whether the wait completed rather than the supervisor
// having been stopped.
func (s *Supervisor) pause(ctx context.Context, d time.Duration) bool {
	select {
	case <-s.options.Clock.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

// logf writes one French line, stamped on the injected clock.
func (s *Supervisor) logf(format string, arguments ...any) {
	fmt.Fprintf(s.options.Out, "%s kiosk : %s\n",
		s.options.Clock.Now().Format(time.RFC3339), fmt.Sprintf(format, arguments...))
}
