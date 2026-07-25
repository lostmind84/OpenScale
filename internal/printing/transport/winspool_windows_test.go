//go:build windows

package transport

// What can be asserted about the Windows spooler on a machine that has no printer.
//
// The production path — a queue that opens, a document that starts, bytes that reach the
// head — needs the SATO WS408 of the bench and lives in hardware_test.go. What is
// reachable here is everything AROUND it, and it is not decoration: an unknown queue name
// is the failure a station really meets, after somebody renames a printer or clones a
// configuration from the station next door (§11.5).

import (
	"errors"
	"strings"
	"syscall"
	"testing"
)

// noSuchQueue is a name Windows cannot resolve. No backslash in it, deliberately: a name
// that starts one would be read as \\server\printer and could go looking for a machine on
// the network.
const noSuchQueue = "openscale-file-qui-n-existe-pas-9f2a"

// TestOpenSystemQueueRefusesAQueueNobodyInstalled exercises the real OpenPrinterW against
// a name that cannot resolve, and asserts the message a volunteer reads.
func TestOpenSystemQueueRefusesAQueueNobodyInstalled(t *testing.T) {
	sink, err := OpenSystemQueue(noSuchQueue)
	if err == nil {
		// A machine that really has a queue by that name would otherwise start a job on
		// it, which is not something a test gets to do.
		sink.Close()
		t.Skipf("cette machine connaît une file nommée %q", noSuchQueue)
	}
	if !strings.Contains(err.Error(), noSuchQueue) {
		t.Fatalf("le message ne nomme pas la file : %v", err)
	}
	if !strings.Contains(err.Error(), "introuvable") {
		t.Fatalf("le message ne dit pas ce qui s'est passé : %v", err)
	}
}

// TestOpenSystemQueueRefusesANameWindowsCannotEvenSpell covers the conversion to UTF-16,
// which fails on the one thing a C string cannot carry.
func TestOpenSystemQueueRefusesANameWindowsCannotEvenSpell(t *testing.T) {
	sink, err := OpenSystemQueue("SATO\x00WS408")
	if err == nil {
		sink.Close()
		t.Fatalf("un nom porteur d'un octet nul a été accepté")
	}
	if !strings.Contains(err.Error(), "nom de file invalide") {
		t.Fatalf("message inattendu : %v", err)
	}
}

// TestASpoolJobRefusesToWriteOnceItsDocumentIsEnded is the local half of the contract,
// and it needs no spooler: once EndDocPrinter has run, the handle belongs to nobody.
//
// It matters because deliver may close a job from two paths — the normal one and the
// cancellation that unblocks a parked write — and a second ClosePrinter on a released
// handle is not a diagnostic, it is a crash.
func TestASpoolJobRefusesToWriteOnceItsDocumentIsEnded(t *testing.T) {
	job := &spoolJob{closed: true}

	if n, err := job.Write([]byte("\x1bA")); err == nil {
		t.Fatalf("Write a rendu %d octets sur un travail terminé", n)
	}
	for call := 1; call <= 3; call++ {
		if err := job.Close(); err != nil {
			t.Fatalf("Close n° %d : %v", call, err)
		}
	}
	// release is the path taken when the document never started; on a job already given
	// up it must touch nothing.
	job.release()
}

// TestLastErrorSaysSomethingUseful covers the one helper that turns a Windows call into a
// sentence.
//
// Call always hands back a non-nil error taken from GetLastError, which on a SUCCESSFUL
// call reads « The operation completed successfully » — so a zero errno beside a failed
// return code means the API failed without setting a cause, and saying THAT is more
// useful than repeating that everything went well.
func TestLastErrorSaysSomethingUseful(t *testing.T) {
	for _, tc := range []struct {
		name string
		from error
		want string
	}{
		{"un errno nul", syscall.Errno(0), "n'a pas donné de cause"},
		{"aucune erreur du tout", nil, "n'a pas donné de cause"},
		{"une vraie cause", syscall.ERROR_ACCESS_DENIED, syscall.ERROR_ACCESS_DENIED.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := lastError(tc.from); !strings.Contains(got.Error(), tc.want) {
				t.Fatalf("lastError(%v) = %v, attendu quelque chose contenant %q", tc.from, got, tc.want)
			}
		})
	}
	if errors.Is(lastError(syscall.ERROR_ACCESS_DENIED), syscall.ERROR_ACCESS_DENIED) == false {
		t.Fatalf("lastError a perdu la cause d'origine, que le journal technique consigne")
	}
}
