package printing

// This file is §8.4: which printer the labels are coming out of, and what the screen
// says about it. Both switches are ASKED FOR — UseFallback and UseMain say at length why
// neither may ever be automatic — and each one forgets what the station knew about the
// printer it just left.

import (
	"context"
	"errors"
	"fmt"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// Routing is which printer the labels are coming out of, and what the screen says about
// it.
type Routing struct {
	// Fallback reports that the station is on the neighbour's printer.
	Fallback bool
	// Name is the FRENCH name of the printer in use.
	Name string
	// Banner is the PERMANENT banner of §8.4, in French. Empty on the main printer:
	// there is nothing to warn about when everything is where it belongs.
	Banner string
	// Available reports that a fallback is configured at all, which is what decides
	// whether the button « Imprimer sur l'imprimante du poste N » is offered (§14.4).
	Available bool
}

// Routing reports which printer is in use.
func (s *Service) Routing() Routing {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	r := Routing{Fallback: s.onFallback, Name: s.mainName, Available: s.fallback != nil}
	if s.onFallback {
		r.Name = s.fallbackName
		r.Banner = fmt.Sprintf("Les étiquettes sortent sur l'imprimante de secours (%s).", s.fallbackName)
	}
	return r
}

// UseFallback routes printing to the fallback printer FOR THE CURRENT SESSION (§8.4,
// bloquant-8).
//
// # Asked for, never automatic — and §8.4 is the one that decides
//
// The document describes an explicit button on the troubleshooting screen, « Imprimer
// sur l'imprimante du poste N », and a permanent banner. It is worth saying why that is
// the right call rather than a timid one, because « switch automatically when the main
// printer fails » sounds like a service.
//
// Nothing observable would trigger it honestly. What the station can see is a write
// that failed, and a write fails on a cable knocked loose for two seconds as readily as
// on a dead printer (important-7 is the same lesson from the other end: we do not
// confirm a physical event with a probe that does not observe it). An automatic switch
// would therefore move a customer's label two metres away, silently, on a transient —
// and the customer is standing at THIS station, watching a slot that stays empty.
//
// And it does not scale down the way it must. The four printers of the parc are two
// metres apart and each is the fallback of its neighbour; a network hiccup that touches
// all four would pile all four stations onto one printer, which is how a bad afternoon
// becomes a closed shop.
//
// So the switch is a human decision, taken by someone who has looked at the printer,
// and the banner is permanent because the same human has to remember to come back.
func (s *Service) UseFallback(ctx context.Context) error {
	s.stateMu.Lock()
	if s.fallback == nil {
		s.stateMu.Unlock()
		return errors.New("aucune imprimante de secours n'est configurée sur ce poste : " +
			"renseignez printer.options.fallback (transport et file de l'imprimante voisine)")
	}
	if s.onFallback {
		s.stateMu.Unlock()
		return nil
	}
	s.onFallback = true
	s.forget()
	s.stateMu.Unlock()

	s.log.Technical(domain.LevelWarn, "printer", "",
		fmt.Sprintf("Les étiquettes sont basculées sur l'imprimante de secours (%s).", s.fallbackName),
		"bascule demandée depuis l'écran de dépannage ; elle dure jusqu'au retour explicite ou "+
			"jusqu'au redémarrage du service")
	s.observeQueueAfterSwitch(ctx)
	return nil
}

// UseMain routes printing back to the main printer.
//
// Also asked for, and for the mirror reason: NOTHING tells this station that the main
// printer has been fixed. Level N1 cannot — it has not written to it since the switch —
// and the person who changed the roll or plugged the cable back in is the only one who
// knows. An automatic return would put the banner out while the labels were still
// coming out of the neighbour's printer, which is the one sentence a volunteer relies
// on to know where to walk.
func (s *Service) UseMain(ctx context.Context) error {
	s.stateMu.Lock()
	if !s.onFallback {
		s.stateMu.Unlock()
		return nil
	}
	s.onFallback = false
	s.forget()
	s.stateMu.Unlock()

	s.log.Technical(domain.LevelInfo, "printer", "",
		fmt.Sprintf("Les étiquettes repassent sur l'imprimante du poste (%s).", s.mainName),
		"retour demandé depuis l'écran de dépannage")
	s.observeQueueAfterSwitch(ctx)
	return nil
}

// target is the printer the labels are going to right now.
func (s *Service) target() ports.Printer {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.onFallback {
		return s.fallback
	}
	return s.main
}

// routedName is the French name of that printer.
func (s *Service) routedName() string {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.onFallback {
		return s.fallbackName
	}
	return s.mainName
}

// observeQueueAfterSwitch re-reads the levels that can answer immediately, so that the
// screen does not keep showing LevelNone until the next label.
func (s *Service) observeQueueAfterSwitch(ctx context.Context) { s.Observe(ctx) }

// forget drops every observation. It is called on both switches, and it is the honest
// half of the routing: what this station knew about one printer says NOTHING about
// another one, and carrying a green light across the switch would be inventing a
// measurement. The report goes back to LevelNone until something is observed.
//
// The caller holds stateMu.
func (s *Service) forget() {
	s.seen = Observations{}
	s.conclude()
}
