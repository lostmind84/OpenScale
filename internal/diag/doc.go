// Package diag is the diagnosis: `openscale doctor`, the health of a station, and
// diagnostic.zip (§15.4).
//
// # The one sentence that shapes this package
//
// « Un diagnostic qui dit "échec" sans dire quoi faire n'a rien diagnostiqué. » Every
// control therefore answers THREE questions and not one: what was checked, how it came
// out, and what to DO about it — the last one in French, complete, addressed to a
// volunteer who is alone in front of a station on a Saturday morning. Control.Remedy is
// never empty when the verdict is not green, and a test of this package enforces it.
//
// # It has to work when nothing else does
//
// §15.1 says `openscale doctor` runs « même quand le service ne démarre pas », which is
// the whole point of the L8 criterion (§18): it « diagnostique un service qui ne démarre
// pas et dit POURQUOI ». So the doctor reads the configuration file, the database and
// the operating system DIRECTLY. It never needs the HTTP layer to be up.
//
// Three of the fifteen controls are the exception, and it is a deliberate one:
// « file d'impression visible depuis le contexte du service », « cadence balance
// observée » and « répertoire catalogue accessible tel que le service le voit » are
// questions only the running service can answer honestly. A doctor run by an
// administrator has the administrator's rights and the administrator's HKCU, so
// answering them locally would answer a DIFFERENT question and would answer it wrong
// (important-11). Those three go through GET /admin/api/health, and when the service is
// silent they say so and say how to make them knowable.
//
// # Nothing here restarts anything
//
// §15.3 puts it as the most important rule of its section: « Les pannes de périphérique
// se dégradent, elles ne redémarrent rien. » This package observes and reports. It owns
// no watchdog, it feeds none, and no caller of it may.
package diag
