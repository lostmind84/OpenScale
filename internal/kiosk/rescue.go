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
	// RescueWaiting is « le poste a répondu, et il ne répond plus ». Something the
	// customer was using went away: the sentence may legitimately talk about a station
	// coming back.
	RescueWaiting RescueReason = iota
	// RescueStarting is the same silence, before the station has EVER answered — a cold
	// boot, where the service is on delayed automatic start and the kiosk task fires five
	// seconds after logon. Nothing has restarted, and a page that says so worries a
	// volunteer about a station that is merely switching on.
	RescueStarting
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
	title := rescueTitle(reason)
	var message, instruction string
	switch reason {
	case RescueCrashLoop:
		message = CodeCrashLoop + " — l'affichage n'arrive pas à rester ouvert " +
			fmt.Sprintf("(%d arrêts en moins de 10 secondes dans la dernière heure).", shortLives)
		instruction = "Prévenez un responsable. Ouvrez « openscale doctor » sur ce poste : " +
			"il dira ce qui manque. En attendant, la caisse peut peser au comptoir."
	case RescueStarting:
		message = "L'écran de pesée s'ouvre dès que le service a fini de démarrer."
		instruction = "Patientez quelques secondes. Si ce message reste affiché, " +
			"prévenez un responsable — il lancera « openscale doctor »."
	default:
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
` + rescueAnimation(reason) + `</style>
</head>
<body>
  <h1>` + html.EscapeString(title) + `</h1>
  <p>` + html.EscapeString(message) + `</p>
` + rescueDots(reason) + `  <p class="instruction">` + html.EscapeString(instruction) + `</p>
  <p class="address">Poste attendu sur ` + html.EscapeString(address) + `</p>
</body>
</html>
`
}

// rescueTitle is the one line read from three metres away, and the only difference
// between the two waiting reasons.
func rescueTitle(reason RescueReason) string {
	switch reason {
	case RescueCrashLoop:
		return "Le poste rencontre un problème"
	case RescueStarting:
		return "Application en cours de démarrage…"
	default:
		return "Le poste redémarre…"
	}
}

// rescueAnimation is the three-dot pulse, in CSS and never in JavaScript.
//
// A page opened over file:// is the one place on this station where a script is what a
// browser policy could refuse, and the page whose whole job is to be displayed when
// nothing else works must not depend on anything.
//
// It is absent from the crash-loop page ON PURPOSE: that page is not waiting for
// anything, and something that moves on it would promise a return that is not coming —
// the flicker §15.2 opened it to stop, at a slower speed.
func rescueAnimation(reason RescueReason) string {
	if reason == RescueCrashLoop {
		return ""
	}
	return `  .dots { display: flex; gap: 0.9rem; margin: 0.5rem 0 1.5rem; }
  .dots span {
    width: 0.9rem; height: 0.9rem; border-radius: 50%; background: #1A1A1A;
    opacity: 0.25; animation: pulse 1.4s ease-in-out infinite;
  }
  .dots span:nth-child(2) { animation-delay: 0.2s; }
  .dots span:nth-child(3) { animation-delay: 0.4s; }
  @keyframes pulse { 0%, 100% { opacity: 0.25; } 50% { opacity: 1; } }
`
}

// rescueDots is the markup the animation above moves.
func rescueDots(reason RescueReason) string {
	if reason == RescueCrashLoop {
		return ""
	}
	// aria-hidden: it carries no information a screen reader would want — the sentence
	// above it already says what is happening.
	return "  <div class=\"dots\" aria-hidden=\"true\"><span></span><span></span><span></span></div>\n"
}
