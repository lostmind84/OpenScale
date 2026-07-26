package platform

import (
	"net"
	"path/filepath"
	"testing"
	"time"
)

// environment builds the lookup NewServiceNotifier takes, from a map.
func environment(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

// TestAStationNobodyIsListeningToStillServes is the case of every Windows station and
// of every station started from a terminal: no NOTIFY_SOCKET, and every notification a
// no-op that cannot fail.
//
// It matters because the same code path serves all three ways of starting the binary. A
// notifier that returned an error here would make `openscale serve` typed by hand print
// a failure about an init system that is not there.
func TestAStationNobodyIsListeningToStillServes(t *testing.T) {
	notifier := NewServiceNotifier(environment(nil), 42)
	if notifier.Enabled() {
		t.Fatal("un notificateur sans NOTIFY_SOCKET se croit écouté")
	}
	for name, send := range map[string]func() error{
		"READY":    notifier.Ready,
		"WATCHDOG": notifier.Alive,
		"STOPPING": notifier.Stopping,
	} {
		if err := send(); err != nil {
			t.Errorf("%s= sans socket a échoué : %v", name, err)
		}
	}
	if err := notifier.Close(); err != nil {
		t.Errorf("fermeture : %v", err)
	}
	if got := notifier.WatchdogInterval(); got != 0 {
		t.Errorf("période de chien de garde %s sans systemd, attendu zéro", got)
	}
}

// TestTheWatchdogPeriodIsReadFromTheUnit reads WATCHDOG_USEC the way systemd writes it:
// microseconds, whatever WatchdogSec said.
func TestTheWatchdogPeriodIsReadFromTheUnit(t *testing.T) {
	for name, testCase := range map[string]struct {
		values map[string]string
		want   time.Duration
	}{
		"WatchdogSec=30":            {map[string]string{watchdogUsecVariable: "30000000"}, 30 * time.Second},
		"unité sans chien de garde": {map[string]string{}, 0},
		"valeur illisible":          {map[string]string{watchdogUsecVariable: "bientôt"}, 0},
		"valeur nulle":              {map[string]string{watchdogUsecVariable: "0"}, 0},
		// NotifyAccess=main: variables inherited by a process that must NOT answer for
		// the unit. Keeping quiet gets the unit restarted; answering would hide a dead
		// main process.
		"WATCHDOG_PID d'un autre processus": {
			map[string]string{watchdogUsecVariable: "30000000", watchdogPIDVariable: "999"}, 0},
		"WATCHDOG_PID le nôtre": {
			map[string]string{watchdogUsecVariable: "30000000", watchdogPIDVariable: "42"}, 30 * time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			if got := watchdogInterval(environment(testCase.values), 42); got != testCase.want {
				t.Fatalf("période %s, attendu %s", got, testCase.want)
			}
		})
	}
}

// TestTheThreeNotificationsReachSystemd is the protocol itself, against a real datagram
// socket.
//
// It only runs where a unixgram socket exists — Linux, which is the only platform that
// has systemd — and it is skipped on Windows, whose AF_UNIX implementation has no
// datagram support. That is a genuine limit of this suite and it is written here rather
// than hidden: on a Windows development machine, what is covered is everything above.
func TestTheThreeNotificationsReachSystemd(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "notify.sock")
	listener, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: socket, Net: "unixgram"})
	if err != nil {
		t.Skipf("pas de socket datagramme sur cette plateforme : %v", err)
	}
	defer func() { _ = listener.Close() }()

	notifier := NewServiceNotifier(environment(map[string]string{
		notifySocketVariable: socket,
		watchdogUsecVariable: "30000000",
	}), 0)
	if !notifier.Enabled() {
		t.Fatal("le notificateur n'a pas trouvé la socket que le test vient de créer")
	}
	defer func() { _ = notifier.Close() }()

	if got := notifier.WatchdogInterval(); got != 30*time.Second {
		t.Fatalf("période %s, attendu 30s", got)
	}

	for _, testCase := range []struct {
		send func() error
		want string
	}{
		{notifier.Ready, "READY=1"},
		{notifier.Alive, "WATCHDOG=1"},
		{notifier.Stopping, "STOPPING=1"},
		{func() error { return notifier.Status("poste en écoute") }, "STATUS=poste en écoute"},
	} {
		if err := testCase.send(); err != nil {
			t.Fatalf("envoi de %s : %v", testCase.want, err)
		}
		buffer := make([]byte, 128)
		if err := listener.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatalf("échéance de lecture : %v", err)
		}
		read, err := listener.Read(buffer)
		if err != nil {
			t.Fatalf("lecture de %s : %v", testCase.want, err)
		}
		if got := string(buffer[:read]); got != testCase.want {
			t.Fatalf("datagramme %q, attendu %q", got, testCase.want)
		}
	}
}

// TestAnAbstractSocketNameIsNotOpenedAsAFile guards the one mistake that turns a
// notifier into a file nobody reads.
//
// systemd names its socket @/org/freedesktop/systemd1/notify/… : the @ stands for the
// Linux abstract namespace, whose name really begins with a NUL byte. Passing the @
// through would create a FILE called "@…" in the working directory of the service, every
// notification would land in it, and the unit would time out at start while the station
// serves perfectly — a failure that looks exactly like a working notifier.
//
// Like the test above, the assertion only carries weight where unixgram exists: on
// Windows every dial fails, so nothing could have been created anyway. It is the Linux
// run — the CI, and the stations of §15.3 — that makes it a guard.
func TestAnAbstractSocketNameIsNotOpenedAsAFile(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)

	notifier := NewServiceNotifier(environment(map[string]string{
		notifySocketVariable: "@openscale-test-abstract-socket",
	}), 0)
	defer func() { _ = notifier.Close() }()
	_ = notifier.Ready()

	entries, err := filepath.Glob(filepath.Join(directory, "*"))
	if err != nil {
		t.Fatalf("lecture du répertoire : %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("un nom de socket abstrait a créé %v sur le disque", entries)
	}
}
