package kiosk

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestTheSearchOrderOfSection15_2IsRespected is the order §15.2 fixes, and the reason
// it is fixed: Edge is present on every Windows station of the parc without anybody
// installing anything, so a station where both are installed must not open Chrome —
// which would give two stations two different kiosk behaviours.
func TestTheSearchOrderOfSection15_2IsRespected(t *testing.T) {
	installed := map[string]string{
		"chrome.exe": `C:\Program Files\Google\Chrome\Application\chrome.exe`,
		"msedge.exe": `C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
	}
	browser, found := Find(WindowsCandidates, func(candidate string) (string, bool) {
		path, ok := installed[candidate]
		return path, ok
	})
	if !found {
		t.Fatal("deux navigateurs installés et aucun trouvé")
	}
	if browser.Name != "msedge.exe" {
		t.Fatalf("navigateur retenu %q, attendu msedge.exe : Edge passe avant Chrome (§15.2)", browser.Name)
	}
}

// TestAStationWithNoBrowserIsSaidSoAndNotGuessed keeps the one failure no relaunch
// fixes from being reported as something else.
func TestAStationWithNoBrowserIsSaidSoAndNotGuessed(t *testing.T) {
	if _, found := Find(WindowsCandidates, func(string) (string, bool) { return "", false }); found {
		t.Fatal("un poste sans navigateur ne doit pas en trouver un")
	}
}

// TestEveryArgumentOfSection15_2IsOnTheCommandLine holds the command line to the
// document, switch by switch: each one is there for a failure somebody diagnosed, and
// a silent removal is a failure that comes back.
func TestEveryArgumentOfSection15_2IsOnTheCommandLine(t *testing.T) {
	arguments := Arguments(Browser{Name: "chromium", Path: "/usr/bin/chromium"},
		"http://127.0.0.1:8085", "/tmp/profile")
	line := strings.Join(arguments, " ")

	for _, required := range []string{
		"--kiosk", "http://127.0.0.1:8085",
		"--user-data-dir=/tmp/profile",
		"--no-first-run",
		"--disable-session-crashed-bubble",
		"--noerrdialogs",
		"--check-for-update-interval=31536000",
		"--autoplay-policy=no-user-gesture-required",
	} {
		if !strings.Contains(line, required) {
			t.Errorf("argument %q absent de la ligne de commande : %s", required, line)
		}
	}
	if strings.Contains(line, "--edge-kiosk-type") {
		t.Error("--edge-kiosk-type passé à Chromium, qui ne le connaît pas")
	}
}

// TestEdgeGetsItsOwnFullScreenSwitch is the one difference §15.2 records between the
// browsers, and Edge without it opens a kiosk window that is not full screen.
func TestEdgeGetsItsOwnFullScreenSwitch(t *testing.T) {
	edge := Browser{Name: "msedge.exe", Path: `C:\Program Files\Microsoft\Edge\Application\msedge.exe`}
	if !edge.IsEdge() {
		t.Fatal("msedge.exe n'a pas été reconnu comme Edge")
	}
	line := strings.Join(Arguments(edge, "http://127.0.0.1:8085", `C:\Temp\profile`), " ")
	if !strings.Contains(line, "--edge-kiosk-type=fullscreen") {
		t.Fatalf("Edge sans --edge-kiosk-type=fullscreen : %s", line)
	}
}

// TestTheRescuePageIsAFileTheBrowserCanOpen checks the two things that make the rescue
// page work at all: it exists on disk, and the URL is one a browser accepts on this
// platform — file:///C:/… needs three slashes, /tmp/… already has one.
func TestTheRescuePageIsAFileTheBrowserCanOpen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "profile")
	url, err := WriteRescuePage(dir, RescueWaiting, "http://127.0.0.1:8085", 0)
	if err != nil {
		t.Fatalf("écriture de la page de secours : %v", err)
	}
	if !strings.HasPrefix(url, "file:///") {
		t.Fatalf("URL de la page de secours %q : un navigateur attend file:///", url)
	}
	if strings.Contains(url, `\`) {
		t.Fatalf("URL de la page de secours %q : un antislash n'est pas un séparateur d'URL", url)
	}
	raw, err := os.ReadFile(filepath.Join(dir, RescueFileName))
	if err != nil {
		t.Fatalf("relecture de la page de secours : %v", err)
	}
	page := string(raw)
	if !strings.Contains(page, "Le poste redémarre") {
		t.Errorf("la page d'attente ne dit pas « Le poste redémarre… » : %s", page)
	}
	if strings.Contains(page, CodeCrashLoop) {
		t.Error("la page d'attente porte ERR-KSK-02, qui est le code de la boucle de plantage")
	}
	if strings.Contains(page, "<script") || strings.Contains(page, "http://fonts") {
		t.Error("la page de secours dépend de quelque chose : elle doit être autonome")
	}
}

// TestTheCrashLoopPageCarriesTheCodeAndTheCount is what a volunteer reads out over the
// telephone: the code §15.2 allocates, and how many times it happened.
func TestTheCrashLoopPageCarriesTheCodeAndTheCount(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteRescuePage(dir, RescueCrashLoop, "http://127.0.0.1:8085", 21); err != nil {
		t.Fatalf("écriture de la page de secours : %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, RescueFileName))
	if err != nil {
		t.Fatalf("relecture : %v", err)
	}
	page := string(raw)
	for _, expected := range []string{CodeCrashLoop, "21 arrêts", "openscale doctor"} {
		if !strings.Contains(page, expected) {
			t.Errorf("la page de secours ne porte pas %q :\n%s", expected, page)
		}
	}
}

// TestTheAddressIsEscapedInTheRescuePage keeps a configuration file from writing HTML
// into a page. The address comes from network.listen, which a human edits.
func TestTheAddressIsEscapedInTheRescuePage(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteRescuePage(dir, RescueWaiting, `<script>alert(1)</script>`, 0); err != nil {
		t.Fatalf("écriture : %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, RescueFileName))
	if strings.Contains(string(raw), "<script>alert") {
		t.Fatal("l'adresse est recopiée telle quelle dans la page de secours")
	}
}

// TestTheBrowserIsLookedForWhereItHidesFromAPath covers the case that makes a Windows
// kiosk fail with a browser installed: Edge and Chrome live outside the PATH of the
// unprivileged account the kiosk runs as.
func TestTheBrowserIsLookedForWhereItHidesFromAPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("les répertoires Program Files ne concernent que Windows")
	}
	root := t.TempDir()
	target := filepath.Join(root, `Microsoft\Edge\Application\msedge.exe`)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("préparation : %v", err)
	}
	if err := os.WriteFile(target, []byte("faux navigateur"), 0o644); err != nil {
		t.Fatalf("préparation : %v", err)
	}

	look := LookBrowser([]string{root})
	path, found := look("msedge.exe")
	if !found {
		t.Fatal("un Edge présent hors du PATH n'a pas été trouvé")
	}
	if path != target {
		t.Fatalf("chemin trouvé %q, attendu %q", path, target)
	}
	if _, found := look("chrome.exe"); found {
		t.Error("un Chrome absent a été trouvé")
	}
}
