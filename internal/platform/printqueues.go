package platform

// PrintQueue is one destination this machine can print a label to.
//
// « File » is the French for print QUEUE and never a file on disk — the same wording
// ports.Transport.Describe uses, so that one word means one thing from the syscall up to
// the screen (§8.4).
type PrintQueue struct {
	// Name is what goes into printer.options.queue on Windows, or into
	// printer.options.path on a system whose print destination is a device node.
	Name string
	// Detail is FRENCH and says what kind of destination this is, because « SATO WS408_2 »
	// and « \\SERVEUR\SATO WS408_2 » are two very different things to debug (§14.4).
	Detail string
	// Default reports the destination the system prints to when nobody chose. It is what
	// the first-start wizard proposes, and it is false on every platform that has no such
	// notion.
	Default bool
}
