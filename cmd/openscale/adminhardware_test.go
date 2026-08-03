package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/scale/gramxfoc"
)

// The expert screens of §14.4 — the ones whose answers come from the MACHINE and not
// from the station's own state: which ports and queues exist, what the troubleshooting
// buttons do without a password, the aperçu rendered by the renderer that prints, one
// frame replayed through this station's grammar, and the roll counter of the printer
// IN SERVICE.

// TestTheTroubleshootingRoutesAnswerWithoutAPassword is ADR-033, checked in both
// directions on the same running station.
//
// The criterion moved from the DOOR to the ACT: « ce qui change ce que le poste vend, ou
// la façon dont il pèse » is protected, everything one can merely look at is not. Testing
// the scale, asking the printer for its status, printing a demonstration label, reading a
// configuration whose two hashes are redacted before they leave — none of those changes
// anything, so they answer to whoever is standing at the counter, who can already unplug
// the printer.
func TestTheTroubleshootingRoutesAnswerWithoutAPassword(t *testing.T) {
	bench := newServeBench(t, localDropCatalog)
	bench.start()

	for _, c := range []struct {
		route string
		body  string
	}{
		{"/admin/api/troubleshooting/test-scale", ""},
		{"/admin/api/troubleshooting/test-printer", ""},
		{"/admin/api/troubleshooting/test-label", ""},
		{"/admin/api/troubleshooting/reprint", ""},
		{"/admin/api/troubleshooting/reload-catalog", ""},
		{"/admin/api/troubleshooting/roll-changed", ""},
		{"/admin/api/troubleshooting/fallback-printer", `{"on":true}`},
	} {
		t.Run(c.route, func(t *testing.T) {
			response := bench.post(t, c.route, c.body)
			defer response.Body.Close()
			if response.StatusCode == http.StatusUnauthorized {
				t.Fatalf("%s exige un mot de passe : ADR-018 dit le contraire, et un bénévole "+
					"seul devant un poste muet ne peut plus rien tester", c.route)
			}
			if response.StatusCode == http.StatusNotImplemented {
				t.Fatalf("%s répond 501 : le collaborateur n'est pas câblé dans serve.go", c.route)
			}
			// The answer is French, whatever it is: this route is read by a volunteer.
			if body := readBody(t, response); !hasFrenchSentence(body) {
				t.Fatalf("%s répond %d sans phrase française : %s", c.route, response.StatusCode, body)
			}
		})
	}

	// Ce qui s'OUVRE en lecture. Le mot de passe qu'il fallait pour lire un numéro de
	// port n'achetait rien : la charge utile est expurgée de ses deux empreintes avant
	// de partir, et le journal est déjà dans diagnostic.zip, que personne ne protège.
	for _, route := range []string{
		"/admin/api/config", "/admin/api/config/versions", "/admin/api/ports",
		"/admin/api/printers", "/admin/api/journal", "/admin/api/journal/export.csv",
		"/admin/api/technical", "/admin/api/imports",
	} {
		t.Run("lecture ouverte "+route, func(t *testing.T) {
			response := bench.get(route)
			defer response.Body.Close()
			if response.StatusCode == http.StatusUnauthorized ||
				response.StatusCode == http.StatusConflict {
				t.Fatalf("%s répond %d : ADR-033 l'ouvre en LECTURE, on n'y écrit rien",
					route, response.StatusCode)
			}
		})
	}

	// Ce qui reste fermé, et les deux qui viennent d'y entrer.
	for _, c := range []struct{ method, route, body string }{
		{http.MethodPut, "/admin/api/config", `{}`},
		{http.MethodGet, "/admin/api/config/export", ""},
		{http.MethodPost, "/admin/api/config/restore", `{"version":1}`},
		// Elle coupe la balance et laisse le CLIENT taper son propre poids.
		{http.MethodPost, "/admin/api/troubleshooting/manual-entry", `{"on":true}`},
		// Il remplace toute la grille par un fichier qu'on a apporté.
		{http.MethodPost, "/admin/api/catalog/import", `{}`},
	} {
		t.Run("acte protégé "+c.route, func(t *testing.T) {
			response := bench.do(t, c.method, c.route, c.body)
			defer response.Body.Close()
			// 401 « session absente » sur un poste qui a un mot de passe, 409 « aucun mot
			// de passe posé » sinon : les deux refusent, et l'écran les distingue.
			if response.StatusCode != http.StatusUnauthorized &&
				response.StatusCode != http.StatusConflict {
				t.Fatalf("%s %s répond %d sans session : cet acte change ce que le poste "+
					"vend ou la façon dont il pèse", c.method, c.route, response.StatusCode)
			}
		})
	}
}

// TestTheHardwareRoutesAnswerFromThePlatform is the wiring of the Matériel page (§14.4).
//
// What the enumeration finds on the machine running the test is unknowable — a build agent
// has an unpredictable number of serial ports and print queues — so what is asserted is
// what a screen depends on: a 200, a well-formed list, and never a 501. A 501 here would
// mean the collaborator is not wired, which is exactly the state this lot removes.
func TestTheHardwareRoutesAnswerFromThePlatform(t *testing.T) {
	bench := newServeBench(t, withPassword)
	bench.start()
	bench.login(t)

	ports := bench.get("/admin/api/ports")
	defer ports.Body.Close()
	if ports.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/api/ports = %d : %s", ports.StatusCode, readBody(t, ports))
	}
	var enumerated struct {
		Ports []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"ports"`
	}
	decodeInto(t, ports, &enumerated)
	for _, port := range enumerated.Ports {
		if strings.TrimSpace(port.Name) == "" {
			t.Fatalf("un port sans nom est servi à l'écran : %+v", enumerated.Ports)
		}
	}

	printers := bench.get("/admin/api/printers")
	defer printers.Body.Close()
	if printers.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/api/printers = %d : %s", printers.StatusCode, readBody(t, printers))
	}
}

// TestTheLabelPreviewIsThePNGOfTheRenderer is decision A2: ONE renderer, not two.
//
// A preview produced by a second code path would be a picture of what somebody hoped the
// printer would do. The bytes are checked to be a PNG and the route to be cacheless — the
// settings screen refreshes it at every keystroke, and a cached one would show the previous
// offset.
func TestTheLabelPreviewIsThePNGOfTheRenderer(t *testing.T) {
	bench := newServeBench(t, withPassword)
	bench.start()
	bench.login(t)

	response := bench.get("/admin/api/label/preview.png?demo=1")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/api/label/preview.png = %d : %s",
			response.StatusCode, readBody(t, response))
	}
	if got := response.Header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type %q, attendu image/png", got)
	}
	if got := response.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control %q : un aperçu mis en cache montre le décalage précédent", got)
	}
	body := []byte(readBody(t, response))
	if !bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("le corps n'est pas un PNG : %q", body[:min(16, len(body))])
	}

	// The dual grid is the crowded case, and it must render too: that is what the flag is
	// for — seeing the two-tier layout without having to configure it first.
	dual := bench.get("/admin/api/label/preview.png?demo=1&dual=1")
	defer dual.Body.Close()
	if dual.StatusCode != http.StatusOK {
		t.Fatalf("aperçu bi-tarif = %d : %s", dual.StatusCode, readBody(t, dual))
	}

	// And without a weighing in flight, the aperçu of the LIVE label says so in French
	// rather than drawing an empty label.
	live := bench.get("/admin/api/label/preview.png")
	defer live.Body.Close()
	if live.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("aperçu sans pesée en cours = %d, attendu 422", live.StatusCode)
	}
}

// TestReplayingAFrameGoesThroughTheDecoder is the button « Rejouer cette trame » of the
// Journal page.
//
// The frame is the one the reference vector is written around, and the route is what turns a
// frame that caused an unexplained refusal into a permanent test — without a trip to the
// shop and without a scale. A frame the grammar of §9.2 refuses is a 422 that SAYS SO,
// because « ça ne se décode pas » is the answer, not a failure of the button.
//
// It is decoded with the grammar THIS STATION declares, which is why the bench declares
// one: a frame from the journal of this station was emitted by the scale of this station,
// and replaying it through another protocol would answer « la balance a émis quelque chose
// que la grammaire refuse » — a lie about the hardware, and an invitation to go and look
// at a scale that is fine.
func TestReplayingAFrameGoesThroughTheDecoder(t *testing.T) {
	bench := newServeBench(t, withPassword, declaringScaleType(gramxfoc.IDRS))
	bench.start()
	bench.login(t)

	response := bench.post(t, "/admin/api/replay", `{"frame":"ST,GS,+  1.236KG"}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /admin/api/replay = %d, attendu 202 : %s",
			response.StatusCode, readBody(t, response))
	}

	refused := bench.post(t, "/admin/api/replay", `{"frame":"XX,YY,ZZZ"}`)
	defer refused.Body.Close()
	if refused.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("une trame illisible répond %d, attendu 422 : %s",
			refused.StatusCode, readBody(t, refused))
	}
	if body := readBody(t, refused); !strings.Contains(body, "décode") {
		t.Fatalf("le refus ne dit pas que la trame ne se décode pas : %s", body)
	}
}

// TestAStationThatDeclaresNoProtocolCannotReplayAFrame is the refusal that replaces a
// silent wrong answer.
//
// This route used to build the grammar of §9.2 whatever scale.type said. On a station
// declaring no protocol — or another one — it therefore answered about a grammar nobody
// chose, and « cette trame ne se décode pas » would have been said of the wrong one. The
// refusal now names the setting to fill in, and names a page that exists.
func TestAStationThatDeclaresNoProtocolCannotReplayAFrame(t *testing.T) {
	bench := newServeBench(t, withPassword)
	bench.start()
	bench.login(t)

	refused := bench.post(t, "/admin/api/replay", `{"frame":"ST,GS,+  1.236KG"}`)
	defer refused.Body.Close()
	if refused.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("POST /admin/api/replay = %d sur un poste sans protocole, attendu 422 : %s",
			refused.StatusCode, readBody(t, refused))
	}
	body := readBody(t, refused)
	if !strings.Contains(body, "scale.type") {
		t.Fatalf("le refus ne nomme pas le réglage à renseigner : %s", body)
	}
}

// declaringScaleType makes the bench a station that NAMES a weighing protocol without
// opening a port: scale.present stays false, so no serial handle is taken.
//
// It is a real configuration and not a contrivance — it is what a station looks like
// between the moment « Détecter automatiquement » proposed a protocol and the moment the
// scale is plugged in — and the port has to be there because serial.OptionSchema declares
// it Required, so a type with no options would be a fault and the station would fall back
// on the neutral profile, which names no hardware at all.
func declaringScaleType(id string) func(*domain.Config) {
	return func(cfg *domain.Config) {
		cfg.Scale.Type = id
		cfg.Scale.Options = domain.DriverOptions{"port": json.RawMessage(`"COM8"`)}
	}
}

// TestTheRollAndTheFallbackActOnThePrinterInService is the wiring of two of the nine
// buttons, and of the honest refusal behind one of them.
//
// « J'ai changé le rouleau » is the only gesture that tells this application anything true
// about the paper, so it must reach the counter of the printer IN SERVICE. « Imprimer sur
// l'imprimante du poste N » must refuse, in French, on a station where no fallback is
// configured — which is the shipped state — instead of pretending to switch.
func TestTheRollAndTheFallbackActOnThePrinterInService(t *testing.T) {
	bench := newServeBench(t)
	bench.start()

	roll := bench.post(t, "/admin/api/troubleshooting/roll-changed", "")
	defer roll.Body.Close()
	if roll.StatusCode != http.StatusOK {
		t.Fatalf("roll-changed = %d : %s", roll.StatusCode, readBody(t, roll))
	}

	fallback := bench.post(t, "/admin/api/troubleshooting/fallback-printer", `{"on":true}`)
	defer fallback.Body.Close()
	if fallback.StatusCode == http.StatusOK {
		t.Fatal("la bascule vers une imprimante de secours a réussi sur un poste qui n'en " +
			"déclare aucune")
	}
	if body := readBody(t, fallback); !strings.Contains(body, "secours") {
		t.Fatalf("le refus ne nomme pas ce qui manque : %s", body)
	}
}
