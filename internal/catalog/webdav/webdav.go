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
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"openscale/internal/catalog"
	"openscale/internal/catalog/csvodoo"
	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

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
		client:    newClient(folder.Host),
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
func newClient(host string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: connectTimeout}).DialContext,
			TLSHandshakeTimeout:   connectTimeout,
			ResponseHeaderTimeout: connectTimeout,
		},
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if request.URL.Host != host {
				return fmt.Errorf("redirection vers %s, hors de l'hôte déclaré %s",
					request.URL.Host, host)
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

// Name reports the registry key of this source.
func (s *Source) Name() string { return domain.CatalogSourceWebDAV }

// Describe reports what the dashboard shows permanently: the source, the URL watched
// and the account used, which is the difference between this source and the local one
// (§10.1).
func (s *Source) Describe() string {
	if s.username == "" {
		return fmt.Sprintf("WebDAV, %s (sans compte)", s.file)
	}
	return fmt.Sprintf("WebDAV, %s (compte %s)", s.file, s.username)
}

// Next blocks until a whole catalog is available, or until ctx is done.
func (s *Source) Next(ctx context.Context) (*ports.Batch, error) {
	tick, stop := s.clock.Ticker(s.interval)
	defer stop()
	for {
		batch, err := s.poll(ctx)
		if err != nil {
			return nil, err
		}
		if batch != nil {
			return batch, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-tick:
		case <-s.wake:
			// « Recharger le catalogue » was pressed. The poll below is the SAME one the
			// tick performs, share credentials and stability rule included.
		}
	}
}

// Wake asks the watch to poll NOW rather than at the next tick (§14.4).
func (s *Source) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
		// A poll is already asked for. Two are the same request.
	}
}

// poll asks the share what it holds, and reads only a file that has stopped moving.
//
// A share that does not answer is NOT an error of this function: returning one would
// send the watcher round the loop with no delay at all. It is logged, counted, and
// the next poll tries again — which is what a station with a flaky network needs.
func (s *Source) poll(ctx context.Context) (*ports.Batch, error) {
	if s.isClosed() {
		return nil, nil
	}
	stamp, found, err := s.propfind(ctx)
	switch {
	case err != nil:
		s.unreachable(err)
		return nil, nil
	case !found:
		s.failures = 0
		s.stability.Forget()
		return nil, nil
	}
	s.failures = 0
	if !s.stability.Observe(stamp) {
		return nil, nil
	}
	return s.get(ctx)
}

// unreachable reports a share that did not answer, and raises the level on the third
// consecutive failure.
func (s *Source) unreachable(err error) {
	s.failures++
	s.stability.Forget()
	level := domain.LevelWarn
	if s.failures >= attemptsBeforeAlarm {
		level = domain.LevelError
	}
	s.log.Technical(level, "catalog", "ERR-CAT-03",
		fmt.Sprintf("Partage de catalogue injoignable (%d essai(s) consécutif(s)).", s.failures),
		err.Error())
}

// propfind asks for the size and the date of the watched file.
//
// Depth 1 on the FOLDER rather than Depth 0 on the file, because that is the request
// a WebDAV server always answers the same way, and because a 404 on a folder listing
// tells an operator something a 404 on a file does not: the path is wrong.
func (s *Source) propfind(ctx context.Context) (catalog.Stamp, bool, error) {
	const body = `<?xml version="1.0" encoding="utf-8"?>` +
		`<D:propfind xmlns:D="DAV:"><D:prop>` +
		`<D:getcontentlength/><D:getlastmodified/>` +
		`</D:prop></D:propfind>`

	response, err := s.do(ctx, "PROPFIND", s.folder, strings.NewReader(body), func(r *http.Request) {
		r.Header.Set("Depth", "1")
		r.Header.Set("Content-Type", "application/xml; charset=utf-8")
	})
	if err != nil {
		return catalog.Stamp{}, false, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusMultiStatus && response.StatusCode != http.StatusOK {
		return catalog.Stamp{}, false, fmt.Errorf("PROPFIND %s : %s", s.folder, response.Status)
	}

	var listing multistatus
	if err := xml.NewDecoder(io.LimitReader(response.Body, maxListingBytes)).Decode(&listing); err != nil {
		return catalog.Stamp{}, false, fmt.Errorf("réponse PROPFIND illisible : %w", err)
	}
	return listing.find(s.fileName)
}

// get downloads the file and parses it as it arrives.
func (s *Source) get(ctx context.Context) (*ports.Batch, error) {
	// A copy still in flight means the previous batch was never acknowledged — a file
	// the share would not let us DELETE, downloaded again five seconds later. It is
	// thrown away rather than left behind: keeping it would hold an open handle per
	// download, and half a file in the archive directory is worse than no file at all.
	s.take().Discard()

	response, err := s.do(ctx, http.MethodGet, s.file, nil, func(r *http.Request) {
		// identity: a compressed body would make the byte count of the import record
		// and the ceiling of §10.1 measure two different things.
		r.Header.Set("Accept-Encoding", "identity")
	})
	if err != nil {
		s.unreachable(err)
		return nil, nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		s.unreachable(fmt.Errorf("GET %s : %s", s.file, response.Status))
		return nil, nil
	}

	pending, err := s.archive.Begin(s.fileName)
	if err != nil {
		s.log.Technical(domain.LevelWarn, "catalog", "ERR-CAT-05",
			"Archive du catalogue impossible.", err.Error())
	}
	options := s.parse
	options.Now = s.clock.Now()
	batch, err := csvodoo.Parse(io.TeeReader(response.Body, pending), options)
	if err != nil {
		s.refuse(ctx, pending, err)
		return nil, err
	}
	s.keep(pending)
	return batch, nil
}

// keep stores the copy in flight, or throws it away when the source was closed while
// the body was being parsed — the shutdown landing in the middle of a download.
func (s *Source) keep(pending *catalog.Pending) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		pending.Discard()
		return
	}
	s.pending = pending
	s.mu.Unlock()
}

// take removes the copy in flight and hands it over, so that exactly one caller ever
// commits or discards it.
func (s *Source) take() *catalog.Pending {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.pending
	s.pending = nil
	return pending
}

// isClosed reports a source that has been shut down.
func (s *Source) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// refuse sets aside a file nothing could be made of, counts the failure against its
// CONTENT, and deletes it from the share.
//
// Deleting it is what stops the watcher re-reading the same broken content every five
// seconds for ever. The copy and its reason stay locally (failure test 9), and the
// count is what turns the third refusal of the same content into a red light (§10.5).
func (s *Source) refuse(ctx context.Context, pending *catalog.Pending, cause error) {
	s.stability.Forget()
	archived, err := pending.Commit()
	if err != nil {
		s.log.Technical(domain.LevelWarn, "catalog", "ERR-CAT-05",
			"Archive du catalogue refusé impossible.", err.Error())
	}
	if err := s.archive.Explain(archived, "ERR-CAT-03", cause.Error()); err != nil {
		s.log.Technical(domain.LevelWarn, "catalog", "ERR-CAT-05",
			"Motif du refus non écrit.", err.Error())
	}
	entry, counted := s.quarantine.Count(ctx, cause)
	if err := s.delete(ctx); err != nil {
		s.log.Technical(domain.LevelWarn, "catalog", "ERR-CAT-05",
			"Fichier de catalogue refusé non supprimé.", err.Error())
		return
	}
	level := domain.LevelWarn
	if counted && entry.FailureCount >= s.quarantine.Threshold() {
		level = domain.LevelError
	}
	s.log.Technical(level, "catalog", "ERR-CAT-03",
		"Catalogue refusé, fichier mis de côté.", archived)
}

// Acknowledge names the local copy and THEN deletes the remote file.
//
// The DELETE is the acknowledgement, exactly as the os.Remove is for the local drop
// (ADR-004).
func (s *Source) Acknowledge(ctx context.Context, batch *ports.Batch, result ports.BatchResult) error {
	pending := s.take()
	s.stability.Forget()

	archived, err := pending.Commit()
	if err != nil {
		s.log.Technical(domain.LevelWarn, "catalog", "ERR-CAT-05",
			"Archive du catalogue impossible.", err.Error())
	}
	if result.Result == domain.ImportRejected || result.Result == domain.ImportFailed {
		if err := s.archive.Explain(archived, result.Code, result.Reason); err != nil {
			s.log.Technical(domain.LevelWarn, "catalog", "ERR-CAT-05",
				"Motif du refus non écrit.", err.Error())
		}
	}
	if err := s.delete(ctx); err != nil {
		return fmt.Errorf("%w : le compte %s n'a pas pu supprimer %s (lot %s) : %w",
			catalog.ErrNotAcknowledged, s.account(), s.file, batch.ID, err)
	}
	s.log.Technical(domain.LevelInfo, "catalog", "",
		"Catalogue acquitté, fichier supprimé du partage.", archived)
	return nil
}

// account is the wording of the user a message names, or an honest absence.
func (s *Source) account() string {
	if s.username == "" {
		return "anonyme"
	}
	return s.username
}

// delete removes the file from the share. A file already gone is a success: the
// acknowledgement has taken place, whoever performed it.
func (s *Source) delete(ctx context.Context) error {
	response, err := s.do(ctx, http.MethodDelete, s.file, nil, nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxListingBytes))
	switch response.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusAccepted, http.StatusNotFound:
		return nil
	}
	return fmt.Errorf("DELETE %s : %s", s.file, response.Status)
}

// do issues one request, bounded by a budget measured on the INJECTED clock.
//
// That is what makes a test of a hanging share instantaneous instead of two minutes:
// http.Client.Timeout would read the wall clock (§16.4).
func (s *Source) do(ctx context.Context, method string, target *url.URL, body io.Reader,
	decorate func(*http.Request)) (*http.Response, error) {
	ctx, cancel := ports.WithBudget(ctx, s.clock, bodyBudget)
	request, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		cancel()
		return nil, err
	}
	if s.username != "" {
		request.SetBasicAuth(s.username, s.password)
	}
	if decorate != nil {
		decorate(request)
	}
	response, err := s.client.Do(request)
	if err != nil {
		cancel()
		return nil, err
	}
	// The budget covers the BODY as well, so it is released when the body is closed
	// and not when the headers arrive.
	response.Body = &closingBody{ReadCloser: response.Body, release: cancel}
	return response, nil
}

// closingBody releases the budget of a request when its body is closed.
type closingBody struct {
	io.ReadCloser
	release context.CancelFunc
}

// Close closes the body and releases the budget, once.
func (b *closingBody) Close() error {
	err := b.ReadCloser.Close()
	if b.release != nil {
		b.release()
		b.release = nil
	}
	return err
}

// Close stops watching and throws away a copy in flight.
func (s *Source) Close() error {
	s.mu.Lock()
	s.closed = true
	pending := s.pending
	s.pending = nil
	s.mu.Unlock()

	pending.Discard()
	s.client.CloseIdleConnections()
	return nil
}

// maxListingBytes bounds a PROPFIND answer. A directory listing that does not fit in
// a megabyte is not a directory listing.
const maxListingBytes = 1 << 20

// multistatus is the answer of a PROPFIND, reduced to the two properties asked for.
type multistatus struct {
	XMLName   xml.Name      `xml:"DAV: multistatus"`
	Responses []davResponse `xml:"DAV: response"`
}

// davResponse is one entry of the listing.
type davResponse struct {
	Href     string      `xml:"DAV: href"`
	Propstat []davStatus `xml:"DAV: propstat"`
}

// davStatus is one property block of one entry.
type davStatus struct {
	Status        string `xml:"DAV: status"`
	ContentLength string `xml:"DAV: prop>getcontentlength"`
	LastModified  string `xml:"DAV: prop>getlastmodified"`
}

// find reports the size and the date of one file of the listing.
//
// The comparison is on the LAST SEGMENT of the href and never on the whole path: a
// server is free to answer with an absolute path, a relative one or an escaped one,
// and the file name is the only part all three agree on.
func (m multistatus) find(fileName string) (catalog.Stamp, bool, error) {
	for _, entry := range m.Responses {
		href := entry.Href
		if unescaped, err := url.PathUnescape(href); err == nil {
			href = unescaped
		}
		if path.Base(strings.TrimSuffix(href, "/")) != fileName {
			continue
		}
		for _, property := range entry.Propstat {
			if property.ContentLength == "" {
				continue
			}
			size, err := strconv.ParseInt(strings.TrimSpace(property.ContentLength), 10, 64)
			if err != nil {
				return catalog.Stamp{}, false, fmt.Errorf(
					"taille annoncée %q pour %s", property.ContentLength, fileName)
			}
			modified, err := http.ParseTime(strings.TrimSpace(property.LastModified))
			if err != nil {
				// A share that does not date its files is not a reason to refuse the
				// catalog: the size alone still makes the stability rule work, it
				// just makes it slightly weaker.
				modified = time.Time{}
			}
			return catalog.Stamp{Size: size, Modified: modified}, true, nil
		}
	}
	return catalog.Stamp{}, false, nil
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
