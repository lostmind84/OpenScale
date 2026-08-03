package main

import (
	"fmt"
	"net"
	"time"

	"openscale/internal/station/ports"
	"openscale/internal/web"
)

// This file takes the socket, and tells the TWO refusals apart: an address something
// is already answering on — another instance of this application — and an address this
// machine cannot have. THE SOCKET IS THE SINGLE-INSTANCE LOCK.

// probeBudget is how long the single-instance probe waits for the address to answer.
//
// It is a NETWORK deadline in the TCP stack of the kernel, of the same nature as the
// write deadline of internal/web/stream.go, and it is spent before the injected clock
// exists as far as this decision is concerned: no business decision rests on it, and no
// test can be made to wait on it — a refused bind answers or does not answer at once.
const probeBudget = 250 * time.Millisecond

// listen opens the socket and tells the TWO failures apart.
//
// THE SOCKET IS THE SINGLE-INSTANCE LOCK (internal/web/binder.go), and that package
// deliberately leaves the discrimination to its caller: only the caller can probe the
// address. The two cases need two different sentences — an address that refuses a bind
// AND answers is another instance of this very application (ERR-SYS-01); one that
// refuses and answers nothing is an address this station cannot have (ERR-SYS-02) —
// and sending a volunteer hunting for a ghost process is the failure this tells apart.
func listen(clk ports.Clock, address string, log ports.TechnicalLog) (*web.Binder, error) {
	binder, err := web.Listen(clk, address, log)
	if err == nil {
		return binder, nil
	}
	if respondsToProbe(address) {
		return nil, &serviceFailure{Code: codeAnotherInstance, Exit: exitFatal, Err: err, Message: fmt.Sprintf(
			"une autre instance d'OpenScale est déjà lancée sur ce poste : %s répond déjà. "+
				"Arrêtez le service avant d'en lancer un second.", address)}
	}
	return nil, &serviceFailure{Code: codeCannotListen, Exit: exitFatal, Err: err, Message: fmt.Sprintf(
		"impossible d'écouter sur %s : %v. Cette adresse n'appartient pas à ce poste, "+
			"ou le port est réservé.", address, err)}
}

// respondsToProbe reports whether something is already answering on that address.
//
// A bare TCP connection and nothing more. Asking /healthz would say « and it is us »,
// which is a stronger claim than this decision needs and a weaker probe than it looks:
// an instance in « configuration d'usine » answers, one wedged mid-shutdown may not,
// and either way the remedy a volunteer reads is the same — stop what is holding the
// address before starting a second one.
func respondsToProbe(address string) bool {
	conn, err := net.DialTimeout("tcp", address, probeBudget)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
