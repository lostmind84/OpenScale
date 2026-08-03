// This file holds the SERVER ITSELF: what it is built from, what it holds, and the
// three ways the outside world reaches it.
//
// HTTPServer is where the three shutdown traps of §13.4 are closed -- BaseContext
// above all, without which cancelling the root context would not cancel a single
// request.

package web

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"openscale/internal/domain"
	"openscale/internal/station/ports"
	"sync/atomic"
	"time"
)

// maxSubscribers is how many SSE streams one station serves at once (§13.1).
//
// Eight, and the ninth is REFUSED rather than queued. A station has one screen; the
// other seven are a volunteer's phone, a second browser tab and a curl left running
// overnight. Queuing the ninth would hold an HTTP goroutine for as long as somebody
// keeps the tab open, which is the leak §13.1 claims not to have.
const maxSubscribers = 8

// probeBudget is what /healthz gives the Hub to answer one event (§14.5).
//
// It is spent on the INJECTED clock: a liveness probe that reads the wall clock
// would make the shutdown test wait for real.
const probeBudget = 500 * time.Millisecond

// deviceBudget bounds the two troubleshooting actions that really do talk to a
// device — the self-test and the fallback switch.
//
// A handler NEVER waits on hardware without a deadline: the volunteer pressing the
// button is standing in front of the screen.
const deviceBudget = 10 * time.Second

// Options is everything the HTTP layer is given. Clock and Hub are required; every
// other collaborator is optional and its absence is answered honestly.
type Options struct {
	Clock ports.Clock
	Hub   Hub
	// Controller is the station. Without it the station cannot be reconfigured and
	// /healthz falls back to probing the Hub, which is what it measures anyway.
	Controller Controller
	// Technical is where a browser error and an administrative action are recorded.
	// station.Hub.TechnicalLog() is what a real station passes.
	Technical ports.TechnicalLog

	Store           Store
	Config          ConfigStore
	Catalog         CatalogAdmin
	Hardware        Hardware
	Printer         SelfTester
	Troubleshooting Troubleshooting
	// Diagnostic builds diagnostic.zip. Nil answers 501 on that route and nothing else:
	// a station that cannot produce an archive still weighs.
	Diagnostic Diagnostician
	// Dashboard feeds the roll light, the disk light, the catalog line and the unattended
	// restart indicator of §14.4. Nil leaves those four absent from GET /admin/api/health
	// and never fails it.
	Dashboard Dashboard
	// Update installs a newer release from the screen. Nil answers 501 on the act.
	Update Updater
	// Restart stops the station so that its supervisor starts it again. Nil answers
	// 501 on that route: see Restarter.
	Restart Restarter
	// Reboot restarts the machine, after the countdown of rebootPlan. Nil answers 501.
	Reboot Rebooter

	// Assets is the built front end (internal/web/dist through //go:embed). Nil
	// serves a placeholder page rather than a 404: a station whose front end has not
	// been built still has to say so on the screen it is standing in front of.
	Assets fs.FS
	// Images is the photo directory, rooted at <data>/images, laid out as
	// <2 first characters of the sha>/<sha>.<detected extension> (§10.7).
	Images fs.FS

	// Registries are what a configuration is validated against (§11.3): the drivers
	// and templates this binary actually carries.
	Registries domain.Registries
	// Binder owns the socket, and is what makes network.listen reloadable (§11.4,
	// ADR-027). Nil disables the rebind and nothing else.
	Binder *Binder
	// Version is what the dashboard displays next to the configuration fingerprint.
	Version string
}

// Server is the HTTP layer. It is built once and is safe for concurrent use.
type Server struct {
	clock           ports.Clock
	hub             Hub
	controller      Controller
	technical       ports.TechnicalLog
	store           Store
	configStore     ConfigStore
	catalog         CatalogAdmin
	hardware        Hardware
	printer         SelfTester
	troubleshooting Troubleshooting
	diagnostician   Diagnostician
	dashboard       Dashboard
	updater         Updater
	restarter       Restarter
	// rebootPlan is the countdown before the machine restarts, or nil on a platform
	// that cannot restart — which is what the two reboot routes answer 501 on.
	rebootPlan *rebootPlan
	assets     fs.FS
	images     fs.FS
	registries domain.Registries
	binder     *Binder
	version    string

	// subscribers counts the SSE streams in flight. An atomic and not a mutex: the
	// only question asked of it is « are there already eight? », and it is asked
	// from one handler goroutine per browser.
	subscribers atomic.Int64

	// catalogPayload caches the serialized catalog, keyed by the POINTER of the
	// snapshot it was built from. A catalog is immutable and only ever replaced
	// wholesale, so a pointer that has not moved describes bytes that cannot have.
	catalogPayload atomic.Pointer[catalogPayload]

	sessions *sessionStore
	handler  http.Handler
}

// New wires the HTTP layer over a running station.
func New(o Options) (*Server, error) {
	switch {
	case o.Clock == nil:
		return nil, errors.New("web: New: pas d'horloge ; tout budget se dépense sur l'horloge INJECTÉE (§5.3)")
	case o.Hub == nil:
		return nil, errors.New("web: New: pas de Hub ; la couche HTTP ne décide de rien elle-même")
	}
	if o.Technical == nil {
		o.Technical = ports.NopTechnicalLog{}
	}
	s := &Server{
		clock: o.Clock, hub: o.Hub, controller: o.Controller, technical: o.Technical,
		store: o.Store, configStore: o.Config, catalog: o.Catalog,
		hardware: o.Hardware, printer: o.Printer, troubleshooting: o.Troubleshooting,
		diagnostician: o.Diagnostic, dashboard: o.Dashboard, updater: o.Update,
		restarter: o.Restart,
		assets:    o.Assets, images: o.Images, registries: o.Registries,
		binder: o.Binder, version: o.Version,
		sessions: newSessionStore(o.Clock),
	}
	if o.Reboot != nil {
		s.rebootPlan = newRebootPlan(o.Clock, o.Reboot.Reboot, s.reportRebootRefused)
	}
	s.handler = s.routes()
	return s, nil
}

// Handler is the whole route table of §14.5.
func (s *Server) Handler() http.Handler { return s.handler }

// ServeHTTP lets a Server be used where an http.Handler is expected.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }

// HTTPServer builds the net/http server with the wiring §13.4 says not to forget.
//
// BaseContext is the point of this function. r.Context() derives from it, and it
// defaults to context.Background: without this line, cancelling the root context
// leaves every in-flight request context alive, Shutdown waits for SSE streams that
// never become idle, and the shutdown burns its entire budget every time a browser
// is connected — that is, always.
//
// RegisterOnShutdown is the second call site of CloseSubscribers, kept ON PURPOSE:
// it covers a Shutdown triggered without going through Station.Stop. It is safe
// because CloseSubscribers is idempotent (§13.3).
func (s *Server) HTTPServer(root context.Context, closeSubscribers func()) *http.Server {
	srv := &http.Server{
		Handler:     s.handler,
		BaseContext: func(net.Listener) context.Context { return root },
		// A header that never finishes arriving must not hold a connection for ever.
		// It is a network deadline like the write deadline of stream.go, and it is
		// the only budget of this file the injected clock does not own.
		ReadHeaderTimeout: 10 * time.Second,
	}
	if closeSubscribers != nil {
		srv.RegisterOnShutdown(closeSubscribers)
	}
	return srv
}
