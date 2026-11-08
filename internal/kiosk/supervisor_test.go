package kiosk

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"openscale/internal/fake"
)

// The kiosk is supervised WITHOUT a browser here, and that is the point: there is no
// browser on the machine that runs the tests, and none on the CI. What is proved below
// is the whole of §15.2's supervision — the relaunch delay, the waiting page, the
// switch back, the twenty-first crash, the sleep inhibition — in microseconds of
// simulated time.

// bench is one supervisor under test, with its fake browser and its fake clock.
type bench struct {
	clock    *benchClock
	profile  string
	launched chan *fakeBrowser
	targets  chan string
	// current is the browser launched last, so that the test knows whether the supervisor
	// is watching a live one or has a death still to notice.
	current atomic.Pointer[fakeBrowser]
	alive   atomic.Bool
	awakes  atomic.Int64
	// attached is how many client screens the station reports. Zero is the default, and
	// it is harmless: the presence watch only ever fires on a screen it has SEEN, so a
	// test that never attaches one never meets it.
	attached  atomic.Int64
	returned  chan error
	cancel    context.CancelFunc
	stationOK string

	// waitOnce lets the cleanup and a test both wait for the supervisor to return
	// without the second of them waiting on a channel the first has already drained.
	waitOnce sync.Once
	result   error
	timedOut bool
}

// wait cancels nothing: it waits for Run to have returned, once, whoever asks.
func (b *bench) wait(t *testing.T) error {
	t.Helper()
	b.waitOnce.Do(func() {
		select {
		case b.result = <-b.returned:
		case <-time.After(2 * time.Second):
			b.timedOut = true
		}
	})
	if b.timedOut {
		t.Fatal("le superviseur n'est pas revenu après l'annulation")
	}
	return b.result
}

// fakeBrowser is a browser that exits when the test says so.
type fakeBrowser struct {
	exit   chan struct{}
	once   sync.Once
	killed atomic.Bool
}

// Wait blocks until the test ends this browser.
func (f *fakeBrowser) Wait() error {
	<-f.exit
	return nil
}

// Kill ends it, exactly as killing a real process makes Wait return.
func (f *fakeBrowser) Kill() error {
	f.killed.Store(true)
	f.die()
	return nil
}

// die makes Wait return, once.
func (f *fakeBrowser) die() { f.once.Do(func() { close(f.exit) }) }

// dead reports whether this browser has exited, by the test's hand or by a Kill.
func (f *fakeBrowser) dead() bool {
	select {
	case <-f.exit:
		return true
	default:
		return false
	}
}

// benchClock is the fake clock, plus the one thing the bench must know about it: which of
// its one-shot waiters are the supervisor WAITING, and which are probe budgets nobody
// waits on any more.
//
// Every liveness question goes through ports.WithBudget, which registers an After of
// ProbeBudget. The question returns at once — Alive and Attached are synchronous here —
// and its cancel ends the goroutine that watched the budget, but a fake clock has no way
// to unregister an After: the waiter stays counted until two seconds of fake time have
// gone by. Counted as « the supervisor is parked », it let the fix of 07/08/2026 advance
// the clock while the supervisor had simply not been scheduled yet, which is the fault
// that fix was about; see nextLaunch.
type benchClock struct {
	*fake.Clock
	// mu orders After against Advance, so that a budget's deadline is computed on the
	// instant the fake clock registered it at.
	mu sync.Mutex
	// budgets are the deadlines of the probe budgets not yet come due.
	budgets []time.Time
}

// After registers a waiter on the fake clock and remembers it when it is a probe budget.
//
// A budget is recognised by its duration alone. That holds because the supervisor's own
// waits are RelaunchDelay and StartGrace, and both differ from ProbeBudget — which
// TestTheBenchTellsABudgetFromAWait keeps true.
func (c *benchClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d == ProbeBudget {
		c.budgets = append(c.budgets, c.Clock.Now().Add(d))
	}
	return c.Clock.After(d)
}

// Advance moves the fake clock, never in the middle of an After.
func (c *benchClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Clock.Advance(d)
}

// supervisorWaiters is how many one-shot waiters are the supervisor's own waits: the
// relaunch pause, the start grace.
func (c *benchClock) supervisorWaiters() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.Clock.Now()
	live := c.budgets[:0]
	for _, at := range c.budgets {
		// The fake clock fires and drops a waiter whose instant is not after now.
		if at.After(now) {
			live = append(live, at)
		}
	}
	c.budgets = live
	waiters, _ := c.Clock.Pending()
	return waiters - len(c.budgets)
}

// newBench starts a supervisor whose browser and clock the test drives.
func newBench(t *testing.T) *bench {
	t.Helper()
	return startBench(t, stationAnswers)
}

// newBenchOnAStationThatDoesNotAnswerYet is the cold boot: the service has not finished
// starting when the supervisor does, which is the case StartGrace exists for.
//
// It is a CONSTRUCTOR and not a field a test writes afterwards, and that distinction is the
// fix of 07/08/2026. The supervisor probes the station on its first instruction, so a test
// that started it and only then wrote the station down was racing that first probe — and
// lost whenever the runner was loaded enough to schedule the supervisor first. It reported
// « poste muet : ouvert sur "http://127.0.0.1:8085" », which accuses the supervisor of
// opening the client screen on a silent station when the station had simply still been
// answering at the instant it looked. Three CI runs between 30/07 and 07/08/2026.
func newBenchOnAStationThatDoesNotAnswerYet(t *testing.T) *bench {
	t.Helper()
	return startBench(t, stationIsSilent)
}

// The two states a station can be in when the supervisor starts, named rather than spelled
// `true` and `false` at the call site — the two constructors above are what tests read.
const (
	stationAnswers  = true
	stationIsSilent = false
)

func startBench(t *testing.T, answering bool) *bench {
	t.Helper()
	b := &bench{
		clock:     &benchClock{Clock: fake.NewClock(start)},
		profile:   filepath.Join(t.TempDir(), "profile"),
		launched:  make(chan *fakeBrowser, 64),
		targets:   make(chan string, 64),
		returned:  make(chan error, 1),
		stationOK: "http://127.0.0.1:8085",
	}
	b.alive.Store(answering)

	supervisor, err := New(Options{
		URL:        b.stationOK,
		Browser:    Browser{Name: "chromium", Path: "/usr/bin/chromium"},
		ProfileDir: b.profile,
		Clock:      b.clock,
		Out:        &strings.Builder{},
		Alive:      func(context.Context) bool { return b.alive.Load() },
		// Un poste muet ne répond pas non plus à cette question-là, et c'est ce que le
		// second retour dit : le superviseur ne doit jamais lire « poste injoignable »
		// comme « aucun écran ».
		Attached: func(context.Context) (int, bool) {
			if !b.alive.Load() {
				return 0, false
			}
			return int(b.attached.Load()), true
		},
		Awake: func() error { b.awakes.Add(1); return nil },
		Launch: func(_ context.Context, browser Browser, arguments []string) (Process, error) {
			process := &fakeBrowser{exit: make(chan struct{})}
			b.current.Store(process)
			b.targets <- targetOf(arguments)
			b.launched <- process
			return process, nil
		},
	})
	if err != nil {
		t.Fatalf("construction du superviseur : %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	go func() { b.returned <- supervisor.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		b.wait(t)
	})
	return b
}

// advance moves the fake clock in small steps, letting the supervisor run between
// them.
//
// In small steps and not one jump: a jump delivers the whole grace period before the
// supervisor has reached its first probe, and the test would then prove that a
// supervisor which never looked at the clock waits correctly.
func (b *bench) advance(d time.Duration) {
	const step = 100 * time.Millisecond
	for elapsed := time.Duration(0); elapsed < d; elapsed += step {
		b.clock.Advance(step)
		time.Sleep(time.Millisecond)
	}
}

// waitUntilParked blocks until the supervisor is waiting ON THE FAKE CLOCK.
//
// This is the synchronisation the whole file rests on. A fake clock only means something
// while somebody is registered to receive what it hands out; moving it at any other instant
// charges the scheduling of the machine to the timeline of the station.
func (b *bench) waitUntilParked(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if b.clock.supervisorWaiters() > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("le superviseur ne s'est jamais mis en attente de l'horloge")
		}
		time.Sleep(time.Millisecond)
	}
}

// nothingLaunched reports whether the browser has stayed closed.
func (b *bench) nothingLaunched() bool {
	select {
	case <-b.launched:
		return false
	default:
		return true
	}
}

// rescuePage is the local page as it stands on disk right now.
func (b *bench) rescuePage(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(b.profile, RescueFileName))
	if err != nil {
		t.Fatalf("relecture de la page de secours : %v", err)
	}
	return string(raw)
}

// targetOf reads the URL out of a command line, which is the argument right after
// --kiosk.
func targetOf(arguments []string) string {
	for i, argument := range arguments {
		if argument == "--kiosk" && i+1 < len(arguments) {
			return arguments[i+1]
		}
	}
	return ""
}

// nextLaunch waits for the next browser, advancing the FAKE clock until it comes.
//
// The wall-clock deadline is a guard against a hung test, never a duration under test:
// every duration this file asserts on is read from the fake clock.
func (b *bench) nextLaunch(t *testing.T) (*fakeBrowser, string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		select {
		case process := <-b.launched:
			return process, <-b.targets
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("aucun lancement de navigateur dans le budget du test")
		}
		// ★ L'HORLOGE NE BOUGE QUE SI QUELQU'UN L'ATTEND, et c'est le correctif du
		// 07/08/2026. Cette boucle avançait de 50 ms à chaque tour, y compris les tours
		// passés à attendre que l'ordonnanceur Go veuille bien exécuter le superviseur :
		// l'horloge fausse comptait donc la CHARGE DE LA MACHINE. Affamé sur un seul
		// processeur, le même test a mesuré « page d'attente revenue après 1h50m » là où le
		// superviseur avait mis quelques centaines de millisecondes — et sur la CI il a
		// franchi deux fois en une semaine l'assertion des deux secondes de §15.2, en
		// accusant le délai de grâce d'avoir été resservi alors qu'awaitStation ne tourne
		// qu'une fois.
		//
		// What « somebody waits on it » means is the fix of 08/11/2026: that one counted
		// every one-shot waiter, and every liveness probe leaves one behind for two
		// seconds of fake time — the ProbeBudget it no longer needs (see benchClock). So
		// the count was never zero after the first probe, and a browser that died while
		// the supervisor was descheduled still saw the clock run: « page d'attente revenue
		// après 2s », on the CI on 08/11/2026, by the same mechanism the 07/08 fix named.
		// The clock now moves for one of two reasons only: the supervisor is parked on a
		// wait of its own, or it is watching a browser that is still alive — the only
		// state in which its recheck ticker is what it waits for. A browser that died and
		// whose death has not yet been noticed is neither, and time stands still.
		if b.supervisorIsWaitingOnTheClock() {
			b.clock.Advance(50 * time.Millisecond)
		}
		time.Sleep(time.Millisecond)
	}
}

// supervisorIsWaitingOnTheClock reports whether advancing the fake clock now gives the
// supervisor something it is waiting for, rather than charging it time it has not yet
// been scheduled to see.
func (b *bench) supervisorIsWaitingOnTheClock() bool {
	if b.clock.supervisorWaiters() > 0 {
		return true
	}
	browser := b.current.Load()
	return browser != nil && !browser.dead()
}

// TestTheBenchTellsABudgetFromAWait keeps benchClock honest: it recognises a probe budget
// by its duration, which only works while no wait of the supervisor lasts as long.
func TestTheBenchTellsABudgetFromAWait(t *testing.T) {
	for _, wait := range []time.Duration{RelaunchDelay, StartGrace} {
		if wait == ProbeBudget {
			t.Fatalf("une attente du superviseur dure ProbeBudget (%s) : le banc la prendrait pour un budget de sonde", ProbeBudget)
		}
	}
}

// TestTheClientScreenComesBackInUnderTwoSeconds is the promise of §15.2 — « le
// superviseur relance en < 2 s » — measured on the injected clock.
func TestTheClientScreenComesBackInUnderTwoSeconds(t *testing.T) {
	b := newBench(t)

	first, target := b.nextLaunch(t)
	if target != b.stationOK {
		t.Fatalf("premier lancement sur %q, attendu l'écran client %q", target, b.stationOK)
	}

	died := b.clock.Now()
	first.die()

	_, target = b.nextLaunch(t)
	if target != b.stationOK {
		t.Fatalf("relance sur %q, attendu l'écran client %q", target, b.stationOK)
	}
	if elapsed := b.clock.Now().Sub(died); elapsed >= 2*time.Second {
		t.Fatalf("relance après %s : §15.2 promet moins de 2 s", elapsed)
	}
}

// TestAStationThatDoesNotAnswerYetShowsTheWaitingPage is what follows the grace period:
// the service is taking longer than StartGrace, and a customer must read a sentence
// instead of a browser error page.
func TestAStationThatDoesNotAnswerYetShowsTheWaitingPage(t *testing.T) {
	b := newBenchOnAStationThatDoesNotAnswerYet(t)
	b.waitUntilParked(t)
	b.advance(StartGrace)

	first, target := b.nextLaunch(t)
	if !strings.HasPrefix(target, "file:///") {
		t.Fatalf("poste muet : ouvert sur %q, attendu la page de secours locale", target)
	}
	if strings.Contains(target, b.stationOK) {
		t.Fatal("le navigateur a été ouvert sur un poste qui ne répond pas")
	}

	// The service finishes starting. The supervisor must go back to the client screen
	// ON ITS OWN, without the browser having died.
	b.alive.Store(true)
	second, target := b.nextLaunch(t)
	if target != b.stationOK {
		t.Fatalf("le poste répond et le navigateur est sur %q", target)
	}
	if !first.killed.Load() {
		t.Error("la page de secours n'a pas été refermée")
	}
	second.die()
}

// TestNothingIsShownWhileTheServiceIsStillStarting is the grace period of §15.2.
//
// At a cold boot the station is not DOWN, it is starting: the scheduled task fires five
// seconds after logon and the service, on delayed automatic start, answers later. A page
// that appears for those few seconds and is then replaced by a browser relaunch reads, to
// whoever is standing in front of the screen, as a station that failed and recovered by
// itself. Showing nothing at all is both truer and less alarming — the machine has just
// booted, a black screen is what one expects.
func TestNothingIsShownWhileTheServiceIsStillStarting(t *testing.T) {
	b := newBenchOnAStationThatDoesNotAnswerYet(t)

	b.waitUntilParked(t)
	b.advance(StartGrace / 2)
	if !b.nothingLaunched() {
		t.Fatal("un navigateur a été lancé pendant le délai de grâce")
	}

	// The service finishes starting inside the grace: the browser must then open ONCE,
	// straight on the client screen, and the customer never sees a local page.
	b.alive.Store(true)
	process, target := b.nextLaunch(t)
	if target != b.stationOK {
		t.Fatalf("le poste a répondu pendant le délai de grâce, navigateur ouvert sur %q", target)
	}
	defer process.die()
	if !b.nothingLaunched() {
		t.Fatal("le navigateur a été lancé deux fois : le client verrait un redémarrage")
	}
}

// TestTheGraceIsBoundedAndEndsOnTheStartingPage keeps the grace from becoming a black
// screen nobody can read.
//
// A service that never comes up — a database that will not open, a port already taken —
// must end on a sentence, not on the desktop of the station account.
func TestTheGraceIsBoundedAndEndsOnTheStartingPage(t *testing.T) {
	b := newBenchOnAStationThatDoesNotAnswerYet(t)
	b.waitUntilParked(t)
	b.advance(StartGrace)

	_, target := b.nextLaunch(t)
	if !strings.HasPrefix(target, "file:///") {
		t.Fatalf("après le délai de grâce, ouvert sur %q au lieu de la page locale", target)
	}
	if page := b.rescuePage(t); !strings.Contains(page, rescueTitle(RescueStarting)) {
		t.Fatalf("la page ouverte ne dit pas %q", rescueTitle(RescueStarting))
	}
}

// TestTheWordingChangesOnceTheStationHasAnswered is why there are two waiting reasons
// rather than one.
//
// « Le poste redémarre… » describes a station that is COMING BACK, and at a cold boot it
// is simply false: nothing has restarted. Once the station has answered at least once, it
// becomes the true sentence, and the same page must say so.
//
// The second thing this proves: the grace period is served ONCE. A browser that dies
// while the station is down comes back on the waiting page inside the two seconds §15.2
// promises, and not after another grace period of black screen.
func TestTheWordingChangesOnceTheStationHasAnswered(t *testing.T) {
	b := newBench(t)

	first, target := b.nextLaunch(t)
	if target != b.stationOK {
		t.Fatalf("premier lancement sur %q, attendu l'écran client", target)
	}

	b.alive.Store(false)
	died := b.clock.Now()
	first.die()

	_, target = b.nextLaunch(t)
	if !strings.HasPrefix(target, "file:///") {
		t.Fatalf("poste devenu muet : ouvert sur %q, attendu la page locale", target)
	}
	if elapsed := b.clock.Now().Sub(died); elapsed >= 2*time.Second {
		t.Fatalf("page d'attente revenue après %s : le délai de grâce a été resservi", elapsed)
	}
	page := b.rescuePage(t)
	if !strings.Contains(page, rescueTitle(RescueWaiting)) {
		t.Fatalf("un poste qui a déjà répondu doit lire %q", rescueTitle(RescueWaiting))
	}
	if strings.Contains(page, rescueTitle(RescueStarting)) {
		t.Fatalf("la page dit encore %q alors que le poste avait répondu", rescueTitle(RescueStarting))
	}
}

// TestTheTwentyFirstQuickDeathOpensTheRescuePage is the anti-flicker rule of §15.2, end
// to end: twenty deaths keep relaunching the client screen, the twenty-first opens the
// still page carrying ERR-KSK-02 — on a station that ANSWERS, which is the case the two
// rescue reasons exist to tell apart.
func TestTheTwentyFirstQuickDeathOpensTheRescuePage(t *testing.T) {
	b := newBench(t)

	for death := 1; death <= CrashLimit; death++ {
		process, target := b.nextLaunch(t)
		if target != b.stationOK {
			t.Fatalf("mort n° %d : ouvert sur %q, attendu l'écran client", death, target)
		}
		process.die()
	}

	// The twenty-first death is the one that changes the answer.
	process, target := b.nextLaunch(t)
	if target != b.stationOK {
		t.Fatalf("mort n° %d : ouvert sur %q, attendu encore l'écran client", CrashLimit+1, target)
	}
	process.die()

	_, target = b.nextLaunch(t)
	if !strings.HasPrefix(target, "file:///") {
		t.Fatalf("après %d morts rapides, ouvert sur %q au lieu de la page de secours",
			CrashLimit+1, target)
	}
	raw, err := os.ReadFile(filepath.Join(b.profile, RescueFileName))
	if err != nil {
		t.Fatalf("relecture de la page de secours : %v", err)
	}
	if !strings.Contains(string(raw), CodeCrashLoop) {
		t.Fatalf("la page ouverte ne porte pas %s", CodeCrashLoop)
	}
}

// TestTheStationDoesNotLeaveTheRescuePageOnItsOwn is the other half of the rule: on the
// ERR-KSK-02 page the station is answering, so a supervisor that rechecked liveness
// would kill and relaunch every second — the flickering the page exists to stop.
func TestTheStationDoesNotLeaveTheRescuePageOnItsOwn(t *testing.T) {
	b := newBench(t)
	for death := 1; death <= CrashLimit+1; death++ {
		process, _ := b.nextLaunch(t)
		process.die()
	}

	onRescue, target := b.nextLaunch(t)
	if !strings.HasPrefix(target, "file:///") {
		t.Fatalf("ouvert sur %q, attendu la page de secours", target)
	}
	// The station answers, and a whole minute passes. Nothing must touch this browser.
	b.clock.Advance(time.Minute)
	time.Sleep(5 * time.Millisecond)
	if onRescue.killed.Load() {
		t.Fatal("le navigateur de la page de secours a été tué : le poste se remettrait à clignoter")
	}
}

// TestTheSleepInhibitionIsRenewedWhileTheKioskRuns keeps the belt over the braces of
// powercfg (§15.2): a power plan somebody switches by hand must not put the screen to
// sleep in front of a customer.
func TestTheSleepInhibitionIsRenewedWhileTheKioskRuns(t *testing.T) {
	b := newBench(t)
	process, _ := b.nextLaunch(t)
	defer process.die()

	deadline := time.Now().Add(3 * time.Second)
	for b.awakes.Load() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("%d renouvellements de l'inhibition de veille, attendu au moins 3 "+
				"(un au démarrage puis un par %s)", b.awakes.Load(), AwakePeriod)
		}
		b.clock.Advance(AwakePeriod)
		time.Sleep(time.Millisecond)
	}
}

// TestStoppingTheSupervisorTakesTheBrowserWithIt is what a logout and a machine
// shutdown do. A browser left behind holds the profile directory, and the next start
// finds it locked.
func TestStoppingTheSupervisorTakesTheBrowserWithIt(t *testing.T) {
	b := newBench(t)
	process, _ := b.nextLaunch(t)

	b.cancel()
	if err := b.wait(t); err != nil {
		t.Fatalf("un arrêt ordinaire est revenu avec une erreur : %v", err)
	}
	if !process.killed.Load() {
		t.Fatal("le navigateur a survécu au superviseur")
	}
}

// TestTheProfileIsWipedAtEveryStart is the « profil dédié effacé à chaque démarrage » of
// §15.2: a profile that accumulates state is a station that behaves differently in month
// six.
func TestTheProfileIsWipedAtEveryStart(t *testing.T) {
	b := newBench(t)
	process, _ := b.nextLaunch(t)
	defer process.die()

	// The rescue page is the only file the supervisor itself puts there.
	entries, err := os.ReadDir(b.profile)
	if err != nil {
		t.Fatalf("lecture du profil : %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != RescueFileName {
		t.Fatalf("le profil contient %d entrée(s), attendu la seule page de secours", len(entries))
	}
}

// TestNewRefusesWhatItCannotWorkWithout keeps a misconfigured supervisor from starting
// and then failing in a loop nobody can read.
func TestNewRefusesWhatItCannotWorkWithout(t *testing.T) {
	complete := Options{
		URL: "http://127.0.0.1:8085", Browser: Browser{Path: "/usr/bin/chromium"},
		ProfileDir: t.TempDir(), Clock: fake.NewClock(start),
		Launch: func(context.Context, Browser, []string) (Process, error) { return nil, nil },
	}
	for name, break_ := range map[string]func(*Options){
		"sans adresse":    func(o *Options) { o.URL = "" },
		"sans navigateur": func(o *Options) { o.Browser = Browser{} },
		"sans profil":     func(o *Options) { o.ProfileDir = "" },
		"sans lanceur":    func(o *Options) { o.Launch = nil },
		"sans horloge":    func(o *Options) { o.Clock = nil },
	} {
		t.Run(name, func(t *testing.T) {
			options := complete
			break_(&options)
			if _, err := New(options); err == nil {
				t.Fatalf("un superviseur %s a été accepté", name)
			}
		})
	}
}
