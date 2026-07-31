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
	// StationRecheck is how often the rescue page asks whether the station is back, and
	// how often the client screen is asked whether it is still attached.
	// One second: a customer watching « Le poste redémarre… » is watching a station
	// that is about to work, and the wait must not be added to the service's own.
	StationRecheck = 1 * time.Second
	// AbsenceGrace is how long the client screen may be gone before the browser is
	// relaunched on it.
	//
	// It is the time a customer spends in front of a page that is not the grid, so it is
	// short; and it is the time a screen may take to reconnect its stream after a hiccup,
	// so it is not one second. Fifteen covers the reconnection of an EventSource — which
	// retries on its own, in units of seconds — without covering a browser that LEFT.
	AbsenceGrace = 15 * time.Second
	// StartGrace is how long a station that has NEVER answered is given before anything
	// is put on the screen.
	//
	// It buys the ordinary cold boot: the kiosk task fires five seconds after logon, the
	// service is on delayed automatic start, and the twenty seconds below cover the gap
	// on a station whose AutoStartDelay has been shortened. Inside the grace the screen
	// stays black — which is what a machine that has just booted looks like anyway —
	// instead of showing a page for a few seconds and then restarting the browser in
	// front of whoever is watching. Beyond it, the page appears: a grace that never ended
	// would be a black screen nobody can read.
	StartGrace = 20 * time.Second
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
	// Attached answers « how many client screens are looking at this station, and did
	// the station answer at all? ». In production it is one GET on /api/v1/screens.
	//
	// It is what sees a browser that LEFT the application — the search a context menu
	// offered, a link — which is the one failure the process watch above is blind to: the
	// browser is alive, full screen, and showing something else. A false answer here
	// costs a relaunch in front of a customer, so the two return values are separate:
	// « the station did not answer » is never read as « no screen ».
	//
	// Nil is legitimate and turns the watch off. It is what a supervisor built by a test
	// that is not about presence passes, and what a station whose service is too old to
	// serve the route falls back to.
	Attached func(ctx context.Context) (int, bool)
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
	// rescue is the file:// URL of the local page, written at start and rewritten
	// whenever the reason it carries changes.
	rescue string
	// rescueReason is what the page on disk currently says, so that it is rewritten when
	// the answer changes and never once per relaunch.
	rescueReason RescueReason
	// answered is true once the station has served at least one /healthz in the life of
	// this supervisor. It is what tells « the poste is starting » from « the poste is
	// coming back », which are the same silence and two different sentences.
	answered bool
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
	// Written here and not at the first need: a profile directory that refuses to be
	// written to is a station that will never show anything, and it must fail now rather
	// than in front of a customer.
	rescue, err := WriteRescuePage(s.options.ProfileDir, RescueStarting, s.options.URL, 0)
	if err != nil {
		return err
	}
	s.rescue, s.rescueReason = rescue, RescueStarting

	go s.keepAwake(ctx)

	s.logf("superviseur démarré : %s sur %s", s.options.Browser.Name, s.options.URL)
	s.awaitStation(ctx)

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

// awaitStation gives a station that has never answered StartGrace to come up, showing
// nothing at all in the meantime.
//
// It runs ONCE, before the first browser of this supervisor. Afterwards a station that
// goes silent gets the waiting page inside the two seconds §15.2 promises: the grace
// covers a boot, not a failure, and re-serving it later would turn a browser that died at
// noon into twenty seconds of black screen in front of the queue.
func (s *Supervisor) awaitStation(ctx context.Context) {
	if s.answering(ctx) {
		return
	}
	s.logf("le poste ne répond pas encore : %s d'attente avant d'afficher quoi que ce soit", StartGrace)

	ticks, stop := s.options.Clock.Ticker(StationRecheck)
	defer stop()
	grace := s.options.Clock.After(StartGrace)
	for {
		select {
		case <-ctx.Done():
			return
		case <-grace:
			s.logf("le poste n'a pas répondu en %s : page de démarrage", StartGrace)
			return
		case <-ticks:
			if s.answering(ctx) {
				return
			}
		}
	}
}

// answering reports whether the station serves, and remembers a yes for good.
//
// The budget is spent in the kernel's TCP stack and bounds one question, never a business
// decision — the same ProbeBudget the liveness probe carries on its own client.
func (s *Supervisor) answering(ctx context.Context) bool {
	probeCtx, cancel := ports.WithBudget(ctx, s.options.Clock, ProbeBudget)
	defer cancel()
	if !s.options.Alive(probeCtx) {
		return false
	}
	s.answered = true
	return true
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
	case wandered:
		// Nor is this one, and it is the one that would hurt most: a station whose screen
		// keeps being brought back would count its own repairs as failures and end up on
		// ERR-KSK-02 — « prévenez un responsable » about a poste that repaired itself.
		s.logf("plus aucun écran client attaché depuis %s : le navigateur a quitté l'application, relance dans %s",
			AbsenceGrace, RelaunchDelay)
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
	// wandered is us killing the browser because it is no longer showing the client
	// screen — the one failure a process watch cannot see, since nothing died.
	wandered
)

// screenWatch is what the supervisor remembers between two presence questions.
//
// It lives for ONE showing of the browser and is dropped with it: a relaunch starts from
// « no screen has been seen yet », which is exactly what a browser that has just been
// started is.
type screenWatch struct {
	// seen is true once a client screen has been attached during this showing.
	//
	// Without it, the fifteen seconds of grace would be counted from the launch of the
	// browser — and a station slow enough to spend them opening the page would kill the
	// browser that was about to appear, then do it again, and again. The watch is for a
	// screen that WAS there and went away, and nothing else.
	seen bool
	// absentSince is when the last attached screen went away. Zero while one is there.
	absentSince time.Time
}

// watch waits for the browser to die, for the station to come back, for the client screen
// to leave the application, or for the supervisor to be stopped.
//
// The two questions the ticker asks are EXCLUSIVE, and which one it asks is which page is
// showing. On the rescue page, « is the station back? » ends the wait; on the client
// screen, that question has no meaning — the station is answering, that is why the screen
// is up — and the one worth asking is « is anybody still looking at it? ».
func (s *Supervisor) watch(ctx context.Context, process Process, exited <-chan struct{}, onRescue bool) outcome {
	// The ticker only exists when one of the two questions is live: a ticker nobody
	// reads is a timer that leaks.
	watching := !onRescue && s.options.Attached != nil
	var recheck <-chan time.Time
	if onRescue || watching {
		ticks, stop := s.options.Clock.Ticker(StationRecheck)
		defer stop()
		recheck = ticks
	}
	screen := screenWatch{}

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
			if onRescue {
				if s.answering(ctx) {
					s.logf("le poste répond de nouveau : retour à l'écran client")
					_ = process.Kill()
					<-exited
					return switched
				}
				continue
			}
			if s.screenLeft(ctx, &screen) {
				_ = process.Kill()
				<-exited
				return wandered
			}
		}
	}
}

// screenLeft reports whether the browser has stopped showing the client screen for longer
// than the grace.
//
// It is written to be WRONG IN ONE DIRECTION ONLY. Every uncertainty — a station that did
// not answer, a screen that has never attached during this showing — resets or holds the
// count, because the cost of not firing is a page a volunteer closes by hand, and the cost
// of firing wrongly is a browser killed in front of a customer in the middle of weighing.
func (s *Supervisor) screenLeft(ctx context.Context, screen *screenWatch) bool {
	probeCtx, cancel := ports.WithBudget(ctx, s.options.Clock, ProbeBudget)
	defer cancel()

	attached, answered := s.options.Attached(probeCtx)
	if !answered {
		// Nothing is known about the screen. A station that is restarting must not have
		// its browser killed on top of it — and when the station really is gone, the
		// rescue page of target() is what covers the customer, not this.
		screen.absentSince = time.Time{}
		return false
	}
	if attached > 0 {
		screen.seen = true
		screen.absentSince = time.Time{}
		return false
	}
	if !screen.seen {
		return false
	}
	now := s.options.Clock.Now()
	if screen.absentSince.IsZero() {
		screen.absentSince = now
		return false
	}
	return now.Sub(screen.absentSince) >= AbsenceGrace
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
	if s.answering(ctx) {
		return s.options.URL, false
	}
	reason := RescueStarting
	if s.answered {
		reason = RescueWaiting
	}
	s.logf("le poste ne répond pas sur %s : %s", s.options.URL, rescueTitle(reason))
	s.showRescue(reason)
	return s.rescue, true
}

// showRescue rewrites the local page when what it has to say has changed.
//
// When it has changed, and not before every launch: the page is rewritten twice in the
// life of an ordinary station — never, or once when a station that had answered goes
// silent — and a file rewritten every second would be a disk woken up for nothing.
func (s *Supervisor) showRescue(reason RescueReason) {
	if s.rescueReason == reason {
		return
	}
	page, err := WriteRescuePage(s.options.ProfileDir, reason, s.options.URL, s.crashes.ShortLives())
	if err != nil {
		// The page already on disk carries the other wording, which is still true enough
		// to read: showing it beats showing the browser's own error page.
		s.logf("la page locale n'a pas pu être réécrite : %v", err)
		return
	}
	s.rescue, s.rescueReason = page, reason
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
	s.showRescue(RescueCrashLoop)
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
