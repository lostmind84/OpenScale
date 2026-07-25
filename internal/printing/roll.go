package printing

import (
	"context"
	"fmt"
	"sync"

	"openscale/internal/domain"
)

const (
	// DefaultRollCapacity is how many labels a roll is assumed to hold when
	// printer.options.roll_capacity says nothing: 1000 (§8.5, §11.2).
	//
	// It is a DEFAULT and not a measurement. §21 n° 12 puts the real figure at « two
	// minutes » of work — read the supplier's label, or count one roll to its end — and
	// nothing is blocked until somebody does.
	DefaultRollCapacity = 1000

	// rollAlertPercent is where the amber light comes on: 90 % of the capacity (§8.5,
	// §15.4). On the shipped capacity of 1000 that is « environ 100 étiquettes
	// restantes », which is the sentence §15.4 quotes.
	//
	// Integer arithmetic, because this application keeps no float in a decision
	// (§6.1): the comparison is printed*100 >= capacity*90.
	rollAlertPercent = 90
)

// RollStore is the persistence a roll counter needs, and ALL of it.
//
// Declared on the consumer's side, like every other interface of this application
// (§5.2, cut 3): internal/printing must not import internal/store, and the store must
// not learn what a roll is. The composition root writes the six-line adapter over
// (DB).AddMeta / SetMeta / Meta on the key labels_since_roll — a key the glossary has
// already frozen, and AddMeta exists precisely so that the increment is one statement
// and cannot be lost between a read and a write.
type RollStore interface {
	// AddLabels adds n to the counter and reports the new total.
	AddLabels(ctx context.Context, n int64) (int64, error)
	// SetLabels forces the counter to n. This is the recalibration by hand.
	SetLabels(ctx context.Context, n int64) error
	// Labels reports the counter and whether it was ever written.
	Labels(ctx context.Context) (int64, bool, error)
}

// RollState is what the « rouleau » light of the dashboard and the troubleshooting
// screen show (§14.4, §15.4).
type RollState struct {
	// Printed is how many labels came out since the roll was last declared changed.
	Printed int64
	// Capacity is what a roll is assumed to hold.
	Capacity int
	// Remaining is Capacity - Printed and CAN BE NEGATIVE. A negative value is not an
	// error: it is a roll that held more than the configured capacity, or a roll that
	// was changed without anybody saying so — which is the ordinary case this counter
	// is built around.
	Remaining int64
	// Level is domain.LevelInfo or domain.LevelWarn, and NEVER domain.LevelError. A
	// roll that is about to run out is a maintenance job, not a breakdown: the station
	// keeps selling and somebody fetches a roll between two customers.
	Level string
	// Message is FRENCH, and it is the sentence a volunteer reads beside the light.
	Message string
	// Known reports whether the counter has ever been written. A freshly installed
	// station has no counter at all, and saying « environ 1000 étiquettes restantes »
	// about a roll nobody has described would be a number invented on the spot.
	Known bool
}

// RollCounter counts the labels printed since a volunteer last changed the roll (§8.5).
//
// # It is wrong, and it is BUILT to be wrong
//
// A roll is changed by hand, usually by whoever is closest, and nothing on a thermal
// printer reports it. No query can recover the truth. So this counter is designed
// around being recalibrated rather than around being right: Changed puts it back to
// zero (« J'ai changé le rouleau », §14.4), and SetPrinted takes any figure a volunteer
// can justify — a half-used roll, a bigger roll, a station whose database was restored.
//
// # It NEVER refuses a print
//
// There is no method on this type that a print path could read as a veto, and that is
// structural rather than a matter of discipline: State reports, Printed records, and
// neither returns anything a caller could mistake for permission. Printed does not even
// return an error — a persistence failure is logged and the count carries on in memory.
//
// A counter that refused to print because it BELIEVED the roll empty would be worse
// than no counter at all: it would stop a station that has paper, on the strength of a
// number nobody maintains, in front of a customer holding a bag. What the end of a roll
// costs today is one label; what a wrong veto costs is the till.
type RollCounter struct {
	store    RollStore
	capacity int
	log      TechnicalLog

	// mu guards the cached count. The count is cached because the dashboard reads it on
	// every refresh and the print worker bumps it on every label: a database that has
	// gone away must slow neither of the two.
	mu      sync.Mutex
	printed int64
	known   bool
}

// NewRollCounter builds the counter over the persistence and the capacity a
// configuration carries.
//
// A capacity that is not positive falls back on DefaultRollCapacity instead of being
// refused. Control 41 of Config.Validate has already turned back anything under 50
// labels by the time a station runs, so the case left here is a caller that passed
// nothing — and a station is not worth refusing to start over a figure whose only job
// is to colour a light. A nil log is replaced by one that discards, so that no caller
// has to check.
func NewRollCounter(store RollStore, capacity int, log TechnicalLog) *RollCounter {
	if capacity <= 0 {
		capacity = DefaultRollCapacity
	}
	if log == nil {
		log = nopLog{}
	}
	return &RollCounter{store: store, capacity: capacity, log: log}
}

// Load reads the persisted counter, and is the one place that may report a failure to
// the caller: it runs at start-up, where a broken database is worth saying out loud and
// nobody is waiting at the scale.
//
// A counter with no store is legitimate — a station whose database is not open yet, a
// test — and it simply stays at zero and unknown.
func (c *RollCounter) Load(ctx context.Context) error {
	if c.store == nil {
		return nil
	}
	printed, known, err := c.store.Labels(ctx)
	if err != nil {
		return fmt.Errorf("le compteur de rouleau n'a pas pu être lu : %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.printed, c.known = printed, known
	return nil
}

// Printed records that n labels came out, and RETURNS NOTHING.
//
// The signature is the design. This is called on the success path of a print, and any
// error it could return would sit one `if` away from failing a weighing that already
// succeeded — which is exactly the mistake important-9 removed elsewhere in §8.5. So a
// persistence failure is journalled and the count carries on in memory: the light stays
// approximately right until the database comes back, and the customer never learns that
// a counter had an opinion.
func (c *RollCounter) Printed(ctx context.Context, n int64) {
	if n <= 0 {
		return
	}
	c.mu.Lock()
	c.printed += n
	c.known = true
	c.mu.Unlock()

	if c.store == nil {
		return
	}
	stored, err := c.store.AddLabels(ctx, n)
	if err != nil {
		c.log.Technical(domain.LevelWarn, "printer", "",
			"Le compteur de rouleau n'a pas pu être enregistré ; l'impression, elle, a bien eu lieu.",
			err.Error())
		return
	}
	// The store is the reference when it answers: a station restarted mid-roll, or two
	// writers, and the in-memory tally would drift away from the row that survives.
	c.mu.Lock()
	c.printed = stored
	c.mu.Unlock()
}

// Changed puts the counter back to zero. It is the button « J'ai changé le rouleau » of
// the Dépannage page (§14.4), and the only gesture that tells this application anything
// true about the paper.
func (c *RollCounter) Changed(ctx context.Context) error { return c.SetPrinted(ctx, 0) }

// SetPrinted forces the counter to n — the recalibration by hand.
//
// It exists because Changed is not enough. A volunteer who puts back a half-used roll,
// or who counted what the last one really held, has a figure this application has no
// way of deriving; refusing it would leave them with a light they know to be wrong and
// no way to fix it, which is how a light stops being read.
//
// A negative count is refused, in French: there is no such thing as minus four labels,
// and accepting it would make the remaining figure lie in the other direction. A count
// ABOVE the capacity is accepted, because a bigger roll is a real thing and the
// capacity is a default nobody has measured (§21 n° 12).
func (c *RollCounter) SetPrinted(ctx context.Context, n int64) error {
	if n < 0 {
		return fmt.Errorf("compteur de rouleau à %d : le nombre d'étiquettes déjà imprimées "+
			"ne peut pas être négatif", n)
	}
	if c.store != nil {
		if err := c.store.SetLabels(ctx, n); err != nil {
			return fmt.Errorf("le compteur de rouleau n'a pas pu être enregistré : %w", err)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.printed, c.known = n, true
	return nil
}

// Capacity reports how many labels a roll is assumed to hold.
func (c *RollCounter) Capacity() int { return c.capacity }

// State reports what the light shows. It reads memory and touches no database, because
// it is called on every refresh of the dashboard and on every state broadcast.
func (c *RollCounter) State() RollState {
	c.mu.Lock()
	printed, known := c.printed, c.known
	c.mu.Unlock()

	state := RollState{
		Printed:   printed,
		Capacity:  c.capacity,
		Remaining: int64(c.capacity) - printed,
		Level:     domain.LevelInfo,
		Known:     known,
	}
	switch {
	case !known:
		state.Message = "rouleau non renseigné : le compteur démarrera à la première étiquette."
	case state.Remaining <= 0:
		state.Level = domain.LevelWarn
		state.Message = fmt.Sprintf("le rouleau est probablement fini : %d étiquettes imprimées "+
			"pour une capacité de %d. L'impression continue.", printed, c.capacity)
	case printed*100 >= int64(c.capacity)*rollAlertPercent:
		state.Level = domain.LevelWarn
		state.Message = fmt.Sprintf("rouleau à changer : environ %s restantes.",
			labelCount(state.Remaining))
	default:
		state.Message = fmt.Sprintf("environ %s restantes sur un rouleau de %d.",
			labelCount(state.Remaining), c.capacity)
	}
	return state
}

// labelCount spells a number of labels with the right plural. « environ 1 étiquettes
// restantes » is the kind of detail a volunteer reads as carelessness, and carelessness
// is what makes a light stop being trusted.
func labelCount(n int64) string {
	if n == 1 {
		return "1 étiquette"
	}
	return fmt.Sprintf("%d étiquettes", n)
}

// nopLog discards. It exists so that no caller of this package has to check whether its
// journal is nil, the same reason ports.NopTechnicalLog exists one floor up.
type nopLog struct{}

// Technical does nothing.
func (nopLog) Technical(level, source, code, message, detail string) {}
