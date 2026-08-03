package main

import (
	"fmt"
	"io"
	"strings"

	"openscale/internal/domain"
)

// This file is what `serve` runs, and says, when the configuration it read is not what
// this binary expects: the neutral profile of §11.3 with the two blocks that must
// survive it, and the lists — every fault, every migration — written where whoever
// started the service can read them.

// fallbackProfile is what a station RUNS when its own configuration is unusable (§11.3).
//
// It is the neutral profile, plus the two things that must survive the fallback — and
// both were found by starting a station out of the box and trying to repair it from its
// own screen.
//
// # The administration block
//
// §11.3 replaces the configuration a station OPERATES ON. It has no business replacing
// the identity of whoever administers it: the password and the recovery code are the
// answer to « qui a le droit de réparer ce poste », and that answer is on the
// installation sheet, in the shop's folder, matching the hash IN THE FILE. Dropping them
// left the login form answering « aucun mot de passe n'est défini » and the recovery form
// answering « ce poste n'a pas de code de secours » — on the ONE station both exist for.
// The screen was then unreachable on exactly the station §11.3 says it must serve.
//
// # The network block
//
// Same rule, and it was learnt the same way. The neutral profile replaces what the
// station RUNS ON; it has no business replacing the way one REACHES it in order to
// repair it. Its address is 127.0.0.1:8085, which every station of the parc shares, so
// borrowing it moved a station off the address its file declares — while the kiosk, which
// reads that same file and reads it successfully because a faulty file is still a
// readable one, kept opening the declared address. A black client screen on the very
// station §11.3 exists to keep alive, and an administration screen shut back onto the
// loopback at the moment a volunteer arrives with a laptop to fix it.
//
// The address of the file is kept only while it is USABLE: when the faults name the
// network block itself, the neutral profile provides it, because a fallback that copied
// an unbindable address would turn ERR-CFG-01 — a station serving its fault list — into
// ERR-SYS-02, a station that is not there at all.
func fallbackProfile(broken domain.Config, faults []domain.Fault) domain.Config {
	cfg := domain.NeutralProfile()
	cfg.Admin = broken.Admin
	if !faultedOn(faults, "network") {
		cfg.Network = broken.Network
	}
	return cfg
}

// faultedOn reports whether any fault names a field of one configuration section.
//
// It matches the section and everything beneath it — "network" answers for
// "network.listen" — so a control added to that section later is covered without this
// function having to learn its name. Half a block is what must never be borrowed: an
// address open to the network behind an administration screen closed to it is harder to
// diagnose than a fallback that is wrong in both directions at once.
func faultedOn(faults []domain.Fault, section string) bool {
	for _, fault := range faults {
		if fault.Field == section || strings.HasPrefix(fault.Field, section+".") {
			return true
		}
	}
	return false
}

// reportFaults writes the whole list of §11.3 where whoever started the service can
// read it.
//
// ALL of them and not the first: a volunteer who came to fix one file should leave
// having fixed it, and not discover the second fault after a restart.
func reportFaults(out io.Writer, path string, faults []domain.Fault) {
	fmt.Fprintf(out, "openscale : %s comporte %d faute(s) — le poste démarre en configuration "+
		"d'usine (ERR-CFG-01) et sert l'écran d'administration :\n", path, len(faults))
	for _, fault := range faults {
		fmt.Fprintf(out, "  %s\n", fault.String())
	}
}

// reportMigration writes what this binary had to change to read the file, where whoever
// started the service can read it.
//
// It says nothing when there is nothing to say: a station whose file is already at this
// schema must not print a paragraph at every boot.
func reportMigration(out io.Writer, path string, notes []domain.MigrationNote) {
	if len(notes) == 0 {
		return
	}
	fmt.Fprintf(out, "openscale : %s a été écrit par une version précédente — %d "+
		"changement(s), appliqués EN MÉMOIRE. Le fichier n'est pas modifié ; "+
		"« openscale config migrate » l'écrit :\n", path, len(notes))
	for _, note := range notes {
		fmt.Fprintf(out, "  %s\n", note)
	}
}
