package web

import (
	"context"
	"net/http"
	"testing"

	"openscale/internal/update"
)

// stubUpdater answers what a test decides, without a network and without a disk.
type stubUpdater struct {
	status   update.Status
	applyErr error
	checkErr error
	applied  string
	checks   int
}

func (u *stubUpdater) Status(repository string) (update.Status, error) {
	status := u.status
	status.Repository = repository
	return status, nil
}

func (u *stubUpdater) Check(context.Context, string) (update.Check, error) {
	u.checks++
	return update.Check{}, u.checkErr
}

func (u *stubUpdater) Apply(_ context.Context, _, wanted string) error {
	u.applied = wanted
	return u.applyErr
}

// nominalUpdate is a station on 2.0.3 whose repository offers 2.1.0.
func nominalUpdate() update.Status {
	return update.Status{
		Running: "2.0.3", Supported: true, Available: true, HasCheck: true,
		Check: update.Check{
			Tag: "2.1.0", Version: "2.1.0",
			CheckedAt:   epoch,
			PublishedAt: epoch,
			HTMLURL:     "https://github.com/lostmind84/OpenScale/releases/tag/2.1.0",
		},
	}
}

// TestTheUpdatePageReadsWithoutAPassword: the settings pages open for reading and
// ask at the write (ADR-033), and this page is no different.
func TestTheUpdatePageReadsWithoutAPassword(t *testing.T) {
	b := newBench(t, func(o *benchOptions) {
		o.update = &stubUpdater{status: nominalUpdate()}
	})

	got := decodeStatus[updateDTO](t, b.get("/admin/api/update"), http.StatusOK)
	if got.Running != "2.0.3" || got.Latest != "2.1.0" || !got.Available {
		t.Fatalf("état servi = %+v", got)
	}
	if got.Repository == "" {
		t.Error("le dépôt suivi n'est pas servi : c'est ce que la page affiche")
	}
	if got.Outcome != nil {
		t.Error("un poste qui n'a jamais basculé porte un compte rendu")
	}
}

// TestTheTwoActsAreProtected. Every POST of this screen is, and reading a port
// number is not what the password guards -- writing is.
func TestTheTwoActsAreProtected(t *testing.T) {
	b := newBench(t, func(o *benchOptions) {
		o.update = &stubUpdater{status: nominalUpdate()}
	})
	// A password is POSED but no session is opened: without it the guard answers
	// 409 « ce poste n'a pas encore de mot de passe », which is a different
	// refusal and would let this test pass without the routes being guarded.
	b.setPassword("mot-de-passe-long", "ABCD2345")

	for _, route := range []struct {
		path, body string
	}{
		{"/admin/api/update/check", `{}`},
		{"/admin/api/update/apply", `{"version":"2.1.0"}`},
	} {
		response := b.post(route.path, route.body)
		defer response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s sans session = %d, attendu 401", route.path, response.StatusCode)
		}
	}
}

// TestApplyingCarriesTheVersionTheScreenShowed.
func TestApplyingCarriesTheVersionTheScreenShowed(t *testing.T) {
	updater := &stubUpdater{status: nominalUpdate()}
	b := adminBench(t, func(o *benchOptions) { o.update = updater })

	response := b.post("/admin/api/update/apply", `{"version":"2.1.0"}`)
	defer response.Body.Close()
	// 202 and not 200: the swap has started and the station that accepted it is
	// about to be stopped by it. There is no second answer on this connection.
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("statut %d, attendu 202 : %s", response.StatusCode, body(t, response))
	}
	if updater.applied != "2.1.0" {
		t.Errorf("version installée = %q", updater.applied)
	}
}

// TestEverySentinelBecomesItsOwnCodeAndFrenchSentence freezes the whole mapping.
//
// A code nobody can look up is worse than none, and two refusals that ask
// different things of a volunteer must not share one: « attendez un instant » and
// « rechargez la page » are not the same instruction.
func TestEverySentinelBecomesItsOwnCodeAndFrenchSentence(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"réseau", update.ErrUnreachable, http.StatusBadGateway, "ERR-UPD-01"},
		{"empreinte", update.ErrChecksumMismatch, http.StatusBadGateway, "ERR-UPD-02"},
		{"occupé", &update.BusyError{Reason: "Une pesée est en cours."},
			http.StatusConflict, "ERR-UPD-03"},
		{"déjà en vol", update.ErrAlreadyRunning, http.StatusConflict, "ERR-UPD-04"},
		{"archive absente", update.ErrAssetMissing, http.StatusBadGateway, "ERR-UPD-08"},
		{"version périmée", update.ErrVersionMoved, http.StatusConflict, "ERR-UPD-09"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := adminBench(t, func(o *benchOptions) {
				o.update = &stubUpdater{status: nominalUpdate(), applyErr: c.err}
			})
			got := decodeStatus[problem](t,
				b.post("/admin/api/update/apply", `{"version":"2.1.0"}`), c.status)
			if got.Code != c.code {
				t.Errorf("code %q, attendu %q", got.Code, c.code)
			}
			if got.Message == "" {
				t.Error("refus sans phrase française")
			}
		})
	}
}

// TestTheBusyRefusalCarriesTheGuardsOwnSentence.
//
// The Hub knows whether a weighing is in progress or a catalogue is waiting to
// enter service. This layer does not, and paraphrasing would lose the only
// information the volunteer can act on.
func TestTheBusyRefusalCarriesTheGuardsOwnSentence(t *testing.T) {
	const reason = "Un catalogue vient d'arriver et n'est pas encore en service. Réessayez dans un instant."
	b := adminBench(t, func(o *benchOptions) {
		o.update = &stubUpdater{
			status: nominalUpdate(), applyErr: &update.BusyError{Reason: reason},
		}
	})

	got := decodeStatus[problem](t,
		b.post("/admin/api/update/apply", `{"version":"2.1.0"}`), http.StatusConflict)
	if got.Message != reason {
		t.Fatalf("message = %q, attendu la phrase du garde-fou %q", got.Message, reason)
	}
}

// TestAStationWithoutAnUpdaterAnswers501AndKeepsWeighing.
//
// The Linux binary carries the routes and says honestly that it cannot: hiding
// them would leave a screen guessing, and a button doing nothing would be worse.
func TestAStationWithoutAnUpdaterAnswers501AndKeepsWeighing(t *testing.T) {
	b := adminBench(t, func(o *benchOptions) { o.update = nil })

	got := decodeStatus[problem](t,
		b.post("/admin/api/update/apply", `{"version":"2.1.0"}`), http.StatusNotImplemented)
	if got.Code != "ERR-UPD-05" {
		t.Errorf("code = %q, attendu ERR-UPD-05", got.Code)
	}
	// And reading still answers, so the page can say why the button is absent.
	read := decodeStatus[updateDTO](t, b.get("/admin/api/update"), http.StatusOK)
	if read.Supported {
		t.Error("un poste sans mise à jour se déclare géré")
	}
	if read.Running == "" {
		t.Error("la version installée n'est pas servie : la page n'a rien à afficher")
	}
}

// TestAnUnreadableBodyIsARequestErrorAndNotAGateway.
func TestAnUnreadableBodyIsARequestErrorAndNotAGateway(t *testing.T) {
	b := adminBench(t, func(o *benchOptions) {
		o.update = &stubUpdater{status: nominalUpdate()}
	})
	response := b.post("/admin/api/update/apply", `pas du json`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut %d, attendu 400", response.StatusCode)
	}
}

// TestCheckPollsAndAnswersTheFreshState: the button says « vérifier maintenant »,
// so it must both poll and hand back what it found, without a second round trip.
func TestCheckPollsAndAnswersTheFreshState(t *testing.T) {
	updater := &stubUpdater{status: nominalUpdate()}
	b := adminBench(t, func(o *benchOptions) { o.update = updater })

	got := decodeStatus[updateDTO](t, b.post("/admin/api/update/check", `{}`), http.StatusOK)
	if updater.checks != 1 {
		t.Fatalf("%d sondage(s), attendu 1", updater.checks)
	}
	if got.Latest != "2.1.0" {
		t.Errorf("état rendu après le sondage = %+v", got)
	}
}

// TestAFailedCheckIsARefusalAndNotAnEmptyPage.
func TestAFailedCheckIsARefusalAndNotAnEmptyPage(t *testing.T) {
	b := adminBench(t, func(o *benchOptions) {
		o.update = &stubUpdater{status: nominalUpdate(), checkErr: update.ErrUnreachable}
	})

	got := decodeStatus[problem](t,
		b.post("/admin/api/update/check", `{}`), http.StatusBadGateway)
	if got.Code != "ERR-UPD-01" {
		t.Errorf("code = %q", got.Code)
	}
}

// TestARepositoryWithNoReleaseIsNotABreakdown: a fork that has published nothing
// stable answers a sentence, not a gateway error.
func TestARepositoryWithNoReleaseIsNotABreakdown(t *testing.T) {
	b := adminBench(t, func(o *benchOptions) {
		o.update = &stubUpdater{status: nominalUpdate(), checkErr: update.ErrNoRelease}
	})

	got := decodeStatus[problem](t,
		b.post("/admin/api/update/check", `{}`), http.StatusOK)
	if got.Message == "" {
		t.Fatal("un dépôt sans version ne dit rien")
	}
	if got.Code != "" {
		t.Errorf("code %q : ce n'est pas une panne", got.Code)
	}
}

// TestTheLastOutcomeReachesTheScreen: whatever happened, the page tells it -- even
// if the browser was closed while the station was down.
func TestTheLastOutcomeReachesTheScreen(t *testing.T) {
	status := nominalUpdate()
	status.HasOutcome = true
	status.Outcome = update.Outcome{
		Status: update.StatusRolledBack, ExitCode: 10, From: "2.0.3", To: "2.1.0",
		Reason: "le poste ne répond pas", FinishedAt: epoch,
	}
	b := newBench(t, func(o *benchOptions) { o.update = &stubUpdater{status: status} })

	got := decodeStatus[updateDTO](t, b.get("/admin/api/update"), http.StatusOK)
	if got.Outcome == nil {
		t.Fatal("le dernier compte rendu n'atteint pas l'écran")
	}
	if got.Outcome.Status != update.StatusRolledBack || got.Outcome.To != "2.1.0" {
		t.Errorf("compte rendu = %+v", got.Outcome)
	}
	if got.Outcome.FinishedAt == "" {
		t.Error("le compte rendu ne dit pas quand")
	}
}
