// Package station is the orchestration of the weighing station: the Hub, its two
// workers, its supervisor, its idempotency cache and the hot reload of §11.4.
//
// # One goroutine, one clock
//
// Everything that DECIDES happens in one goroutine — Hub.run — and the only place
// the model changes is the call to domain.Transition, which is pure. Now, the age
// of a measurement and its expiry all come from the INJECTED clock (ports.Clock):
// no line of this package calls time.Now, and `go run ./tools/boundary` fails if
// one ever does.
//
// That single rule is what closes bloquant-1. The age of a measurement is COMPUTED
// as Now - Measurement.Timestamp and never accumulated tick by tick, so a lost tick
// — a full catalog import, a VACUUM INTO, the weekly integrity check — can no
// longer UNDER-COUNT it and let an expired weight reach a label.
//
// # What is shared, and how
//
// The loop owns its fields outright: no mutex protects h.model, h.seq or
// h.subscribers, because a single goroutine writes them. Subscribing,
// unsubscribing and closing subscriber channels are SERIALIZED through
// h.subscriptions, served by the same select as the commands — that is the whole
// reason the channel exists, and a mutex over the subscriber map would reopen the
// race this design closes.
//
// What crosses goroutines crosses through an atomic pointer to an IMMUTABLE value:
// the configuration, the catalog, the last published snapshot, the health the
// supervisor observed. Nothing mutable is ever published.
//
// # This package knows no driver
//
// It sees ports.Scale, ports.Printer, ports.CatalogSource and ports.Clock, and
// nothing else — cut 2 of §5.2. Re-instantiating a scale after a configuration
// change therefore goes through an injected factory, not through a registry this
// package would have to import.
package station
