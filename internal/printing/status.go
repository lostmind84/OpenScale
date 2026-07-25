package printing

import (
	"context"
	"fmt"
	"strings"

	"openscale/internal/station/ports"
)

// Level is HOW a printer state was learned: the three levels of §8.5.
//
// It travels beside the health because the two answer different questions, and the
// second is worth little without the first. « Prête » learned at N2 is a fact read on
// the print queue of the system; « prête » learned at N1 would be a guess drawn from a
// write that succeeded — and a write succeeds on a queue whose printer is unplugged, on
// a device node nobody is listening to, and ALWAYS on a file. That is why no N1
// observation is ever allowed to yield PrinterReady here, and why the level is shown on
// the administration screen beside the light.
//
// §8.5 orders these three « du certain à l'incertain » and the numbering runs the other
// way, from the poorest to the richest. Both orders are real and this file uses both,
// for two different jobs: RELIABILITY decides what a level is ALLOWED to conclude
// (verdictOf), RICHNESS decides which conclusion gets to word the report (Assess).
type Level uint8

const (
	// LevelNone is the level of a station that has observed NOTHING yet — the service
	// has just started, or it has just been pointed at another printer. It is a real
	// state, it is not « prête », and giving it a name is the whole point of this type.
	LevelNone Level = iota
	// LevelN1 is always available: the outcome of handing a frame to the transport
	// (§8.5). It separates reachable from unreachable and it knows NOTHING ELSE — not
	// the paper, not the cover, not the head, not whether a single dot was burnt.
	LevelN1
	// LevelN2 is the print queue of the system — OFFLINE, PAPER_OUT, ERROR, PAPER_JAM
	// and the number of pending jobs. §8.5 calls it « la source la plus riche des deux
	// OS » and rates it certain on Windows, partial on Linux.
	LevelN2
	// LevelN3 is the SBPL ENQ on a bidirectional transport. Any non-empty answer means
	// the printer is ALIVE; what the answer MEANS is known only once the fine decoding
	// is enabled, which §8.5 leaves OFF until a real frame has been captured on site.
	LevelN3
)

// String reports the level the way the journal, the database and the screen spell it.
// One spelling per value, and it is the one §8.5 uses.
func (l Level) String() string {
	switch l {
	case LevelN1:
		return "N1"
	case LevelN2:
		return "N2"
	case LevelN3:
		return "N3"
	case LevelNone:
		return "none"
	}
	return "unknown"
}

// Condition is what a printer is physically in, as reported by either of the two levels
// that can see it (§8.5).
//
// ONE vocabulary for N2 and N3 on purpose: the queue of the system and the printer
// itself observe THE SAME FOUR FACTS, and giving each its own words would put two
// tables on the administration screen for one roll of labels. The four names are the
// ones §8.5 lists for the Windows queue, because that is the only source this project
// has ever seen spell them.
type Condition struct {
	Offline  bool
	PaperOut bool
	Error    bool
	PaperJam bool
}

// OK reports that nothing at all is wrong.
func (c Condition) OK() bool { return c == Condition{} }

// Health reports what a station may conclude from these conditions.
//
// PaperOut is deliberately NOT a failure: the last label came out, and turning the end
// of a roll into an error sent a customer away with a valid label and a red screen
// telling them to fetch a volunteer — so they stuck two on, or weighed again, and the
// till counted twice (important-9, §8.5). Everything else stops the printing and is
// therefore a fault.
func (c Condition) Health() ports.PrinterHealth {
	switch {
	case c.Offline || c.PaperJam || c.Error:
		return ports.PrinterFaulted
	case c.PaperOut:
		return ports.PrinterConsumable
	}
	return ports.PrinterReady
}

// Detail spells the conditions in French, worst first, for the volunteer reading the
// troubleshooting screen. It reports EVERY condition that is up rather than the first:
// a printer that is offline AND out of paper needs both gestures.
func (c Condition) Detail() string {
	var said []string
	if c.Offline {
		said = append(said, "l'imprimante est hors ligne")
	}
	if c.PaperJam {
		said = append(said, "bourrage papier")
	}
	if c.Error {
		said = append(said, "l'imprimante signale une erreur")
	}
	if c.PaperOut {
		said = append(said, "plus d'étiquettes : le rouleau est à changer")
	}
	if len(said) == 0 {
		return "l'imprimante est prête"
	}
	return strings.Join(said, " ; ")
}

// WriteOutcome is what level N1 observes: what became of the last frame handed to the
// transport.
type WriteOutcome struct {
	// OK reports that every byte was accepted. It does NOT report that a label came out
	// — no transport guarantees that, which is why the customer screen says « Étiquette
	// envoyée à l'imprimante » and the reprint bar is permanent (important-7).
	OK bool
	// Detail is FRENCH: it is read by a volunteer on the troubleshooting screen.
	Detail string
}

// Queue is what level N2 reads on the print queue of the system (§8.5).
type Queue struct {
	Condition Condition
	// PendingJobs is the queue depth. Zero on a healthy station that has just printed;
	// a number that keeps growing is a printer that accepts jobs and prints none.
	PendingJobs int
}

// QueueProbe reads the print queue of the system — level N2.
//
// It is INJECTED and OPTIONAL, and that is not a convenience: ports.Transport has no
// method that exposes a queue, because a transport is a channel of bytes and a queue is
// an object of the operating system. The Windows implementation (EnumJobs, the printer
// status word) belongs to internal/platform (§5.1); Linux has no equivalent worth the
// name, which §8.5 itself concedes by rating that row « partielle ».
//
// A station with no probe simply never reaches N2, and Assess SAYS SO instead of
// filling the gap with an optimistic guess.
type QueueProbe interface {
	// Queue reports the state of the print queue this station prints through.
	Queue(ctx context.Context) (Queue, error)
}

// Native is what level N3 got back from an ENQ (§8.5).
type Native struct {
	// Raw is the status frame exactly as it came back, and it is shown in hex on the
	// administration screen. It is what will let someone complete the decoding without
	// travelling to the shop.
	Raw []byte
	// Detail is what the driver said about it, in French.
	Detail string
	// Condition is the READING of Raw. Nil is the SHIPPED state: §8.5 leaves the fine
	// decoding off until a real frame has been captured, so a station that has one
	// answer and no decoder knows the printer is ALIVE and nothing more.
	Condition *Condition
	// Failed reports that the probe itself failed — a transport error, which is an
	// observation. A SILENCE is not a failure and leaves this false with a nil
	// Condition and an empty Raw: the contrapositive of « toute réponse non vide =
	// imprimante vivante » is « on ne sait pas », never « morte ».
	Failed bool
}

// Observations is everything a station managed to see about its printer, and by which
// level it saw it.
//
// A nil field is a level that COULD NOT LOOK — no probe, a one-way transport, nothing
// printed yet. That is not the same thing as a level that looked and saw nothing, and
// keeping the two apart is the entire reason this type exists.
type Observations struct {
	Write  *WriteOutcome
	Queue  *Queue
	Native *Native
}

// StatusReport is what a station may say about its printer, and how it came to know it.
type StatusReport struct {
	// Level is the richest level that actually CONCLUDED something. A level that looked
	// and learned nothing does not raise it.
	Level  Level
	Health ports.PrinterHealth
	// Detail is FRENCH, complete, and names what was observed rather than the rule that
	// classified it.
	Detail string
	// Raw is the N3 status frame, when there is one.
	Raw []byte
	// PendingJobs comes from N2 alone: it is the one figure no other level can produce.
	PendingJobs int
}

// Ready reports whether the station may tell a volunteer the printer is ready.
//
// It exists so that no caller has to remember which health values count. A report that
// never rose above N1 can never answer true here, whatever the write did.
func (r StatusReport) Ready() bool { return r.Health == ports.PrinterReady }

// Status converts the report to the type the Hub consumes.
func (r StatusReport) Status() ports.PrinterStatus {
	return ports.PrinterStatus{
		Health:      r.Health,
		Detail:      r.Detail,
		Raw:         r.Raw,
		PendingJobs: r.PendingJobs,
	}
}

// verdict is one level's conclusion. A level that concluded nothing produces none.
type verdict struct {
	level  Level
	health ports.PrinterHealth
	detail string
}

// Assess concludes what the station has the right to say, from what each level of §8.5
// was able to observe.
//
// # The rule that matters
//
// A printer known ONLY at N1 is NEVER announced ready. A successful write proves that a
// transport accepted bytes; it does not prove there is paper, a head, or even a printer
// — the Windows queue accepts jobs while the device is unplugged, and the `file`
// transport accepts everything by construction. So N1 answers « joignable », which is
// PrinterUnknown plus a French sentence saying exactly that, and the one thing it is
// allowed to conclude on its own is a FAILURE: a write that came back short or errored
// is hard evidence, and evidence of failure is not symmetric with evidence of success.
//
// Same rule one floor up: an N3 answer with no decoder attached proves the printer is
// ALIVE and nothing else, so it cannot say ready either. PrinterReady means « answered
// and has nothing to report » (ports), and « we cannot read the report » is not that.
//
// # How the levels combine
//
// The WORST health wins, because no level may talk another level out of a fault it
// observed. Among the levels that reached that same health, the RICHEST one words the
// report: richness is precisely what makes a level able to say more, and reliability
// has already done its work above, by deciding what each level was allowed to conclude
// at all.
func Assess(o Observations) StatusReport {
	verdicts := make([]verdict, 0, 3)
	if v, ok := verdictOfWrite(o.Write); ok {
		verdicts = append(verdicts, v)
	}
	if v, ok := verdictOfQueue(o.Queue); ok {
		verdicts = append(verdicts, v)
	}
	if v, ok := verdictOfNative(o.Native); ok {
		verdicts = append(verdicts, v)
	}

	report := StatusReport{
		Level:  LevelNone,
		Health: ports.PrinterUnknown,
		Detail: "état inconnu : rien n'a encore été observé de cette imprimante.",
	}
	if o.Queue != nil {
		report.PendingJobs = o.Queue.PendingJobs
	}
	if o.Native != nil {
		report.Raw = o.Native.Raw
	}
	if len(verdicts) == 0 {
		return report
	}

	chosen := verdicts[0]
	for _, v := range verdicts[1:] {
		if severity(v.health) > severity(chosen.health) ||
			(v.health == chosen.health && v.level > chosen.level) {
			chosen = v
		}
	}
	report.Level, report.Health, report.Detail = chosen.level, chosen.health, chosen.detail
	return report
}

// verdictOfWrite is level N1: what the last frame handed to the transport became.
//
// A success concludes PrinterUnknown ON PURPOSE — see Assess. It still counts as a
// conclusion, because « joignable, et on ne sait rien de plus » is a strictly better
// answer to give a volunteer than « rien n'a encore été observé ».
func verdictOfWrite(w *WriteOutcome) (verdict, bool) {
	if w == nil {
		return verdict{}, false
	}
	if !w.OK {
		return verdict{LevelN1, ports.PrinterFaulted, join(
			"l'imprimante n'a pas accepté la dernière étiquette", w.Detail)}, true
	}
	return verdict{LevelN1, ports.PrinterUnknown, join(
		"état inconnu : la dernière étiquette est bien partie, mais rien ne dit qu'elle est sortie "+
			"(niveau N1 : le poste sait joindre l'imprimante, pas la regarder)", w.Detail)}, true
}

// verdictOfQueue is level N2: the print queue of the system, and the only level whose
// « rien à signaler » is worth a green light.
func verdictOfQueue(q *Queue) (verdict, bool) {
	if q == nil {
		return verdict{}, false
	}
	detail := q.Condition.Detail()
	if q.PendingJobs > 0 {
		detail = fmt.Sprintf("%s (%d travail(aux) en attente dans la file)", detail, q.PendingJobs)
	}
	return verdict{LevelN2, q.Condition.Health(), detail}, true
}

// verdictOfNative is level N3: the answer to an ENQ.
//
// Three outcomes and they are all different. A failed probe is a fault. A decoded frame
// concludes like a queue would. A frame with no decoder concludes « vivante », which is
// PrinterUnknown — richer than N1 and still not « prête ». A SILENCE concludes nothing
// at all and does not even raise the level: §8.5 says « toute réponse non vide =
// imprimante vivante », and the contrapositive of that is ignorance, not death.
func verdictOfNative(n *Native) (verdict, bool) {
	switch {
	case n == nil:
		return verdict{}, false
	case n.Failed:
		return verdict{LevelN3, ports.PrinterFaulted, join(
			"l'imprimante n'a pas répondu à l'interrogation", n.Detail)}, true
	case n.Condition != nil:
		return verdict{LevelN3, n.Condition.Health(), join(n.Condition.Detail(), n.Detail)}, true
	case len(n.Raw) > 0:
		return verdict{LevelN3, ports.PrinterUnknown, join(fmt.Sprintf(
			"l'imprimante a répondu %d octet(s) : elle est vivante. Le décodage détaillé de la "+
				"trame n'est pas activé, l'état exact reste donc inconnu", len(n.Raw)), n.Detail)}, true
	}
	return verdict{}, false
}

// severity ranks the health values by how much they must NOT be overridden by another
// level. A fault seen once is a fault; an all-clear is the weakest claim of the four,
// and ignorance sits below it because a level that knows nothing must never win against
// a level that knows something.
func severity(h ports.PrinterHealth) int {
	switch h {
	case ports.PrinterFaulted:
		return 3
	case ports.PrinterConsumable:
		return 2
	case ports.PrinterReady:
		return 1
	}
	return 0
}

// join appends a driver's own French wording to ours, when it has one to add.
func join(said, detail string) string {
	if detail == "" {
		return said + "."
	}
	return said + " : " + detail
}

// nativeFrom turns the conclusion a driver reached into the OBSERVATION it rests on.
//
// ports.Printer.Status returns a conclusion, because that is what the Hub consumes, but
// this file needs the evidence underneath it to combine it with the other two levels. A
// driver speaks ENQ and nothing else does, so its answer is exactly the N3 row of §8.5
// and the mapping is a reading of that row, not a translation loss:
//
//	Faulted     the probe failed          -> Failed
//	Consumable  it said media-empty       -> PaperOut
//	Ready       it answered, nothing up   -> an empty Condition
//	Unknown     alive but undecoded, or silent, or a one-way transport
func nativeFrom(s ports.PrinterStatus) *Native {
	n := &Native{Raw: s.Raw, Detail: s.Detail}
	switch s.Health {
	case ports.PrinterFaulted:
		n.Failed = true
	case ports.PrinterConsumable:
		n.Condition = &Condition{PaperOut: true}
	case ports.PrinterReady:
		n.Condition = &Condition{}
	}
	return n
}
