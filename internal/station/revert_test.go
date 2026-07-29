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
// internal/station knows no file, so what it does is CALL BACK with the document that file
// carried before the save. The composition root writes it, through the same atomic path as
// any other save. This caller hands no FileBefore, so the two documents are the same one —
// which is the ordinary station, and the reason the defect stayed invisible for so long.
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

	outcome, err := b.station.Reload(ReloadRequest{Next: next})
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
	if _, err := b.station.Reload(ReloadRequest{Next: next}); err != nil {
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

// TestLaCibleDuRetourArriereEstLeFichierEtPasCeQueLePosteFaitTourner.
//
// The countdown has TWO documents to put back and they are not the same one. The station
// goes back to what it was RUNNING — on a station whose file is faulty that is the neutral
// profile, and applying anything else would hand a live station a configuration §11.3 says
// never to run. The FILE goes back to what IT carried, which is the cooperative's own.
//
// Remembering only one of the two is what wrote the factory profile over a shop's file.
func TestLaCibleDuRetourArriereEstLeFichierEtPasCeQueLePosteFaitTourner(t *testing.T) {
	reverted := &revertRecorder{}
	forge := &scaleForge{}
	b := newBench(t, func(o *benchOptions) {
		o.newScale = forge.New
		o.onRevert = reverted.record
		// What a station whose configuration is unusable RUNS (§11.3).
		o.config = func(cfg *domain.Config) { *cfg = domain.NeutralProfile() }
	})
	forge.clock = b.clock

	running := b.hub.Config()
	file := loadConfig(t)
	if file.Station.Coop == running.Station.Coop {
		t.Fatal("le fichier et ce qui tourne portent la même coopérative : le banc ne " +
			"distingue plus les deux documents")
	}

	next := loadConfig(t)
	next.Scale.Type = "gram-xfoc-rs"
	outcome, err := b.station.Reload(ReloadRequest{Next: next, FileBefore: &file})
	if err != nil {
		t.Fatalf("Reload : %v", err)
	}
	if outcome.ConfirmBefore.IsZero() {
		t.Fatal("un changement de matériel n'arme aucun compte à rebours")
	}

	b.clock.Advance(confirmationWindow + time.Second)
	awaitCondition(t, func() bool { return reverted.count() == 1 },
		"le retour arrière n'a jamais été signalé à qui écrit le fichier")

	if got := reverted.last().Station.Coop; got != file.Station.Coop {
		t.Errorf("le fichier serait réécrit avec la coopérative %q, attendu celle qu'il "+
			"portait, %q", got, file.Station.Coop)
	}
	if got := b.hub.Config().Station.Coop; got != running.Station.Coop {
		t.Errorf("le poste fait tourner la coopérative %q, attendu celle qu'il faisait "+
			"tourner, %q : le fichier a été appliqué au poste vivant", got, running.Station.Coop)
	}
}

// TestSansFichierDAvantLeRetourArriereRepondCeQueLePosteFaisaitTourner.
//
// FileBefore is a POINTER and nil is a legitimate answer: it means « je n'ai pas pu lire le
// fichier », and what a caller in that state possesses is the configuration in service. The
// fallback has to stay, or a station whose file is unreadable would see its rollback write
// the zero configuration — a document nobody ever validated, and one that no station starts on.
func TestSansFichierDAvantLeRetourArriereRepondCeQueLePosteFaisaitTourner(t *testing.T) {
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
	if _, err := b.station.Reload(ReloadRequest{Next: next}); err != nil {
		t.Fatalf("Reload : %v", err)
	}

	b.clock.Advance(confirmationWindow + time.Second)
	awaitCondition(t, func() bool { return reverted.count() == 1 },
		"le retour arrière n'a jamais été signalé à qui écrit le fichier")

	rewritten := reverted.last()
	if got, want := rewritten.Fingerprint(), before.Fingerprint(); got != want {
		t.Fatalf("le fichier serait réécrit avec l'empreinte %s, attendu %s : un appelant qui "+
			"n'a pas pu lire le fichier ne possède que ce qui tourne", got, want)
	}
}

// revertRecorder keeps what the station handed back, so that « le fichier revient en
// arrière » is an assertion and not a hope.
type revertRecorder struct {
	mu      sync.Mutex
	configs []domain.Config
}

// record is the hook the station calls with the file as it was before the save.
func (r *revertRecorder) record(fileBefore domain.Config) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.configs = append(r.configs, fileBefore)
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
