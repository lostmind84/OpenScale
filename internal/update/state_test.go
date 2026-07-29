package update

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// benchEpoch is an arbitrary fixed instant. Nothing here reads a wall clock.
var benchEpoch = time.Date(2026, 7, 29, 10, 15, 0, 0, time.UTC)

// writeOutcomeFile writes what update.ps1 writes.
func writeOutcomeFile(t *testing.T, dir string, outcome Outcome) {
	t.Helper()
	raw, err := json.Marshal(outcome)
	if err != nil {
		t.Fatalf("encodage : %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("création du répertoire : %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "outcome.json"), raw, 0o644); err != nil {
		t.Fatalf("écriture : %v", err)
	}
}

// TestTakeOutcomeIsIdempotent is the property that keeps a station rebooted three
// times from journalling the same swap three times.
func TestTakeOutcomeIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	state := State{Dir: dir}

	staging := filepath.Join(dir, "2.1.0")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatalf("staging : %v", err)
	}
	if err := state.WritePending(Pending{
		Tag: "2.1.0", To: "2.1.0", From: "2.0.3",
		StartedAt: benchEpoch, StagingRoot: staging,
	}); err != nil {
		t.Fatalf("WritePending : %v", err)
	}
	writeOutcomeFile(t, dir, Outcome{
		Status: StatusSucceeded, ExitCode: 0, From: "2.0.3", To: "2.1.0",
		FinishedAt: benchEpoch.Add(time.Minute),
	})

	first, found, err := state.TakeOutcome()
	if err != nil || !found {
		t.Fatalf("TakeOutcome : %v, trouvé %v", err, found)
	}
	if first.Status != StatusSucceeded || first.To != "2.1.0" {
		t.Errorf("compte rendu lu = %+v", first)
	}

	if _, found, err = state.TakeOutcome(); err != nil || found {
		t.Fatalf("le même compte rendu est repris une seconde fois (trouvé %v, %v)", found, err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Error("le répertoire de staging survit à la lecture du compte rendu")
	}
	if _, found, _ := state.ReadPending(); found {
		t.Error("pending.json survit à la lecture du compte rendu")
	}

	last, found, err := state.LastOutcome()
	if err != nil || !found {
		t.Fatalf("LastOutcome : %v, trouvé %v", err, found)
	}
	if last.To != "2.1.0" {
		t.Errorf("le dernier compte rendu servi à l'écran = %+v", last)
	}
}

// TestTheStagingIsCleanedWhateverTheOutcome: a cancelled swap must not leave tens
// of megabytes on a station nobody watches.
func TestTheStagingIsCleanedWhateverTheOutcome(t *testing.T) {
	dir := t.TempDir()
	state := State{Dir: dir}
	staging := filepath.Join(dir, "2.1.0")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatalf("staging : %v", err)
	}
	if err := state.WritePending(Pending{
		Tag: "2.1.0", StartedAt: benchEpoch, StagingRoot: staging,
	}); err != nil {
		t.Fatalf("WritePending : %v", err)
	}
	writeOutcomeFile(t, dir, Outcome{
		Status: StatusRolledBack, ExitCode: 10, Reason: "le poste ne répond pas",
		FinishedAt: benchEpoch.Add(time.Minute),
	})

	if _, _, err := state.TakeOutcome(); err != nil {
		t.Fatalf("TakeOutcome : %v", err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Error("une bascule annulée laisse son staging derrière elle")
	}
}

// TestOnlyThreeOutcomesAreKept bounds a directory nothing else prunes.
func TestOnlyThreeOutcomesAreKept(t *testing.T) {
	dir := t.TempDir()
	state := State{Dir: dir}

	for i := range 5 {
		writeOutcomeFile(t, dir, Outcome{
			Status: StatusSucceeded, To: fmt.Sprintf("2.1.%d", i),
			FinishedAt: benchEpoch.Add(time.Duration(i) * time.Minute),
		})
		if _, _, err := state.TakeOutcome(); err != nil {
			t.Fatalf("TakeOutcome : %v", err)
		}
	}
	kept, err := filepath.Glob(filepath.Join(dir, "outcome-*.json"))
	if err != nil {
		t.Fatalf("glob : %v", err)
	}
	if len(kept) != keptOutcomes {
		t.Fatalf("%d comptes rendus gardés, attendu %d", len(kept), keptOutcomes)
	}
	// And the one the screen shows is the NEWEST, not whichever the filesystem
	// happened to hand back first.
	last, found, err := state.LastOutcome()
	if err != nil || !found {
		t.Fatalf("LastOutcome : %v, trouvé %v", err, found)
	}
	if last.To != "2.1.4" {
		t.Errorf("dernier compte rendu = %q, attendu 2.1.4", last.To)
	}
}

// TestNoOutcomeIsNotAnError: a station that has never been updated reads nothing
// and says so, rather than failing.
func TestNoOutcomeIsNotAnError(t *testing.T) {
	state := State{Dir: filepath.Join(t.TempDir(), "jamais-cree")}
	if _, found, err := state.TakeOutcome(); err != nil || found {
		t.Fatalf("TakeOutcome sur un poste neuf : trouvé %v, %v", found, err)
	}
	if _, found, err := state.LastOutcome(); err != nil || found {
		t.Fatalf("LastOutcome sur un poste neuf : trouvé %v, %v", found, err)
	}
	if _, found, err := state.ReadPending(); err != nil || found {
		t.Fatalf("ReadPending sur un poste neuf : trouvé %v, %v", found, err)
	}
	if _, found, err := state.ReadCheck(); err != nil || found {
		t.Fatalf("ReadCheck sur un poste neuf : trouvé %v, %v", found, err)
	}
}

// TestAPendingSwapThatNeverReportedGoesStale is the failure mode the bench of
// 29/07/2026 uncovered, and it walls a station shut.
//
// A script that never starts leaves Start() returning nil -- measured: with
// DETACHED_PROCESS, powershell.exe exits after 100 ms with code 0 without reading
// its file -- so pending.json is written and no outcome.json will EVER arrive.
// Without an age, ErrAlreadyRunning would refuse every later update, forever, on
// the strength of a swap that did nothing and said nothing.
func TestAPendingSwapThatNeverReportedGoesStale(t *testing.T) {
	pending := Pending{Tag: "2.1.0", StartedAt: benchEpoch}

	if pending.Stale(benchEpoch.Add(SwapBudget - time.Second)) {
		t.Error("une bascule dans son budget est déclarée périmée")
	}
	if !pending.Stale(benchEpoch.Add(SwapBudget + time.Second)) {
		t.Error("une bascule qui a dépassé son budget n'est jamais périmée")
	}
}

// TestAPendingSwapWithoutAnInstantIsStale: a file written by an older version, or
// one truncated by a power cut, carries the zero time. Reading that as « started
// just now » would wall the station for the same reason.
func TestAPendingSwapWithoutAnInstantIsStale(t *testing.T) {
	if !(Pending{Tag: "2.1.0"}).Stale(benchEpoch) {
		t.Fatal("une bascule sans instant de départ n'est pas déclarée périmée")
	}
}

// TestClearPendingLetsAWalledStationTryAgain.
func TestClearPendingLetsAWalledStationTryAgain(t *testing.T) {
	dir := t.TempDir()
	state := State{Dir: dir}
	staging := filepath.Join(dir, "2.1.0")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatalf("staging : %v", err)
	}
	if err := state.WritePending(Pending{
		Tag: "2.1.0", StartedAt: benchEpoch, StagingRoot: staging,
	}); err != nil {
		t.Fatalf("WritePending : %v", err)
	}

	if err := state.ClearPending(); err != nil {
		t.Fatalf("ClearPending : %v", err)
	}
	if _, found, _ := state.ReadPending(); found {
		t.Fatal("pending.json survit à ClearPending")
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Error("le staging d'une bascule abandonnée reste sur le disque")
	}
}

// TestAnUnreadableStateFileIsAnErrorAndNotAnAbsence: « the file is not there »
// and « the file is corrupt » are two different things, and treating the second
// as the first would silently restart a swap somebody is watching.
func TestAnUnreadableStateFileIsAnErrorAndNotAnAbsence(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pending.json"),
		[]byte("{ceci n'est pas du json"), 0o644); err != nil {
		t.Fatalf("écriture : %v", err)
	}
	if _, _, err := (State{Dir: dir}).ReadPending(); err == nil {
		t.Fatal("un pending.json illisible passe pour une absence")
	}
}

// TestTheCheckSurvivesARestart: without it a station that reboots every night
// would poll on every start and never on a schedule.
func TestTheCheckSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	if err := (State{Dir: dir}).WriteCheck(Check{
		CheckedAt: benchEpoch, Tag: "2.1.0", Version: "2.1.0",
		PublishedAt: benchEpoch.Add(-24 * time.Hour),
		HTMLURL:     "https://github.com/lostmind84/OpenScale/releases/tag/2.1.0",
	}); err != nil {
		t.Fatalf("WriteCheck : %v", err)
	}
	check, found, err := (State{Dir: dir}).ReadCheck()
	if err != nil || !found {
		t.Fatalf("ReadCheck : trouvé %v, %v", found, err)
	}
	if !check.CheckedAt.Equal(benchEpoch) || check.Version != "2.1.0" {
		t.Errorf("sondage relu = %+v", check)
	}
}
