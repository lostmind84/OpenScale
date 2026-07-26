package platform

import (
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// The environment variables systemd sets on a service it started, and the only
// contract between this file and the init system.
//
// There is no build tag on this file, and none is needed: the protocol is three
// short strings on a datagram socket, and a Windows service is simply never started
// with NOTIFY_SOCKET set. What would need a tag is the socket family, and net.Dial
// answers « unknown network unixgram » there — which is exactly the same outcome as
// « nobody is listening », handled below as such.
const (
	notifySocketVariable = "NOTIFY_SOCKET"
	watchdogUsecVariable = "WATCHDOG_USEC"
	watchdogPIDVariable  = "WATCHDOG_PID"
)

// ServiceNotifier reports this process's own state to the init system that started
// it. It is the `Type=notify` half of the unit of §15.3.
//
// It says THREE things and never asks any: ready, still alive, stopping. What it must
// never carry is the state of a device — §15.3 makes that the most important rule of
// the section, because a watchdog fed by the health of a printer restarts a station
// when a roll of labels runs out.
type ServiceNotifier struct {
	// conn is nil when this process was not started by systemd, which is the case on
	// every Windows station and on any station started by hand. Every method is then a
	// no-op, on purpose: the same code path serves both, and a supervisor that only works
	// under systemd is a supervisor nobody can debug from a terminal.
	conn net.Conn
	// watchdog is WatchdogSec as systemd computed it, and zero when the unit does not
	// ask for a watchdog at all.
	watchdog time.Duration
}

// NewServiceNotifier builds the notifier from the environment.
//
// The lookup is INJECTED rather than read from os.Getenv here, because that is the
// whole surface a test has to drive: a socket that does not exist, a WATCHDOG_USEC
// meant for another process, a unit with no watchdog at all.
func NewServiceNotifier(lookup func(string) string, pid int) *ServiceNotifier {
	n := &ServiceNotifier{}
	address := lookup(notifySocketVariable)
	if address == "" {
		return n
	}
	// A leading @ is the Linux abstract namespace, whose name really starts with a NUL
	// byte. Passing the @ through would open a FILE called "@…" in the working
	// directory of the service, and every notification would then be written to a file
	// nobody reads — a silent failure that looks exactly like a working notifier.
	if strings.HasPrefix(address, "@") {
		address = "\x00" + address[1:]
	}
	conn, err := net.Dial("unixgram", address)
	if err != nil {
		// Nothing to report to: not started by systemd, or started by something that
		// set the variable and then went away. Either way the station serves.
		return n
	}
	n.conn = conn
	n.watchdog = watchdogInterval(lookup, pid)
	return n
}

// watchdogInterval reports the WatchdogSec of the unit, or zero when this process is
// not the one expected to answer for it.
func watchdogInterval(lookup func(string) string, pid int) time.Duration {
	// WATCHDOG_PID exists for NotifyAccess=all, where a child could feed the watchdog
	// of its parent. §15.3 sets NotifyAccess=main, so a mismatch means the variables
	// were inherited by something that must NOT answer: keeping quiet gets the unit
	// restarted, feeding it would hide a dead main process.
	if owner := lookup(watchdogPIDVariable); owner != "" && owner != strconv.Itoa(pid) {
		return 0
	}
	microseconds, err := strconv.ParseInt(lookup(watchdogUsecVariable), 10, 64)
	if err != nil || microseconds <= 0 {
		return 0
	}
	return time.Duration(microseconds) * time.Microsecond
}

// Enabled reports whether an init system is actually listening.
func (n *ServiceNotifier) Enabled() bool { return n != nil && n.conn != nil }

// WatchdogInterval reports how often the unit expects to hear that the station is
// alive, and zero when it expects nothing.
func (n *ServiceNotifier) WatchdogInterval() time.Duration {
	if n == nil {
		return 0
	}
	return n.watchdog
}

// Ready tells the init system the station is serving.
//
// It is sent when the socket is open and the routes answer, never when the process
// starts: `Type=notify` exists so that whatever systemd orders After= us waits for a
// station that really serves, and announcing readiness earlier would give that away
// for nothing.
func (n *ServiceNotifier) Ready() error { return n.send("READY=1") }

// Alive feeds the watchdog for one period.
func (n *ServiceNotifier) Alive() error { return n.send("WATCHDOG=1") }

// Stopping tells the init system the shutdown of §13.4 has begun, so that the time
// spent draining the print worker is not read as a process that hung.
func (n *ServiceNotifier) Stopping() error { return n.send("STOPPING=1") }

// Status sets the one line `systemctl status` shows. It is FRENCH: it is read by
// whoever is standing in front of the station.
func (n *ServiceNotifier) Status(line string) error { return n.send("STATUS=" + line) }

// send writes one datagram, and treats having nobody to talk to as success.
func (n *ServiceNotifier) send(message string) error {
	if !n.Enabled() {
		return nil
	}
	_, err := n.conn.Write([]byte(message))
	return err
}

// Close releases the socket. A closed notifier keeps answering: a shutdown must not
// depend on the order in which two defers run.
func (n *ServiceNotifier) Close() error {
	if !n.Enabled() {
		return nil
	}
	conn := n.conn
	n.conn = nil
	return conn.Close()
}

// SystemEnv is the lookup NewServiceNotifier gets in production.
func SystemEnv(name string) string { return os.Getenv(name) }
