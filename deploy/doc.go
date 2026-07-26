// Package deploy holds no code. It holds what installs a station — the two PowerShell
// installers of §15.2, the systemd units of §15.3, the udev rules, and the scheduled task
// — and one test file that keeps them honest.
//
// # Why a Go test guards shell scripts
//
// Because the scripts and the binary agree on things nobody would notice drifting apart.
// The name of the service, the name of the scheduled task, the account, the location of
// the configuration, the list of subcommands the installer calls, and above all the stop
// budget a supervisor grants the shutdown of §13.4 — that last one is the number whose
// drift made `update.ps1` fail intermittently on a healthy station, and it is now
// compared to station.ShutdownBudget() by a test rather than copied by a human.
//
// What the test cannot do is install anything: creating a service, a local account and a
// scheduled task needs an elevated session, and a test suite that asked for one would be
// a test suite nobody runs. What it does instead is check every claim that can be checked
// without administrator rights — the scripts parse, the units carry the directives that
// matter, the backup and the restore work on a throwaway directory.
package deploy
