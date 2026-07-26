//go:build windows

package platform

import (
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// TestTheQueueWordingSaysWhatAnOperatorHasToDoNext covers the four documented spooler flags.
//
// Only one of them changes what somebody does: WORK_OFFLINE means Windows itself has decided
// the queue is unreachable and is holding the jobs, which is a different remedy from « la
// file existe et l'imprimante ne répond pas ». The others are descriptive, and they are
// checked so that the wording cannot drift into English or into jargon.
func TestTheQueueWordingSaysWhatAnOperatorHasToDoNext(t *testing.T) {
	for _, c := range []struct {
		what       string
		server     string
		attributes uint32
		want       []string
	}{
		{"file locale", "", printerAttributeLocal, []string{"file locale"}},
		{"file réseau", "", printerAttributeNetwork, []string{"file réseau"}},
		{"file d'un serveur", `\\SERVEUR`, printerAttributeNetwork,
			[]string{"file partagée par", `\\SERVEUR`}},
		{"partagée par ce poste", "", printerAttributeLocal | printerAttributeShared,
			[]string{"file locale", "partagée par ce poste"}},
		{"hors ligne", "", printerAttributeLocal | printerAttributeWorkOffline,
			[]string{"hors ligne", "travaux en attente"}},
	} {
		t.Run(c.what, func(t *testing.T) {
			got := describeQueue(c.server, c.attributes)
			for _, fragment := range c.want {
				if !strings.Contains(got, fragment) {
					t.Fatalf("libellé %q, il manque %q", got, fragment)
				}
			}
		})
	}
}

// TestAWideStringOutsideTheBufferIsAnsweredWithNothing is the guard `go test -race` taught
// this file to have.
//
// The pointers the spooler hands back point INSIDE the buffer we gave it, so the length of a
// name is bounded by what is left of that buffer. A pointer from anywhere else is answered
// with nothing at all: it cannot happen with the spooler, and it is the one case where
// guessing a length would read this process's memory.
func TestAWideStringOutsideTheBufferIsAnsweredWithNothing(t *testing.T) {
	buffer := make([]byte, 64)
	name, err := syscall.UTF16FromString("SATO WS408_2")
	if err != nil {
		t.Fatalf("encodage : %v", err)
	}
	// Inside: the name is copied into the buffer and read back from it, which is exactly
	// what PRINTER_INFO_4W describes.
	inside := (*uint16)(unsafe.Pointer(&buffer[0]))
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(&buffer[0])), len(buffer)/2), name)
	if got := utf16String(buffer, inside); got != "SATO WS408_2" {
		t.Fatalf("chaîne lue %q, attendu « SATO WS408_2 »", got)
	}

	if got := utf16String(buffer, &name[0]); got != "" {
		t.Fatalf("une chaîne hors du tampon a été lue : %q", got)
	}
	if got := utf16String(buffer, nil); got != "" {
		t.Fatalf("un pointeur nul a produit %q", got)
	}
	if got := utf16String(nil, inside); got != "" {
		t.Fatalf("un tampon vide a produit %q", got)
	}
}

// TestAStringWithNoTerminatorStopsAtTheEndOfTheBuffer is the badly filled buffer.
//
// It cannot happen with the spooler either, and the answer is still bounded: what is there is
// the best answer available, and it is never longer than the allocation.
func TestAStringWithNoTerminatorStopsAtTheEndOfTheBuffer(t *testing.T) {
	buffer := make([]byte, 8)
	runes := unsafe.Slice((*uint16)(unsafe.Pointer(&buffer[0])), len(buffer)/2)
	for i := range runes {
		runes[i] = 'A'
	}
	if got := utf16String(buffer, (*uint16)(unsafe.Pointer(&buffer[0]))); got != "AAAA" {
		t.Fatalf("chaîne lue %q, attendu quatre A bornés par le tampon", got)
	}
}
