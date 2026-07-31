package platform

// PrintQueue is one destination this machine can print a label to.
//
// « File » is the French for print QUEUE and never a file on disk — the same wording
// ports.Transport.Describe uses, so that one word means one thing from the syscall up to
// the screen (§8.4).
type PrintQueue struct {
	// Name is the destination itself, as the platform spells it: « SATO WS408_2 » for a
	// Windows queue, /dev/usb/lp0 for a print node, 192.168.0.43:9100 for a host that
	// answered the network scan.
	Name string
	// Key is the printer.options key this destination goes INTO: domain.DeviceKeyQueue on
	// Windows, domain.DeviceKeyPath for a device node, domain.DeviceKeyAddress for a host
	// found on the raw print port.
	//
	// It travels with the name because only the enumeration knows it, and the screen that
	// draws the list has no way to tell one from another: the three are strings, and
	// « 192.168.0.43:9100 » looks exactly as much like a queue name as « SATO WS408_2 »
	// does. Clicking a discovered printer used to write it into printer.options.queue,
	// which validates and cannot print.
	Key string
	// Detail is FRENCH and says what kind of destination this is, because « SATO WS408_2 »
	// and « \\SERVEUR\SATO WS408_2 » are two very different things to debug (§14.4).
	Detail string
	// Default reports the destination the system prints to when nobody chose. It is what
	// the first-start wizard proposes, and it is false on every platform that has no such
	// notion.
	Default bool
}
