//go:build windows

package platform

import (
	"context"
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

// This file answers the button « Lister les files » of §14.4 on the platform the whole
// parc runs on.
//
// It talks to winspool.drv through syscall for the same reason
// printing/transport/winspool_windows.go does: the standard library reaches the spooler
// with no cgo and no dependency, and the call sequence is the one any wrapper would make
// — ask for the size, allocate, enumerate.

var (
	procEnumPrinters      = winspoolDLL.NewProc("EnumPrintersW")
	procGetDefaultPrinter = winspoolDLL.NewProc("GetDefaultPrinterW")
)

// winspoolDLL is bound LAZILY: a station must still start on a machine whose spooler is
// broken. The failure then belongs to the one screen that asked for the list, instead of
// killing a process that could go on weighing.
var winspoolDLL = syscall.NewLazyDLL("winspool.drv")

// The EnumPrinters flags. LOCAL is the queues installed on this machine, CONNECTIONS the
// ones the logged-on user has mapped from a server: a station printing through
// \\SERVEUR\SATO would otherwise get an empty list and an operator would conclude the
// printer does not exist.
const (
	printerEnumLocal       = 0x00000002
	printerEnumConnections = 0x00000004
	// printerInfoLevel4 is the LIGHT level: name, server, attributes, and no round trip
	// to the device. Level 2 would carry the whole DEVMODE of every queue, which on a
	// machine with a network printer that is switched off can block for seconds — and
	// this list is drawn while a volunteer waits.
	printerInfoLevel4 = 4
)

// The printer attributes this list reports on. They are documented flags of the spooler,
// so nothing here is inferred.
const (
	printerAttributeLocal       = 0x00000040
	printerAttributeNetwork     = 0x00000010
	printerAttributeShared      = 0x00000008
	printerAttributeWorkOffline = 0x00000400
)

// printerInfo4 is PRINTER_INFO_4W, the three fields level 4 returns.
type printerInfo4 struct {
	printerName *uint16
	serverName  *uint16
	attributes  uint32
}

// PrintQueues enumerates the print queues of this machine, the default one included.
//
// A machine with NO printer at all is not a failure: the list is empty, and the
// administration screen says « aucune file d'impression n'est installée sur ce poste »,
// which is a true sentence with a remedy. An error is returned only when the spooler
// itself refused to answer.
func PrintQueues(ctx context.Context) ([]PrintQueue, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	buffer, count, err := enumeratePrinters()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}

	preferred := defaultPrinterName()
	infos := unsafe.Slice((*printerInfo4)(unsafe.Pointer(&buffer[0])), count)
	out := make([]PrintQueue, 0, count)
	for _, info := range infos {
		name := utf16String(buffer, info.printerName)
		if name == "" {
			continue
		}
		out = append(out, PrintQueue{
			Name:    name,
			Detail:  describeQueue(utf16String(buffer, info.serverName), info.attributes),
			Default: strings.EqualFold(name, preferred),
		})
	}
	return out, nil
}

// enumeratePrinters performs the two-call dance the spooler API imposes: ask how many
// bytes are needed, then enumerate into a buffer of that size.
//
// The first call FAILING is the nominal path — it fails with ERROR_INSUFFICIENT_BUFFER
// and fills the size — which is why its error is not reported and the second call's is.
func enumeratePrinters() ([]byte, uint32, error) {
	const flags = printerEnumLocal | printerEnumConnections
	var needed, returned uint32
	_, _, _ = procEnumPrinters.Call(uintptr(flags), 0, uintptr(printerInfoLevel4),
		0, 0, uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&returned)))
	if needed == 0 {
		// Nothing to enumerate: a station with no printer installed at all.
		return nil, 0, nil
	}

	buffer := make([]byte, needed)
	ok, _, callErr := procEnumPrinters.Call(uintptr(flags), 0, uintptr(printerInfoLevel4),
		uintptr(unsafe.Pointer(&buffer[0])), uintptr(needed),
		uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&returned)))
	if ok == 0 {
		return nil, 0, fmt.Errorf("les files d'impression de ce poste ne peuvent pas être "+
			"énumérées : %w. Le service « Spouleur d'impression » est-il démarré ?", callErr)
	}
	return buffer, returned, nil
}

// defaultPrinterName reports the queue Windows prints to when nobody chose, or nothing.
//
// A machine with no default printer is ordinary — a station whose only printer was
// removed — so a failure here is not one: the list simply comes back with no entry
// marked, and the wizard proposes nothing rather than the wrong thing.
func defaultPrinterName() string {
	var size uint32
	_, _, _ = procGetDefaultPrinter.Call(0, uintptr(unsafe.Pointer(&size)))
	if size == 0 {
		return ""
	}
	buffer := make([]uint16, size)
	ok, _, _ := procGetDefaultPrinter.Call(uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return ""
	}
	return syscall.UTF16ToString(buffer)
}

// describeQueue says what kind of destination a queue is, in the words of §14.4.
func describeQueue(server string, attributes uint32) string {
	parts := make([]string, 0, 3)
	switch {
	case server != "":
		parts = append(parts, "file partagée par "+server)
	case attributes&printerAttributeNetwork != 0:
		parts = append(parts, "file réseau")
	case attributes&printerAttributeLocal != 0:
		parts = append(parts, "file locale")
	}
	if attributes&printerAttributeShared != 0 {
		parts = append(parts, "partagée par ce poste")
	}
	if attributes&printerAttributeWorkOffline != 0 {
		// The one flag that changes what an operator should do next: Windows itself has
		// decided this queue is unreachable and is holding the jobs.
		parts = append(parts, "hors ligne : Windows garde les travaux en attente")
	}
	return strings.Join(parts, ", ")
}

// utf16String reads one NUL-terminated wide string the spooler wrote INSIDE buffer.
//
// # Why the buffer is a parameter
//
// The pointers PRINTER_INFO_4W carries point into the very bytes we handed the spooler, so
// the string cannot be longer than what is left of that buffer — and saying so is what
// makes the read safe. A version of this function that walked a fixed ceiling from the
// pointer was caught by `go test -race`: checkptr reported « unsafe.Slice result straddles
// multiple allocations », which is exactly the bug it exists to catch, and the detector
// found it on the first run rather than on a station.
//
// A pointer OUTSIDE the buffer is answered with nothing at all. It cannot happen with the
// spooler, and it is the one case where guessing would read this process's memory.
func utf16String(buffer []byte, p *uint16) string {
	if p == nil || len(buffer) == 0 {
		return ""
	}
	base := uintptr(unsafe.Pointer(&buffer[0]))
	at := uintptr(unsafe.Pointer(p))
	if at < base || at >= base+uintptr(len(buffer)) {
		return ""
	}
	runes := unsafe.Slice(p, (base+uintptr(len(buffer))-at)/unsafe.Sizeof(uint16(0)))
	for i, r := range runes {
		if r == 0 {
			return syscall.UTF16ToString(runes[:i])
		}
	}
	// No terminator before the end of the buffer: the spooler filled it badly, and what is
	// there is still the best answer available.
	return syscall.UTF16ToString(runes)
}
