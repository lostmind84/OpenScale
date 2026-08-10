package platform

// supervised is the SCM, which the service handler already knows how to ask.
//
// A service registered by InstallService carries the recovery actions of §15.2 — five
// seconds, ten, thirty — AND the failure-actions flag that extends them to a stop the
// station SIGNALLED with a non-zero code, which is the one the button of the
// administration screen produces. The flag is not decoration: Windows defaults it to
// false, and the actions then cover crashes only. A binary run from a console carries
// nothing of the sort.
func supervised() bool { return StartedByServiceManager() }
