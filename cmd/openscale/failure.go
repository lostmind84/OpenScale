package main

import (
	"context"
	"errors"

	"openscale/internal/station/ports"
	"openscale/internal/store"
)

// This file is how `openscale serve` FAILS: the technical code a volunteer reads out
// on the telephone, the exit code the service manager acts on, and the one line of the
// technical journal that survives the process.

// The exit codes a service manager reads.
const (
	// exitFailure is what any refusal of a subcommand returns: a configuration that
	// cannot be read, a database that cannot be opened. systemd restarts, the Windows
	// SCM restarts, and the install sheet says what to look at.
	exitFailure = 1
	// exitFatal is the code of §13.4: the socket could not be taken, or the server
	// stopped serving on its own. « Un poste ne peut pas tourner normalement en étant
	// mort. »
	exitFatal = 3
	// exitRestart is a stop somebody ASKED FOR, from the administration screen.
	//
	// It is non-zero ON PURPOSE, and that is the whole mechanism: a non-zero code is
	// what makes the SCM apply the recovery actions of §15.2 and systemd its
	// Restart=always. A clean 0 would be recorded as a stop nobody undoes, and the
	// station would wait for a human who thinks it is coming back.
	exitRestart = 4
)

// The technical codes of §13.4, each with the sentence a volunteer reads.
const (
	// codeAnotherInstance is ERR-SYS-01: the address refuses a bind AND answers a
	// probe. THE SOCKET IS THE SINGLE-INSTANCE LOCK — no lock file left behind by a
	// crash, no Windows named mutex — and telling this case from the next one is what
	// keeps a volunteer from hunting for a ghost process.
	codeAnotherInstance = "ERR-SYS-01"
	// codeCannotListen is ERR-SYS-02: the address refuses a bind and answers nothing.
	// It is an address this station cannot have, which is a different remedy.
	codeCannotListen = "ERR-SYS-02"
	// codeServerStopped is ERR-SYS-03: Serve returned without a shutdown having been
	// asked for.
	codeServerStopped = "ERR-SYS-03"
	// codeRestartAsked is ERR-SYS-09: a volunteer asked for a restart from the
	// administration screen.
	//
	// It is written to the technical journal BEFORE the stop, because nothing written
	// afterwards would ever be written — and because the Windows event log will record
	// this stop as « inattendu », which it is not. That line is the only place the
	// intention survives.
	codeRestartAsked = "ERR-SYS-09"
)

// serviceFailure is a failure of `openscale serve` that names its technical code and
// the exit code the service manager reads.
//
// §13.4 has `fatal` write to the text journal, to the technical journal AND to stderr,
// then exit 3. Those are three different call sites here, deliberately: the technical
// journal is written where the failure happens, because only there is the database in
// hand; stderr and the exit code belong to main, because only main can exit.
type serviceFailure struct {
	// Code is the ERR-SYS-nn a volunteer reads on the telephone.
	Code string
	// Exit is what the process returns.
	Exit int
	// Message is FRENCH and complete: it names what is wrong and what to do about it.
	Message string
	// Err is the underlying failure, kept so that errors.Is reaches it.
	Err error
}

// Error reports the code and the French sentence, which is what stderr carries.
func (f *serviceFailure) Error() string {
	if f.Code == "" {
		return f.Message
	}
	return f.Code + " : " + f.Message
}

// Unwrap yields the failure this one was built on.
func (f *serviceFailure) Unwrap() error { return f.Err }

// exitCodeFor reports the code the process returns for one error.
func exitCodeFor(err error) int {
	var failure *serviceFailure
	if errors.As(err, &failure) && failure.Exit != 0 {
		return failure.Exit
	}
	return exitFailure
}

// recordFailure writes one fatal error to the technical journal, which is the half of
// §13.4's `fatal` that survives the process.
//
// It is written SYNCHRONOUSLY and directly to the store: the Hub's journal worker may
// not be running yet, or may already have been drained. And it is written on a FRESH
// context, never on the one that is being cancelled — the line that says why the
// station is stopping must not be the first casualty of the stop.
func recordFailure(db *store.DB, clk ports.Clock, err error) {
	var failure *serviceFailure
	if !errors.As(err, &failure) {
		return
	}
	_ = db.RecordTechnical(context.Background(), store.TechnicalEntry{
		OccurredAt: clk.Now(), Level: store.LevelCritical, Source: store.LogSourceSystem,
		Code: failure.Code, Message: failure.Message, Detail: detailOf(failure.Err),
	})
}

// detailOf reports the technical tail of a failure, or nothing.
func detailOf(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
