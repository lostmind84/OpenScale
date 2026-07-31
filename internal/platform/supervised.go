package platform

// Supervised reports whether SOMEBODY WILL RELAUNCH this process if it stops.
//
// It is asked before the station stops itself on purpose, and the answer decides
// whether that act is offered at all. On a developer's machine, `openscale serve` typed
// into a terminal is relaunched by nobody, and a button that stopped it would have
// turned a station off with no way left to turn it back on.
func Supervised() bool { return supervised() }
