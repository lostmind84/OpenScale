package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"

	"openscale/internal/station"
	"openscale/internal/station/ports"
	"openscale/internal/store"
)

// This file holds the three adapters `serve` needs that belong to no screen: the store
// seen as the station's technical sink, the journal relay for the interval before the
// Hub exists, and the HTTP server handed over after the station was built. The
// adapters of the administration routes are in admin.go.

// technicalSink adapts the store to what the station writes technical lines through.
//
// Two structures that carry the same six values, and the conversion is the price of cut
// 3 of §5.2: internal/station names no storage type, so it declares what it needs and
// the composition root joins the two.
type technicalSink struct{ db *store.DB }

// RecordTechnical appends one line to the persisted technical journal.
func (s technicalSink) RecordTechnical(ctx context.Context, e station.TechnicalEntry) error {
	return s.db.RecordTechnical(ctx, store.TechnicalEntry{
		OccurredAt: e.At, Level: e.Level, Source: e.Source,
		Code: e.Code, Message: e.Message, Detail: e.Detail,
	})
}

// relayLog is the technical journal the drivers are given BEFORE the Hub that owns one
// exists.
//
// The interval is real and short — a driver is built, then the station, then the Hub —
// and a driver that reported a bad option during it would otherwise report it into
// nothing. Until the Hub is attached the lines go to the console, where whoever started
// the service by hand can read them; afterwards they go where every other line goes.
type relayLog struct {
	fallback io.Writer

	mu     sync.RWMutex
	target ports.TechnicalLog
}

// attach points the relay at the journal of the running station.
func (l *relayLog) attach(target ports.TechnicalLog) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.target = target
}

// Technical records one event.
func (l *relayLog) Technical(level, source, code, message, detail string) {
	l.mu.RLock()
	target := l.target
	l.mu.RUnlock()
	if target != nil {
		target.Technical(level, source, code, message, detail)
		return
	}
	fmt.Fprintf(l.fallback, "openscale [%s] %s %s : %s %s\n", level, source, code, message, detail)
}

// heldServer is the HTTP server as Station.Stop sees it, handed over after the station
// was built.
//
// station.Options.Server is fixed at construction and the server cannot exist before
// the Hub whose subscribers it closes on shutdown. Rather than move the shutdown
// sequence out of Station.Stop — which is where §13.4 is written and tested — the
// composition root hands over a holder and fills it one line later.
type heldServer struct {
	mu     sync.RWMutex
	server *http.Server
}

// hold puts the server in place. It is called once, before anything can serve.
func (h *heldServer) hold(server *http.Server) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.server = server
}

// Shutdown stops accepting and waits for the active requests, up to ctx.
//
// A holder that was never filled shuts nothing down and says so with a nil error: the
// station failed before it ever served, and a shutdown that reported a failure there
// would name a server that does not exist.
func (h *heldServer) Shutdown(ctx context.Context) error {
	h.mu.RLock()
	server := h.server
	h.mu.RUnlock()
	if server == nil {
		return nil
	}
	return server.Shutdown(ctx)
}
