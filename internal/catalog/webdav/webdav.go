// Package webdav reads the catalog from the share the cooperative really uses.
//
// It is kept in V1 against the advice of important-14, and the reason is not
// theoretical: the production share is a WebDAV host in HTTPS on a NON-STANDARD PORT
// (https://dav.example.org:8001/), mounted as drive Z:. A drive letter is a mapping
// PER USER SESSION, invisible from a Windows service, and a UNC path cannot reach an
// HTTPS host on a non-standard port. Without this source, the real supply chain of
// the shop does not work — that is the difference between delivering and not
// delivering (§10.1).
//
// The sequence is the one of the local drop, spelled in HTTP: PROPFIND for the size
// and the date, the same stability rule, GET to read, and DELETE as the
// acknowledgement.
package webdav

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"openscale/internal/catalog"
	"openscale/internal/catalog/csvodoo"
	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// This file is what a configuration BUILDS: the timeouts and the shipped values of
// §11.2, the Source and its fields, the URL and the HTTP client New derives from
// catalog.options, and the Descriptor the administration screen generates its form
// from. What the station then ASKS of that source is in source.go, and what goes on
// the wire in dav.go.

// The two explicit timeouts of §10.1. They bound the network and nothing else: no
// business decision rests on them.
const (
	connectTimeout = 10 * time.Second
	bodyBudget     = 120 * time.Second
)

// attemptsBeforeAlarm is the third consecutive failure §10.1 calls a retry budget.
//
// THE DELAY BETWEEN ATTEMPTS IS NOT INVENTED HERE. The specification asks for three
// retries and gives no interval; the interval used is `poll_interval_s`, which is
// declared, configured and shown on the dashboard. What this constant decides is
// therefore only when a warning becomes an error — after three consecutive polls that
// could not reach the share.
const attemptsBeforeAlarm = 3

// The shipped values of §11.2.
const (
	defaultPollInterval = 5 * time.Second
	defaultStablePolls  = 2
	defaultMaxArchives  = 30
	defaultArchiveDays  = 60
)

// Label is the wording a volunteer reads in the drop-down list.
const Label = "Partage WebDAV"

// Source reads a catalog over WebDAV.
type Source struct {
	client   *http.Client
	file     *url.URL
	folder   *url.URL
	fileName string
	username string
	password string

	interval   time.Duration
	stability  *catalog.Stability
	archive    *catalog.Archive
	quarantine *catalog.Quarantine
	parse      csvodoo.Options
	clock      ports.Clock
	log        ports.TechnicalLog

	// wake carries ONE immediate poll, asked for from the screen (§14.4, « Recharger le
	// catalogue »). On a share it is the button that matters most: the interval is
	// measured in minutes there, and a volunteer who has just been told the producer
	// re-exported must not wait for it.
	wake chan struct{}

	// mu guards pending and closed, and NOTHING else.
	//
	// The two really do meet: Close runs on the goroutine that stops the station while
	// Next runs on the catalog watch (§13.1 n° 5), and the shutdown can land in the
	// middle of a download. It is held across a pointer read or a pointer write, never
	// across a request.
	mu sync.Mutex
	// pending is the local copy of the remote file currently in flight, waiting for the
	// acknowledgement that will name it.
	pending *catalog.Pending
	// closed stops a source that is being shut down from opening yet another copy. The
	// watch loop polls before it looks at its context, so without this a Close in flight
	// leaves a half-written copy in the archive directory for ever.
	closed bool

	failures int
}

// New builds the source from what catalog.options declares.
func New(c catalog.SourceConfig) (*Source, error) {
	if c.Clock == nil {
		return nil, errors.New("webdav : une source de catalogue reçoit une horloge, jamais time.Now")
	}
	raw, _ := c.Catalog.Options.Text("url")
	folder, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || folder.Scheme != "http" && folder.Scheme != "https" || folder.Host == "" {
		return nil, fmt.Errorf("webdav : catalog.options.url vaut %q, "+
			"attendu une URL http ou https complète (par exemple https://dav.example.org:8001/)", raw)
	}
	if !strings.HasSuffix(folder.Path, "/") {
		folder.Path += "/"
	}

	fileName := catalog.FileName(c.StationNumber)
	file := *folder
	file.Path = folder.Path + fileName

	archive, err := catalog.NewArchive(pathOf(c.DataDir), c.Clock,
		option(c, "max_archives", defaultMaxArchives), option(c, "archive_days", defaultArchiveDays))
	if err != nil {
		return nil, fmt.Errorf("webdav : %w", err)
	}

	parse := csvodoo.OptionsFrom(c.Catalog)
	parse.Source = domain.CatalogSourceWebDAV
	parse.FileName = fileName
	parse.Images = c.Images

	username, _ := c.Catalog.Options.Text("username")
	password, _ := c.Catalog.Options.Text("password")
	return &Source{
		client:    newClient(folder.Scheme, folder.Host),
		file:      &file,
		folder:    folder,
		fileName:  fileName,
		username:  username,
		password:  password,
		interval:  time.Duration(option(c, "poll_interval_s", int(defaultPollInterval/time.Second))) * time.Second,
		stability: catalog.NewStability(option(c, "stable_polls", defaultStablePolls)),
		archive:   archive,
		quarantine: catalog.NewQuarantine(c.Quarantine,
			option(c, "failures_before_reject", catalog.DefaultFailuresBeforeReject)),
		parse: parse,
		clock: c.Clock,
		log:   logOf(c),
		wake:  make(chan struct{}, 1),
	}, nil
}

// pathOf is where the local copies of a remote catalog are kept.
//
// A remote source archives LOCALLY: the point of an archive is to still be readable
// when the share is unreachable, which is exactly the morning somebody needs it.
func pathOf(dataDir string) string {
	return filepath.Join(dataDir, "catalog", "archives")
}

// newClient builds the HTTP client, with the two timeouts and the redirect rule.
//
// A redirect off the declared host is REFUSED: a catalog that arrives from somewhere
// else than the address an operator typed is not the catalog they configured, and
// credentials must never follow a redirection to a host nobody vetted (§10.1).
//
// # A redirection may not drop TLS either, and the host check does not cover it
//
// The two arguments are the scheme and the host AS DECLARED, and the scheme is here
// because checking the host alone left a hole that looks closed. net/http keeps the
// Authorization header across a redirection to the SAME host — which is exactly what the
// check above lets through — so a share answering « 302 http://same-host/… » to an https
// request would put `username:password` on the wire in clear, on a network somebody
// believed was TLS-protected. And nothing in the symptom says so: the catalog arrives, the
// station serves it, the light is green.
//
// The declared scheme is a FLOOR and not a preference: http → https is a redirection worth
// following, https → http never is.
func newClient(scheme, host string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: connectTimeout}).DialContext,
			TLSHandshakeTimeout:   connectTimeout,
			ResponseHeaderTimeout: connectTimeout,
		},
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			if request.URL.Host != host {
				return fmt.Errorf("redirection vers %s, hors de l'hôte déclaré %s",
					request.URL.Host, host)
			}
			if scheme == "https" && request.URL.Scheme != "https" {
				return fmt.Errorf("redirection de https vers %s sur %s : le compte du "+
					"partage ne part pas en clair", request.URL.Scheme, request.URL.Host)
			}
			return nil
		},
	}
}

// option reads a whole-number option, or the value the specification ships.
func option(c catalog.SourceConfig, key string, fallback int) int {
	if value, ok := c.Catalog.Options.Int(key); ok && value > 0 {
		return int(value)
	}
	return fallback
}

// logOf returns the technical log, or one that discards.
func logOf(c catalog.SourceConfig) ports.TechnicalLog {
	if c.Log == nil {
		return ports.NopTechnicalLog{}
	}
	return c.Log
}

// Descriptor is what the administration screen builds its form from, and what
// Config.Validate checks catalog.options against (control 9).
//
// This is the ONE source that carries a secret, and that is the whole point of the
// separation: `local_drop` has neither user nor password because there is nothing to
// authenticate against a directory one owns (§10.1, control 41).
func Descriptor() catalog.Source {
	return catalog.Source{
		ID:    domain.CatalogSourceWebDAV,
		Label: Label,
		Options: []domain.OptionSchema{
			{Key: "url", Kind: domain.OptionURL, Required: true},
			{Key: "username", Kind: domain.OptionText},
			{Key: "password", Kind: domain.OptionText},
			{Key: "separator", Kind: domain.OptionText},
			{Key: "poll_interval_s", Kind: domain.OptionInt, Min: 1, Max: 3600},
			{Key: "stable_polls", Kind: domain.OptionInt, Min: 2, Max: 60},
			{Key: "max_file_size_mb", Kind: domain.OptionInt, Min: 1, Max: 512},
			{Key: "max_image_size_kb", Kind: domain.OptionInt, Min: 16, Max: 4096},
			{Key: "min_readable_ratio", Kind: domain.OptionRatio, Min: 0, Max: 1000},
			{Key: "max_weighable_drop", Kind: domain.OptionRatio, Min: 0, Max: 500},
			{Key: "max_archives", Kind: domain.OptionInt, Min: 1, Max: 1000},
			{Key: "archive_days", Kind: domain.OptionInt, Min: 1, Max: 3650},
			{Key: "failures_before_reject", Kind: domain.OptionInt, Min: 1, Max: 100},
		},
		New: func(c catalog.SourceConfig) (ports.CatalogSource, error) { return New(c) },
	}
}

// Compile-time proof that the source honours the contract the Hub consumes.
var _ ports.CatalogSource = (*Source)(nil)
