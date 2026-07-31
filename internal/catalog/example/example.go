package example

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"openscale/internal/catalog"
	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// ID is the value that would go into catalog.type — if this source were registered,
// which it deliberately is not (see doc.go).
const ID = "example_api"

// Label is the wording a volunteer would read in the drop-down list.
const Label = "API d'exemple — modèle à copier, aucun poste ne la propose"

// The keys this source reads from catalog.options, spelled as config.json would carry
// them. The keys the ASSEMBLER enforces are read by catalog.AssembleOptionsFrom and are
// not restated here (ADR-042).
const (
	// URLOption is the address of the ERP. It is the one option with no usable default:
	// a station cannot guess where its cooperative's server lives.
	URLOption = "url"
	// TokenOption is the bearer token. It is a SECRET, and it is the reason the drop
	// directory of `local_drop` may not carry one: a source that authenticates is a
	// different source (control 39, important-11).
	TokenOption = "token"
	// PollIntervalOption is how often the ERP is asked, in seconds.
	PollIntervalOption = "poll_interval_s"
	// PageSizeOption is how many products one answer carries.
	PageSizeOption = "page_size"
)

// The shipped values of §11.2, used when catalog.options does not carry the key.
//
// The polling interval is measured in MINUTES where the two file sources count in
// seconds, and that is not an oversight: watching a directory costs one stat, and asking
// an ERP for a whole catalog costs it a query. A station that polled a producer's server
// every five seconds would be a station its producer eventually blocks.
const (
	defaultPollInterval = 5 * time.Minute
	defaultPageSize     = 200
	requestBudget       = 30 * time.Second
)

// Source reads a whole catalog from an ERP over HTTP.
//
// It holds no goroutine of its own: Next blocks on the injected clock, which is what makes
// a whole polling scenario run in microseconds of wall time (§16.4).
type Source struct {
	endpoint *url.URL
	token    string
	interval time.Duration
	pageSize int
	client   *http.Client

	clock      ports.Clock
	log        ports.TechnicalLog
	quarantine *catalog.Quarantine
	assemble   catalog.AssembleOptions
	reader     readerOptions

	// wake carries ONE immediate poll, asked for from the screen (§14.4, « Recharger le
	// catalogue »). Capacity one and a non-blocking send: a button pressed five times
	// means one extra poll and not five.
	wake chan struct{}

	// mu guards applied and closed, and NOTHING else.
	mu sync.Mutex
	// applied is the fingerprint of the catalog this station last acknowledged, and it is
	// this source's whole acknowledgement.
	//
	// A file source acquits by DELETING, so the next poll finds nothing. This one has
	// nothing to delete, so it remembers instead: without it, every poll would download
	// the producer's whole catalog to conclude that it is the one already in service.
	// That conclusion is nominal and cheap downstream (ADR-015) and expensive on the wire,
	// which is exactly the cost an acknowledgement exists to avoid.
	applied string
	closed  bool
}

// New builds the source from what a configuration declares.
func New(c catalog.SourceConfig) (*Source, error) {
	if c.Clock == nil {
		return nil, errors.New("example : une source de catalogue reçoit une horloge, jamais time.Now")
	}
	raw, _ := c.Catalog.Options.Text(URLOption)
	endpoint, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return nil, fmt.Errorf("example : %s.%s doit être une adresse http(s) absolue, obtenu %q",
			"catalog.options", URLOption, raw)
	}
	token, _ := c.Catalog.Options.Text(TokenOption)

	assemble := catalog.AssembleOptionsFrom(c.Catalog)
	assemble.Source = ID
	assemble.Designation = endpoint.Host + endpoint.Path
	assemble.Images = c.Images

	return &Source{
		endpoint: endpoint,
		token:    strings.TrimSpace(token),
		interval: time.Duration(option(c, PollIntervalOption,
			int(defaultPollInterval/time.Second))) * time.Second,
		pageSize: option(c, PageSizeOption, defaultPageSize),
		// A client of its own, with a budget: an ERP that accepts the connection and never
		// answers must not hold the catalog watch for the lifetime of the process.
		client: &http.Client{Timeout: requestBudget},
		clock:  c.Clock,
		log:    logOf(c),
		quarantine: catalog.NewQuarantine(c.Quarantine,
			option(c, "failures_before_reject", catalog.DefaultFailuresBeforeReject)),
		assemble: assemble,
		reader: readerOptions{
			maxImageSize:     assemble.MaxImageSize,
			keepPhotos:       assemble.KeepPhotos,
			fallbackCategory: c.Catalog.FallbackCategory,
		},
		wake: make(chan struct{}, 1),
	}, nil
}

// option reads a whole-number option, or the value the specification ships.
func option(c catalog.SourceConfig, key string, fallback int) int {
	if value, ok := c.Catalog.Options.Int(key); ok && value > 0 {
		return int(value)
	}
	return fallback
}

// logOf returns the technical log, or one that discards, so no driver checks for nil.
func logOf(c catalog.SourceConfig) ports.TechnicalLog {
	if c.Log == nil {
		return ports.NopTechnicalLog{}
	}
	return c.Log
}

// Name reports the registry key of this source.
func (s *Source) Name() string { return ID }

// Describe reports the wording the administration screen shows permanently: the active
// source and what it interrogates (§10.1).
//
// The token is NOT in it, and no rendering of this source ever puts it there: what a
// screen displays travels into a screenshot a volunteer sends to whoever is helping them.
func (s *Source) Describe() string {
	return fmt.Sprintf("API %s://%s%s", s.endpoint.Scheme, s.endpoint.Host, s.endpoint.Path)
}

// Next blocks until a catalog this station has not applied is available, or until ctx is
// done.
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
		}
	}
}

// Wake asks for one immediate poll, from « Recharger le catalogue » (§14.4).
func (s *Source) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
		// A poll is already pending. Five presses are one extra poll, not five.
	}
}

// poll asks the ERP once. A nil batch with a nil error means « nothing new », which is
// the ordinary answer and not a failure.
func (s *Source) poll(ctx context.Context) (*ports.Batch, error) {
	if s.isClosed() {
		return nil, nil
	}
	batch, err := s.read(ctx)
	if err != nil {
		s.refuse(ctx, err)
		// A producer that is down, or an answer that does not parse, is a FEED problem and
		// never a reason to stop watching: the catalog N−1 stays in service and the next
		// tick tries again (§10.4).
		return nil, nil
	}
	if s.alreadyApplied(batch.ID) {
		return nil, nil
	}
	return batch, nil
}

// read fetches the catalog and assembles it.
//
// Everything a catalog DECIDES happens in the one call to catalog.Assemble: this function
// obtains bytes, counts them, and gives the result a name.
func (s *Source) read(ctx context.Context) (*ports.Batch, error) {
	counter := newCountingReader()
	reader := &rowReader{
		fetch:   func(number int) (io.ReadCloser, error) { return s.get(ctx, number, counter) },
		options: s.reader,
	}
	assemble := s.assemble
	assemble.Now = s.clock.Now()

	batch, err := catalog.Assemble(reader, assemble)
	if err != nil {
		// The quarantine of §10.5 counts refusals BY CONTENT, so a refusal has to name the
		// content it bears on. There is no file to hash, so what is hashed is the BYTES
		// THAT ARRIVED: an ERP that answers the same broken page three times is banned,
		// and one whose breakage varies never is. That is weaker than a file's digest and
		// it is the honest maximum here — a key that identifies nothing would count three
		// attempts against three unknowns and never reach the threshold.
		return nil, catalog.Refused(counter.digest(), counter.count, err)
	}

	// On SUCCESS the identity is computed over the PRODUCTS and never over the bytes: a
	// server free to reorder its keys, change its whitespace or add a field would publish
	// a new digest every night for an unchanged catalog, and « le même catalogue deux
	// fois » would stop being the nominal case it is (catalog.Fingerprint says why).
	batch.ID, batch.Bytes = catalog.Fingerprint(batch.Products), counter.count
	return batch, nil
}

// get asks for one page and hands back its body, already counted and hashed.
func (s *Source) get(ctx context.Context, number int, counter *countingReader) (io.ReadCloser, error) {
	target := *s.endpoint
	query := target.Query()
	query.Set("page", strconv.Itoa(number))
	query.Set("page_size", strconv.Itoa(s.pageSize))
	target.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("example : requête vers %s : %w", target.Redacted(), err)
	}
	if s.token != "" {
		request.Header.Set("Authorization", "Bearer "+s.token)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("example : %s ne répond pas : %w", s.endpoint.Host, err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		// The status is named and the body is not: an ERP that answers 500 with an HTML
		// error page would otherwise put that page in a French sentence a volunteer reads.
		return nil, fmt.Errorf("%w : %s répond %s à la page %d",
			catalog.ErrContent, s.endpoint.Host, response.Status, number)
	}
	return counter.wrap(response.Body), nil
}

// alreadyApplied reports the catalog this station acknowledged last.
func (s *Source) alreadyApplied(fingerprint string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fingerprint != "" && fingerprint == s.applied
}

// refuse counts a failure against its CONTENT and says so, once.
//
// There is nothing to archive and nothing to remove — the two acts a file source performs
// here — so what is left is the count and the sentence. The light only goes red once the
// SAME content has been refused often enough: a producer who fixes their export must not
// find a station that has already given up on it (§10.5).
func (s *Source) refuse(ctx context.Context, cause error) {
	entry, counted := s.quarantine.Count(ctx, cause)
	level := domain.LevelWarn
	if counted && entry.FailureCount >= s.quarantine.Threshold() {
		level = domain.LevelError
	}
	s.log.Technical(level, "catalog", "ERR-CAT-03",
		"Catalogue refusé : l'ERP n'a pas fourni un catalogue exploitable.", cause.Error())
}

// Acknowledge remembers the catalog that was applied, so the next poll does not download
// it again.
//
// It remembers an APPLIED or UNCHANGED batch and never a REFUSED one, and that asymmetry
// is the whole design. Remembering a refusal would make the station stop asking about a
// content it never put in service, so the quarantine would never see it three times and
// the red light of §10.5 would never come on — the producer would fix nothing, because
// nobody would have told them.
func (s *Source) Acknowledge(_ context.Context, batch *ports.Batch, result ports.BatchResult) error {
	if batch == nil || batch.ID == "" {
		return nil
	}
	switch result.Result {
	case domain.ImportApplied, domain.ImportUnchanged:
		s.mu.Lock()
		s.applied = batch.ID
		s.mu.Unlock()
	}
	return nil
}

// isClosed reports a source that has been shut down.
func (s *Source) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// Close stops watching the ERP.
//
// It closes no connection: every body opened by this source is closed by the reader that
// consumed it, on every exit, and a request in flight is cut by the context of the Next
// that carries it.
func (s *Source) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.client.CloseIdleConnections()
	return nil
}

// countingReader counts and hashes everything that arrives, across every page.
//
// One counter for the whole catalog and not one per page: the import record says how big
// the catalog was, and a figure that only covered the last page would be a figure nobody
// could act on.
type countingReader struct {
	count int64
	hash  hash.Hash
}

// newCountingReader opens the running count of one catalog.
func newCountingReader() *countingReader {
	return &countingReader{hash: sha256.New()}
}

// wrap returns a body that counts and hashes as it is read.
func (c *countingReader) wrap(body io.ReadCloser) io.ReadCloser {
	return &countedBody{body: body, counter: c}
}

// digest reports the digest of everything that arrived, and an EMPTY STRING when nothing
// did — the honest answer of a source whose producer never replied, and one the quarantine
// knows not to count.
func (c *countingReader) digest() string {
	if c.count == 0 {
		return ""
	}
	return hex.EncodeToString(c.hash.Sum(nil))
}

// countedBody forwards one page's bytes to the counter of the whole catalog.
type countedBody struct {
	body    io.ReadCloser
	counter *countingReader
}

// Read counts and hashes as it forwards.
func (b *countedBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if n > 0 {
		b.counter.count += int64(n)
		b.counter.hash.Write(p[:n])
	}
	return n, err
}

// Close releases the page.
func (b *countedBody) Close() error { return b.body.Close() }

// Descriptor is what a composition root would register — in ONE LINE, and that line is
// deliberately written nowhere (§5.2, ADR-050).
//
// The schema is what the administration screen generates its form from and what control 9
// validates catalog.options against. Two things in it are worth copying and not just
// reading:
//
//   - `url` is declared OptionURL, which is what puts this source in the list control 39
//     offers when somebody types a web address into a drop path. Nothing names it there;
//     the kind is the declaration.
//   - NO key is declared UseDropDirectory, so the drop probe of control 46 never runs
//     against this source. A source that watched a directory would declare it and get the
//     probe for free, without a line being added to internal/domain (ADR-052).
func Descriptor() catalog.Source {
	return catalog.Source{
		ID:    ID,
		Label: Label,
		Options: []domain.OptionSchema{
			{Key: URLOption, Kind: domain.OptionURL, Required: true},
			{Key: TokenOption, Kind: domain.OptionText},
			{Key: PollIntervalOption, Kind: domain.OptionInt, Min: 30, Max: 86400},
			{Key: PageSizeOption, Kind: domain.OptionInt, Min: 1, Max: 5000},
			{Key: "max_image_size_kb", Kind: domain.OptionInt, Min: 16, Max: 4096},
			{Key: "min_readable_ratio", Kind: domain.OptionRatio, Min: 0, Max: 1000},
			{Key: "max_weighable_drop", Kind: domain.OptionRatio, Min: 0, Max: 500},
			{Key: "failures_before_reject", Kind: domain.OptionInt, Min: 1, Max: 100},
		},
		New: func(c catalog.SourceConfig) (ports.CatalogSource, error) { return New(c) },
	}
}

// Compile-time proof that the source honours the contract the Hub consumes.
var _ ports.CatalogSource = (*Source)(nil)
