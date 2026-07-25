//go:build !windows

package transport_test

// The twin of winspool_windows_test.go: on a platform with no Windows spooler, the
// transport still EXISTS, is still selectable in config.json, and fails with a sentence
// that says what to do instead.
//
// The distinction is not academic. A build tag on the whole transport would make
// printer.options.transport = "winspool" a compilation question: the name would simply
// not exist in a Linux binary, Config.Validate would call it « transport inconnu », and a
// station cloned from a Windows configuration (§11.5) would be refused with a message
// about a name rather than about a platform.

import (
	"context"
	"strings"
	"testing"

	"openscale/internal/printing/transport"
)

// TestTheWindowsQueueFailsCleanlyElsewhere asserts the refusal AND what it points to.
func TestTheWindowsQueueFailsCleanlyElsewhere(t *testing.T) {
	sink, err := transport.OpenSystemQueue(testQueue)
	if err == nil {
		sink.Close()
		t.Fatalf("une file d'impression Windows a été ouverte hors de Windows")
	}
	for _, expected := range []string{"Windows", "devfile"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("le message ne parle pas de %q : %v", expected, err)
		}
	}
}

// TestTheWindowsQueueIsStillSelectableElsewhere is the other half: the transport builds,
// it describes itself, and only the job fails.
func TestTheWindowsQueueIsStillSelectableElsewhere(t *testing.T) {
	queue, err := transport.NewWinspool(transport.WinspoolOptions{Queue: testQueue})
	if err != nil {
		t.Fatalf("NewWinspool : %v", err)
	}
	defer queue.Close()

	if !strings.Contains(queue.Describe(), testQueue) {
		t.Fatalf("Describe() = %q et ne nomme pas la file", queue.Describe())
	}
	if _, err := queue.Write(context.Background(), frame); err == nil {
		t.Fatalf("une impression a réussi sur une plateforme sans spouleur Windows")
	}
}
