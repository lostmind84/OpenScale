package catalog

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// ErrUnknownSource reports a catalog.type no source of this binary answers to.
var ErrUnknownSource = errors.New("catalog : ce catalog.type ne correspond à aucune source")

// DefaultSourceID is the value catalog.type carries when nobody has chosen.
//
// The local drop is the default because it is the one source that needs NOTHING: no
// account, no password, no drive letter, no UNC path. A directory the service creates
// and owns cannot be misconfigured, and the administration drag-and-drop writes into
// it rather than being a third source (§10.1, A4).
const DefaultSourceID = domain.CatalogSourceLocalDrop

// FileName is the name a station watches for, and it derives from station.number and
// from NOTHING else.
//
// There is deliberately no `pattern` setting: "flv_<n>.csv" is a constant of the
// exchange format, like the semicolon and the order of the seven columns. Two
// declarations of the same fact is the failure the legacy application died of — 227
// columns suffixed _Poste1 to _Poste4 (§11.2).
func FileName(stationNumber int) string {
	return fmt.Sprintf("flv_%d.csv", stationNumber)
}

// ImageSink is where the bytes of a decoded photo go.
//
// It is declared on the CONSUMER's side, like every other contract of this
// application: the parser knows how to recognise a JPEG from its header bytes and how
// to address it by its sha, and it knows nothing about directories. A nil sink is
// legitimate and means "count them, do not keep them" — which is exactly what a dry
// run of the import report wants (§10.3 bis).
type ImageSink interface {
	// Put stores one image under its sha256. It is called at most once per distinct
	// sha, and storing the same content twice must be a no-op: that is what makes a
	// re-import write no file at all (§10.7).
	Put(sha, format string, content []byte) error
}

// SourceConfig is everything a catalog source is handed, and nothing it could invent.
//
// A struct rather than six positional parameters, for the reason the printer registry
// gives: the set is already large and the day a source needs a seventh, every factory
// would have to be re-typed for a value it ignores.
type SourceConfig struct {
	// Catalog is the whole catalog block: the driver options, the image source, the
	// fallback category and the shelves of this station.
	Catalog domain.CatalogConfig
	// StationNumber is where the watched file name comes from, and its only real
	// consumer (§11.2).
	StationNumber int
	// DataDir is <data>: the local drop lives in <data>/catalog/incoming and its
	// archives in <data>/catalog/archives. A source that took an arbitrary path would
	// be the Z: drive of the legacy application under another name (§10.1).
	DataDir string
	// Clock stamps an archive name and drives the polling. No source reads time.Now.
	Clock ports.Clock
	// Log is where a source reports what an operator may have to act on. No driver
	// opens a journal file (ADR-013).
	Log ports.TechnicalLog
	// Images receives the decoded photos. Nil discards them.
	Images ImageSink
	// Quarantine counts the CONTENTS a source could not turn into a catalog (§10.5).
	//
	// A source carries it because a source is the only thing that READS: a file that
	// does not parse never becomes a batch, so nothing downstream will ever see it, and
	// « three refusals of the same content » would be uncountable anywhere else. Nil is
	// legitimate and means the refusal happens without being counted.
	Quarantine FailureLedger
}

// Factory builds one catalog source from what a configuration hands it.
type Factory func(c SourceConfig) (ports.CatalogSource, error)

// Source is one catalog source as the registry knows it.
type Source struct {
	// ID is the value that goes into catalog.type: "local_drop" or "webdav".
	ID string
	// Label is what a volunteer reads in the drop-down list, in French.
	Label string
	// Options is the schema the administration screen GENERATES its form from, and the
	// one Config.Validate checks catalog.options against (control 9).
	Options []domain.OptionSchema
	// New builds an instance of this source.
	New Factory
}

// Registry is the set of catalog sources this binary was built with.
//
// A value rather than a package-level map: registration happens once, in the
// composition root, so the only thing a global would buy is a state shared between
// tests that nobody can reset.
type Registry struct {
	sources []Source
}

// NewRegistry returns a registry with no source in it.
func NewRegistry() *Registry { return &Registry{} }

// Register adds one source. It is the ONE LINE of §5.2.
//
// It PANICS rather than returning an error, exactly like the printer registry: every
// refusal here is a COMPOSITION mistake with no operator input in it, so it is
// settled before the first weighing. The messages are English because nobody but a
// developer can ever read them.
func (r *Registry) Register(s Source) {
	switch {
	case s.ID == "":
		panic("catalog: a source registers under an ID, which is the value of catalog.type")
	case s.Label == "":
		panic("catalog: source " + s.ID +
			" registers without the label a volunteer reads in the drop-down list")
	case s.New == nil:
		panic("catalog: source " + s.ID + " registers without a factory")
	case s.ID == domain.CatalogSourceManual:
		// A4: the drag-and-drop writes into local_drop and the polling does the rest.
		// Registering it as a source would recreate the third code path the decision
		// removed, and Config.Validate refuses the value anyway (control 5).
		panic("catalog: `manual` is an OBSERVATION of provenance, never a source (A4, ADR-011)")
	}
	if _, exists := r.lookup(s.ID); exists {
		panic("catalog: source " + s.ID + " is registered twice")
	}
	r.sources = append(r.sources, s)
}

// Descriptors reports what the administration screen needs to build its drop-down
// list and the form behind each entry, in registration order — which is therefore the
// order a volunteer reads.
//
// The result is a COPY, option schemas included: it is handed to a form generator and
// to Config.Validate, and a registry a caller can reach into has stopped describing
// the binary.
func (r *Registry) Descriptors() []domain.DriverDescriptor {
	if len(r.sources) == 0 {
		return nil
	}
	out := make([]domain.DriverDescriptor, 0, len(r.sources))
	for _, s := range r.sources {
		out = append(out, domain.DriverDescriptor{
			ID:      s.ID,
			Label:   s.Label,
			Options: append([]domain.OptionSchema(nil), s.Options...),
		})
	}
	return out
}

// New builds the source catalog.type names.
//
// The error NAMES WHAT IS AVAILABLE: a configuration that spells a source wrong must
// produce the list of the ones that exist, never a bare « type inconnu » (§11.3).
func (r *Registry) New(id string, c SourceConfig) (ports.CatalogSource, error) {
	source, ok := r.lookup(id)
	if !ok {
		return nil, fmt.Errorf("%w : %q ; %s", ErrUnknownSource, id, r.availability())
	}
	return source.New(c)
}

// lookup finds a source by the value of catalog.type.
func (r *Registry) lookup(id string) (Source, bool) {
	for _, s := range r.sources {
		if s.ID == id {
			return s, true
		}
	}
	return Source{}, false
}

// availability is the French tail of the unknown-source error. An empty registry says
// so instead of offering an empty list.
func (r *Registry) availability() string {
	if len(r.sources) == 0 {
		return "aucune source de catalogue n'est disponible dans ce binaire"
	}
	ids := make([]string, 0, len(r.sources))
	for _, s := range r.sources {
		ids = append(ids, s.ID)
	}
	sort.Strings(ids)
	return "sources disponibles : " + strings.Join(ids, ", ")
}
