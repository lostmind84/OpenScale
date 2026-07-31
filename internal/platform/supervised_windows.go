package platform

// supervised is the SCM, which the service handler already knows how to ask.
//
// A service registered by InstallService carries the recovery actions of §15.2 — five
// seconds, ten, thirty — which the SCM applies to a NON-ZERO exit code. A binary run
// from a console carries nothing of the sort.
func supervised() bool { return StartedByServiceManager() }
