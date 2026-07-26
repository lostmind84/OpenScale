package station

import (
	"sync"
	"testing"
	"time"

	"openscale/internal/domain"
)

// TestARollbackPutsTheFileBackTooIsWhatMakesTheCountdownLastPastARestart.
//
// The countdown of §11.4 protects the station that is RUNNING, and the atomic write happens
// before it starts — steps 3 and 4 come before step 6. So a station that rolled back and was
// then restarted by the SCM or by a power cut would come back on the configuration nobody
// confirmed, sixty seconds after the branch was supposed to be safe.
//
// internal/station knows no file, so what it does is CALL BACK with the configuration it has
// just put in service. The composition root writes it, through the same atomic path as any
// other save.
func TestARollbackPutsTheFileBackTooIsWhatMakesTheCountdownLastPastARestart(t *testing.T) {
	reverted := &revertRecorder{}
	forge := &scaleForge{}
	b := newBench(t, func(o *benchOptions) {
		o.newScale = forge.New
		o.onRevert = reverted.record
	})
	forge.clock = b.clock

	before := b.hub.Config()
	next := b.hub.Config()
	next.Scale.Type = "gram-xfoc-rs"

	outcome, err := b.station.Reload(next)
	if err != nil {
		t.Fatalf("Reload : %v", err)
	}
	if outcome.ConfirmBefore.IsZero() {
		t.Fatal("un changement de matériel n'arme aucun compte à rebours")
	}
	if reverted.count() != 0 {
		t.Fatal("le retour arrière a été appelé alors que le compte à rebours vient de commencer")
	}

	// Nobody confirms, and the supervisor notices on its own tick.
	b.clock.Advance(confirmationWindow + time.Second)
	awaitCondition(t, func() bool { return reverted.count() == 1 },
		"le retour arrière n'a jamais été signalé à qui écrit le fichier")

	if got := reverted.last().Scale.Type; got != before.Scale.Type {
		t.Fatalf("le fichier serait réécrit avec scale.type = %q, attendu la version "+
			"précédente %q", got, before.Scale.Type)
	}
	if b.hub.Config().Scale.Type != before.Scale.Type {
		t.Fatal("le poste ne tourne pas sur la version précédente : le retour arrière " +
			"n'a pas eu lieu")
	}
}

// TestAConfirmedChangeNeverWritesTheFileBack keeps the hook from being a rollback that
// fires whatever happens.
//
// A confirmation is a human saying « la balance répond, garde ça » — and re-writing the
// previous configuration then would undo a change that works, which is worse than no
// countdown at all.
func TestAConfirmedChangeNeverWritesTheFileBack(t *testing.T) {
	reverted := &revertRecorder{}
	forge := &scaleForge{}
	b := newBench(t, func(o *benchOptions) {
		o.newScale = forge.New
		o.onRevert = reverted.record
	})
	forge.clock = b.clock

	next := b.hub.Config()
	next.Scale.Type = "gram-xfoc-rs"
	if _, err := b.station.Reload(next); err != nil {
		t.Fatalf("Reload : %v", err)
	}
	if err := b.station.Confirm(); err != nil {
		t.Fatalf("Confirm : %v", err)
	}

	b.clock.Advance(2 * confirmationWindow)
	b.tick()
	if reverted.count() != 0 {
		t.Fatalf("%d retour(s) arrière après une confirmation : le fichier serait réécrit "+
			"avec une configuration que quelqu'un a explicitement gardée", reverted.count())
	}
	if b.hub.Config().Scale.Type != "gram-xfoc-rs" {
		t.Fatal("la configuration confirmée n'est plus en service")
	}
}

// revertRecorder keeps what the station handed back, so that « le fichier revient en
// arrière » is an assertion and not a hope.
type revertRecorder struct {
	mu      sync.Mutex
	configs []domain.Config
}

// record is the hook the station calls.
func (r *revertRecorder) record(previous domain.Config) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.configs = append(r.configs, previous)
}

// count reports how many rollbacks were signalled.
func (r *revertRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.configs)
}

// last reports the configuration of the last rollback.
func (r *revertRecorder) last() domain.Config {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.configs) == 0 {
		return domain.Config{}
	}
	return r.configs[len(r.configs)-1]
}
