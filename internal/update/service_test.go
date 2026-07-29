package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openscale/internal/fake"
	"openscale/internal/platform"
)

// stubSource answers what a test decides, without a network.
type stubSource struct {
	release Release
	err     error
}

func (s *stubSource) Latest(context.Context) (Release, error) { return s.release, s.err }

// stubGuard answers what a test decides, without a station.
type stubGuard struct {
	allow  bool
	reason string
}

func (g stubGuard) UpdateGuard() (bool, string) { return g.allow, g.reason }

// serviceBench wires a Service over fakes and hands back what a test steers.
type serviceBench struct {
	service *Service
	source  *stubSource
	clock   *fake.Clock
	dir     string
	started []platform.UpdateSpec
}

// newServiceBench builds a station running 2.0.3 whose repository offers 2.1.0.
//
// Stage is stubbed: task 3 already proves it against a real archive, and this
// bench must not download anything.
func newServiceBench(t *testing.T, guard Guard) *serviceBench {
	t.Helper()
	running, err := ParseVersion("2.0.3")
	if err != nil {
		t.Fatalf("ParseVersion : %v", err)
	}
	offered, err := ParseVersion("2.1.0")
	if err != nil {
		t.Fatalf("ParseVersion : %v", err)
	}
	dir := t.TempDir()
	bench := &serviceBench{
		clock: fake.NewClock(benchEpoch),
		dir:   dir,
		source: &stubSource{release: Release{
			Tag: "2.1.0", Version: offered,
			PublishedAt: benchEpoch.Add(-24 * time.Hour),
			HTMLURL:     "https://github.com/lostmind84/OpenScale/releases/tag/2.1.0",
		}},
	}
	bench.service = &Service{
		Clock:     bench.clock,
		State:     State{Dir: dir},
		Guard:     guard,
		Running:   running,
		Supported: true,
		Paths:     Paths{InstallDir: dir, DataRoot: dir, UpdatesDir: dir},
		NewSource: func(string) Source { return bench.source },
		StageFunc: func(_ context.Context, r Release) (Staged, error) {
			root := filepath.Join(dir, r.Tag)
			if err := os.MkdirAll(root, 0o755); err != nil {
				return Staged{}, err
			}
			return Staged{
				Tag: r.Tag, Version: r.Version, Root: root,
				Binary: filepath.Join(root, "openscale.exe"),
				Script: filepath.Join(root, "update.ps1"),
			}, nil
		},
		Applier: func(spec platform.UpdateSpec) error {
			bench.started = append(bench.started, spec)
			return nil
		},
	}
	return bench
}

// TestApplyRefusesAVersionThatIsNoLongerTheOne is the property the screen depends
// on: the volunteer confirms what they READ, never what arrived since.
func TestApplyRefusesAVersionThatIsNoLongerTheOne(t *testing.T) {
	b := newServiceBench(t, stubGuard{allow: true})

	if err := b.service.Apply(context.Background(), "lostmind84/OpenScale", "2.0.9"); !errors.Is(err, ErrVersionMoved) {
		t.Fatalf("erreur = %v, attendu ErrVersionMoved", err)
	}
	if len(b.started) != 0 {
		t.Fatal("une bascule a été lancée sur une version que l'écran ne montrait pas")
	}
	if _, found, _ := b.service.State.ReadPending(); found {
		t.Error("un refus laisse un pending.json derrière lui")
	}
}

// TestApplyRefusesWhileTheStationIsBusy, and carries the guard's OWN sentence
// rather than one this layer invented: the guard knows why, this layer does not.
func TestApplyRefusesWhileTheStationIsBusy(t *testing.T) {
	const busy = "Une pesée est en cours. Réessayez dans un instant."
	b := newServiceBench(t, stubGuard{allow: false, reason: busy})

	err := b.service.Apply(context.Background(), "lostmind84/OpenScale", "2.1.0")
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("erreur = %v, attendu ErrBusy", err)
	}
	var busyErr *BusyError
	if !errors.As(err, &busyErr) {
		t.Fatalf("le refus ne porte pas de BusyError : %v", err)
	}
	if busyErr.Reason != busy {
		t.Errorf("raison = %q, attendu %q", busyErr.Reason, busy)
	}
	if len(b.started) != 0 {
		t.Fatal("une bascule a été lancée sur un poste occupé")
	}
}

// TestApplyRefusesASecondSwap: one at a time, and the screen says so.
func TestApplyRefusesASecondSwap(t *testing.T) {
	b := newServiceBench(t, stubGuard{allow: true})
	if err := b.service.State.WritePending(Pending{
		Tag: "2.1.0", StartedAt: b.clock.Now(),
	}); err != nil {
		t.Fatalf("WritePending : %v", err)
	}

	err := b.service.Apply(context.Background(), "lostmind84/OpenScale", "2.1.0")
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("erreur = %v, attendu ErrAlreadyRunning", err)
	}
}

// TestAPendingSwapThatWentStaleDoesNotWallTheStation is the failure mode the
// bench of 29/07/2026 uncovered.
//
// A script that never starts leaves Start() returning nil, so pending.json is
// written and no outcome.json will ever arrive. Without this, ErrAlreadyRunning
// would refuse every later update FOREVER, on the strength of a swap that did
// nothing and said nothing.
func TestAPendingSwapThatWentStaleDoesNotWallTheStation(t *testing.T) {
	b := newServiceBench(t, stubGuard{allow: true})
	abandoned := filepath.Join(b.dir, "2.0.9")
	if err := os.MkdirAll(abandoned, 0o755); err != nil {
		t.Fatalf("staging : %v", err)
	}
	if err := b.service.State.WritePending(Pending{
		Tag: "2.0.9", StartedAt: b.clock.Now().Add(-SwapBudget - time.Minute),
		StagingRoot: abandoned,
	}); err != nil {
		t.Fatalf("WritePending : %v", err)
	}

	if err := b.service.Apply(context.Background(), "lostmind84/OpenScale", "2.1.0"); err != nil {
		t.Fatalf("un poste muré par une bascule périmée refuse encore : %v", err)
	}
	if len(b.started) != 1 {
		t.Fatalf("%d bascule(s) lancée(s), attendu 1", len(b.started))
	}
	if _, err := os.Stat(abandoned); !os.IsNotExist(err) {
		t.Error("le staging de la bascule abandonnée reste sur le disque")
	}
}

// TestApplyRecordsThePendingSwapBeforeHandingOver is the ordering that makes the
// story survive the station's own death: pending.json must exist BEFORE the
// script that kills this process is started, or nothing will ever explain what
// the station was trying to do.
func TestApplyRecordsThePendingSwapBeforeHandingOver(t *testing.T) {
	b := newServiceBench(t, stubGuard{allow: true})
	var sawPending bool
	b.service.Applier = func(platform.UpdateSpec) error {
		_, found, err := b.service.State.ReadPending()
		if err != nil {
			t.Errorf("ReadPending : %v", err)
		}
		sawPending = found
		return nil
	}

	if err := b.service.Apply(context.Background(), "lostmind84/OpenScale", "2.1.0"); err != nil {
		t.Fatalf("Apply : %v", err)
	}
	if !sawPending {
		t.Fatal("le script est lancé avant que pending.json existe")
	}
}

// TestApplyHandsTheStagedScriptAndNotTheStationsOwn.
//
// No station carries an update.ps1: install.ps1 copies the binary and two
// documents, nothing else. The only one within reach is the one that just came
// down with the release, and every path handed over is absolute.
func TestApplyHandsTheStagedScriptAndNotTheStationsOwn(t *testing.T) {
	b := newServiceBench(t, stubGuard{allow: true})
	if err := b.service.Apply(context.Background(), "lostmind84/OpenScale", "2.1.0"); err != nil {
		t.Fatalf("Apply : %v", err)
	}
	spec := b.started[0]
	if filepath.Base(spec.Script) != "update.ps1" {
		t.Errorf("script = %q", spec.Script)
	}
	if !strings.Contains(spec.Script, "2.1.0") {
		t.Errorf("le script lancé ne vient pas du staging de la version : %q", spec.Script)
	}
	for name, path := range map[string]string{
		"Script": spec.Script, "Source": spec.Source,
		"InstallDir": spec.InstallDir, "DataRoot": spec.DataRoot,
		"OutcomePath": spec.OutcomePath, "LogPath": spec.LogPath,
	} {
		if path == "" {
			t.Errorf("%s est vide : le script retomberait sur son défaut, qui pointe Program Files", name)
		}
		if !filepath.IsAbs(path) {
			t.Errorf("%s = %q n'est pas absolu", name, path)
		}
	}
	if filepath.Base(spec.OutcomePath) != "outcome.json" {
		t.Errorf("OutcomePath = %q", spec.OutcomePath)
	}
}

// TestAFailedHandoverDoesNotLeaveTheStationWalled.
//
// If Start() itself refuses -- powershell.exe missing, for instance -- the swap
// never happened, and the pending.json written a line earlier must go with it.
func TestAFailedHandoverDoesNotLeaveTheStationWalled(t *testing.T) {
	b := newServiceBench(t, stubGuard{allow: true})
	b.service.Applier = func(platform.UpdateSpec) error {
		return errors.New("powershell.exe est introuvable")
	}

	if err := b.service.Apply(context.Background(), "lostmind84/OpenScale", "2.1.0"); err == nil {
		t.Fatal("un lancement refusé passe pour une réussite")
	}
	if _, found, _ := b.service.State.ReadPending(); found {
		t.Error("un lancement refusé laisse un pending.json qui murera le poste")
	}
}

// TestStatusReadsFromDiskAndNeverPolls: the page must draw instantly, and the
// poll has its own worker on the injected clock.
func TestStatusReadsFromDiskAndNeverPolls(t *testing.T) {
	b := newServiceBench(t, stubGuard{allow: true})
	b.source.err = errors.New("le réseau ne doit pas être touché ici")

	status, err := b.service.Status("lostmind84/OpenScale")
	if err != nil {
		t.Fatalf("Status : %v", err)
	}
	if status.Running != "2.0.3" {
		t.Errorf("version installée = %q", status.Running)
	}
	if status.HasCheck {
		t.Error("un poste qui n'a jamais sondé annonce un sondage")
	}
	if status.Available {
		t.Error("un poste qui n'a jamais sondé propose une version")
	}
	if !status.Supported {
		t.Error("la plateforme est déclarée non gérée")
	}
}

// TestCheckIsWrittenSoARestartDoesNotPollAgain.
func TestCheckIsWrittenSoARestartDoesNotPollAgain(t *testing.T) {
	b := newServiceBench(t, stubGuard{allow: true})

	if _, err := b.service.Check(context.Background(), "lostmind84/OpenScale"); err != nil {
		t.Fatalf("Check : %v", err)
	}
	check, found, err := b.service.State.ReadCheck()
	if err != nil || !found {
		t.Fatalf("ReadCheck : trouvé %v, %v", found, err)
	}
	if check.Tag != "2.1.0" || !check.CheckedAt.Equal(benchEpoch) {
		t.Errorf("sondage enregistré = %+v", check)
	}

	status, err := b.service.Status("lostmind84/OpenScale")
	if err != nil {
		t.Fatalf("Status : %v", err)
	}
	if !status.Available {
		t.Error("2.1.0 est publiée et 2.0.3 tourne : rien n'est proposé")
	}
}

// TestAPrereleaseIsNeverOffered: /releases/latest already excludes them, and this
// is the belt to those braces -- a fork could publish otherwise.
func TestAPrereleaseIsNeverOffered(t *testing.T) {
	candidate, err := ParseVersion("2.2.0-rc1")
	if err != nil {
		t.Fatalf("ParseVersion : %v", err)
	}
	b := newServiceBench(t, stubGuard{allow: true})
	b.source.release = Release{Tag: "2.2.0-rc1", Version: candidate}

	if _, err := b.service.Check(context.Background(), "lostmind84/OpenScale"); err != nil {
		t.Fatalf("Check : %v", err)
	}
	status, err := b.service.Status("lostmind84/OpenScale")
	if err != nil {
		t.Fatalf("Status : %v", err)
	}
	if status.Available {
		t.Fatal("une préversion est proposée au poste")
	}
	if err := b.service.Apply(context.Background(), "lostmind84/OpenScale", "2.2.0-rc1"); !errors.Is(err, ErrVersionMoved) {
		t.Fatalf("installer une préversion rend %v, attendu ErrVersionMoved", err)
	}
}

// TestAStationAlreadyOnTheLatestIsOfferedNothing.
func TestAStationAlreadyOnTheLatestIsOfferedNothing(t *testing.T) {
	same, err := ParseVersion("2.0.3")
	if err != nil {
		t.Fatalf("ParseVersion : %v", err)
	}
	b := newServiceBench(t, stubGuard{allow: true})
	b.source.release = Release{Tag: "2.0.3", Version: same}

	if _, err := b.service.Check(context.Background(), "lostmind84/OpenScale"); err != nil {
		t.Fatalf("Check : %v", err)
	}
	status, err := b.service.Status("lostmind84/OpenScale")
	if err != nil {
		t.Fatalf("Status : %v", err)
	}
	if status.Available {
		t.Fatal("un poste déjà à jour se voit proposer sa propre version")
	}
}

// TestAStalePendingIsReportedAsAFailureRatherThanAsSilence.
//
// The station must not simply forget: a volunteer who touched the button and saw
// nothing happen deserves a sentence, and « la mise à jour n'a jamais démarré »
// is one that tells them they may try again.
func TestAStalePendingIsReportedAsAFailureRatherThanAsSilence(t *testing.T) {
	b := newServiceBench(t, stubGuard{allow: true})
	if err := b.service.State.WritePending(Pending{
		Tag: "2.1.0", To: "2.1.0", From: "2.0.3",
		StartedAt: b.clock.Now().Add(-SwapBudget - time.Minute),
	}); err != nil {
		t.Fatalf("WritePending : %v", err)
	}

	status, err := b.service.Status("lostmind84/OpenScale")
	if err != nil {
		t.Fatalf("Status : %v", err)
	}
	if !status.HasOutcome {
		t.Fatal("une bascule périmée ne donne aucun compte rendu à l'écran")
	}
	if status.Outcome.Status != StatusNotStarted {
		t.Errorf("statut = %q, attendu %q", status.Outcome.Status, StatusNotStarted)
	}
	if status.Outcome.Reason == "" {
		t.Error("le compte rendu synthétique ne dit pas ce qui s'est passé")
	}
}

// TestTakeOutcomeAtStartupIsWhatTheStationTellsAfterwards.
func TestTakeOutcomeAtStartupIsWhatTheStationTellsAfterwards(t *testing.T) {
	b := newServiceBench(t, stubGuard{allow: true})
	writeOutcomeFile(t, b.dir, Outcome{
		Status: StatusRolledBack, ExitCode: 10, From: "2.0.3", To: "2.1.0",
		Reason: "le poste ne répond pas sur 127.0.0.1:8085/healthz après 60 s",
		FinishedAt: benchEpoch.Add(-time.Hour),
	})

	outcome, found, err := b.service.State.TakeOutcome()
	if err != nil || !found {
		t.Fatalf("TakeOutcome : trouvé %v, %v", found, err)
	}
	if outcome.Status != StatusRolledBack {
		t.Errorf("statut = %q", outcome.Status)
	}
	status, err := b.service.Status("lostmind84/OpenScale")
	if err != nil {
		t.Fatalf("Status : %v", err)
	}
	if !status.HasOutcome || status.Outcome.ExitCode != 10 {
		t.Errorf("compte rendu servi à l'écran = %+v", status.Outcome)
	}
}
