package kiosk

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
)

// RescueReason is why the kiosk is showing a local page instead of the client screen.
type RescueReason int

const (
	// RescueWaiting is « le service n'a pas encore répondu ». It is the ordinary state
	// of the ten seconds after a power cut: the scheduled task fires at logon, the
	// service is still opening its database, and a customer must see a sentence rather
	// than a browser error page.
	RescueWaiting RescueReason = iota
	// RescueCrashLoop is the twenty-first quick death of §15.2 — ERR-KSK-02.
	RescueCrashLoop
)

// CodeCrashLoop is the only ERR code §15.2 allocates to the kiosk, and it belongs to
// the crash loop.
//
// The waiting page carries NO code on purpose: §15.4 gives that row none either, the
// remedy is « attendre 5 s ; sinon openscale doctor », and a code invented here would
// be repeated over the telephone as if a binary emitted it.
const CodeCrashLoop = "ERR-KSK-02"

// RescueFileName is the page written inside the browser profile directory.
//
// Inside the profile, and not beside the binary: the profile is wiped and rewritten at
// every start, so the page can never be a stale file left by a previous version, and
// the kiosk account can write there without any ACL of its own.
const RescueFileName = "rescue.html"

// WriteRescuePage renders the local page and returns the file:// URL that shows it.
//
// It is a FILE and not a route of the station, and that is the entire point: the two
// situations it covers are « the station does not answer » and « the browser cannot
// stay up long enough to load anything ». A rescue page served by the thing that is
// down would be a rescue page nobody ever sees.
func WriteRescuePage(dir string, reason RescueReason, address string, shortLives int) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("le répertoire de profil %s n'a pas pu être créé : %w", dir, err)
	}
	path := filepath.Join(dir, RescueFileName)
	if err := os.WriteFile(path, []byte(rescueHTML(reason, address, shortLives)), 0o644); err != nil {
		return "", fmt.Errorf("la page de secours %s n'a pas pu être écrite : %w", path, err)
	}
	return fileURL(path), nil
}

// fileURL builds the URL a browser takes for a local file, on both platforms.
func fileURL(path string) string {
	slashed := filepath.ToSlash(path)
	// A Windows path starts with a drive letter, and file:///C:/… needs the third
	// slash; a POSIX path already starts with one.
	if len(slashed) > 0 && slashed[0] == '/' {
		return "file://" + slashed
	}
	return "file:///" + slashed
}

// rescueHTML is the page itself: French, no network, no script, no font to load.
//
// It is deliberately unlike the client screen. A customer must not mistake it for a
// working station, and a volunteer must be able to read the one instruction from three
// metres away.
func rescueHTML(reason RescueReason, address string, shortLives int) string {
	title, message, instruction := "", "", ""
	switch reason {
	case RescueCrashLoop:
		title = "Le poste rencontre un problème"
		message = CodeCrashLoop + " — l'affichage n'arrive pas à rester ouvert " +
			fmt.Sprintf("(%d arrêts en moins de 10 secondes dans la dernière heure).", shortLives)
		instruction = "Prévenez un responsable. Ouvrez « openscale doctor » sur ce poste : " +
			"il dira ce qui manque. En attendant, la caisse peut peser au comptoir."
	default:
		title = "Le poste redémarre…"
		message = "L'écran de pesée revient dès que le service répond."
		instruction = "Patientez quelques secondes. Si ce message reste affiché, " +
			"prévenez un responsable — il lancera « openscale doctor »."
	}

	return `<!doctype html>
<html lang="fr">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + html.EscapeString(title) + `</title>
<style>
  html, body { height: 100%; margin: 0; }
  body {
    display: flex; flex-direction: column; align-items: center; justify-content: center;
    background: #FFFFFF; color: #1A1A1A;
    font-family: system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
    text-align: center; padding: 2rem;
  }
  h1 { font-size: clamp(2rem, 6vw, 4rem); margin: 0 0 1.5rem; }
  p  { font-size: clamp(1.1rem, 2.5vw, 1.75rem); max-width: 40ch; line-height: 1.4; margin: 0 0 1rem; }
  .instruction { color: #333333; }
  .address { font-size: 1rem; color: #555555; margin-top: 3rem; }
</style>
</head>
<body>
  <h1>` + html.EscapeString(title) + `</h1>
  <p>` + html.EscapeString(message) + `</p>
  <p class="instruction">` + html.EscapeString(instruction) + `</p>
  <p class="address">Poste attendu sur ` + html.EscapeString(address) + `</p>
</body>
</html>
`
}
