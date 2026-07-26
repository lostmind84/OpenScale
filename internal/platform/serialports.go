package platform

import (
	"context"
	"fmt"
	"sort"
	"strings"

	serialport "go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

// SerialPort is one port of THIS machine, with what makes it recognisable.
//
// « COM8 » names nothing to a volunteer standing in front of four identical boxes;
// « COM8 — USB Serial Port, FTDI » names a cable they can see and unplug (§14.4). That
// is the whole reason this type has three fields and not one.
type SerialPort struct {
	// Name is what goes into scale.options.port: COM8, /dev/ttyUSB0.
	Name string
	// Description is the USB wording the system knows the device by, empty on a port
	// that is not a USB adapter — a real serial header on a mainboard has no such name,
	// and inventing one would be a lie about hardware.
	Description string
	// VID and PID are the four hexadecimal digits of the USB identity, which is what
	// tells two adapters of different makes apart when their names are identical.
	VID, PID string
}

// SerialPorts enumerates the serial ports of this machine.
//
// # Why it takes a context it barely uses
//
// The enumeration is a synchronous system call — SetupDiEnumDeviceInfo on Windows, a
// walk of /sys on Linux — and neither can be interrupted half way. What the context
// does is refuse to START one that nobody is waiting for any more, which is the honest
// half: an administration screen that navigated away leaves this call to finish rather
// than pretending it was cancelled.
//
// # Why the fallback exists
//
// GetDetailedPortsList answers FunctionNotImplemented on the platforms where nobody
// wrote the USB part. A station on such a platform must still get its LIST OF NAMES,
// because a name is what scale.options.port carries and the description is only there
// to help a human pick. Losing the list over a missing description would take the
// « Détecter automatiquement » button of §14.4 away from the platform that needs it
// most.
func SerialPorts(ctx context.Context) ([]SerialPort, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	detailed, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return bareSerialPorts()
	}
	out := make([]SerialPort, 0, len(detailed))
	for _, port := range detailed {
		if port == nil || port.Name == "" {
			continue
		}
		out = append(out, SerialPort{
			Name:        port.Name,
			Description: describePort(port),
			VID:         port.VID,
			PID:         port.PID,
		})
	}
	sortPorts(out)
	return out, nil
}

// bareSerialPorts lists the names alone, which is all some platforms can say.
func bareSerialPorts() ([]SerialPort, error) {
	names, err := serialport.GetPortsList()
	if err != nil {
		return nil, fmt.Errorf("les ports série de ce poste ne peuvent pas être énumérés : %w", err)
	}
	out := make([]SerialPort, 0, len(names))
	for _, name := range names {
		out = append(out, SerialPort{Name: name})
	}
	sortPorts(out)
	return out, nil
}

// describePort is the wording the drop-down list shows next to the port name.
//
// It says what the system knows and nothing more. A port with no USB identity gets an
// empty description rather than « port série » — the screen already shows the name, and
// a label that repeats the obvious teaches an operator to stop reading labels.
func describePort(port *enumerator.PortDetails) string {
	parts := make([]string, 0, 3)
	for _, part := range []string{port.Product, port.Manufacturer} {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if containsFold(parts, part) {
			continue
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 && port.IsUSB && port.VID != "" {
		// No name, but a USB identity: the four digits are the only true thing left to
		// say, and they are enough to tell two adapters apart.
		return fmt.Sprintf("adaptateur USB %s:%s", port.VID, port.PID)
	}
	return strings.Join(parts, ", ")
}

// containsFold reports whether one of the parts already says this, case insensitively.
func containsFold(parts []string, candidate string) bool {
	for _, part := range parts {
		if strings.EqualFold(part, candidate) || strings.Contains(
			strings.ToLower(part), strings.ToLower(candidate)) {
			return true
		}
	}
	return false
}

// sortPorts puts the list in the order a human reads it.
//
// The system enumerates in the order the bus answers, which changes between two
// replugs: a drop-down list whose entries move around is a drop-down list somebody
// picks the wrong entry from.
func sortPorts(ports []SerialPort) {
	sort.Slice(ports, func(i, j int) bool { return ports[i].Name < ports[j].Name })
}
