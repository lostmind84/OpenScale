package kiosk

import (
	"strings"

	"openscale/internal/platform"
)

// PolicyVendor is one browser family: the name a report prints, and the key its browsers
// read their policies from.
type PolicyVendor struct {
	// Label is FRENCH-facing and is the product name — « Microsoft Edge ». It is what
	// `openscale doctor` names when it says whose policies it read.
	Label string
	// Root is the policy path, relative to the hive of the account that runs the browser.
	Root string
}

// PolicyVendors are the three families §15.2 can retain, and the authority on where a
// policy goes.
//
// A slice and not three constants scattered about: the kiosk WRITES these keys and doctor
// READS them, and the day the two disagree is the day a station reports itself locked and
// is not. A test holds PolicyRoots to naming nothing outside this list.
var PolicyVendors = []PolicyVendor{
	{Label: "Microsoft Edge", Root: `Software\Policies\Microsoft\Edge`},
	{Label: "Google Chrome", Root: `Software\Policies\Google\Chrome`},
	{Label: "Chromium", Root: `Software\Policies\Chromium`},
}

// PolicyRoots is where each executable of the search order reads its policies.
//
// The key is the executable name Find resolved, so the table is read by the same name the
// log line prints, and a browser this map does not know is a browser this station poses no
// policy for — which the caller says out loud rather than guessing a path.
var PolicyRoots = map[string]string{
	"msedge.exe":       PolicyVendors[0].Root,
	"chrome.exe":       PolicyVendors[1].Root,
	"chromium.exe":     PolicyVendors[2].Root,
	"chromium":         PolicyVendors[2].Root,
	"chromium-browser": PolicyVendors[2].Root,
	"google-chrome":    PolicyVendors[1].Root,
	"chrome":           PolicyVendors[1].Root,
}

// PolicyRoot is where this browser reads its policies, or the empty string.
func PolicyRoot(browser Browser) string {
	return PolicyRoots[strings.ToLower(baseName(browser.Path))]
}

// Policies is what keeps the station INSIDE the application, and it exists because a
// command line cannot say it.
//
// # The failure it answers
//
// A right click on the administration screen — the one surface where the context menu is
// deliberately left alive, so that « Copier » works on an error a volunteer is reading over
// the telephone (§14.1) — offers « Rechercher sur le web ». One click, and the kiosk window
// is on a search engine. There is no address bar and no back button in a `--kiosk` window,
// the browser is perfectly alive so the supervisor sees nothing to relaunch, and the
// station is lost until somebody knows about Alt+←. The same click on the local rescue
// page, which carries no script by design, does the same thing.
//
// # What each value closes
//
// The first two are the mechanism; the rest close the doors that would make a customer
// look at something other than the grid.
func Policies(url string) []platform.PolicyValue {
	return []platform.PolicyValue{
		// Everything is forbidden, and then this station's own address is allowed back.
		// Denying a list of search engines would be a list to keep up to date; denying
		// everything is a rule that stays true.
		platform.PolicyText("URLBlocklist", "1", "*"),
		platform.PolicyText("URLAllowlist", "1", policyPattern(url)),
		// The rescue page. It is served over file:// BECAUSE the station is not answering
		// (§15.2), so a blocklist that swallowed it would replace « Le poste redémarre… »
		// with « Bloqué par votre administrateur » on the one screen whose whole job is to
		// be readable when nothing works. The scheme is opened rather than the exact path:
		// the profile directory moves with --profile, and a pattern that has to be right
		// about a path is a pattern that is one day wrong about it.
		platform.PolicyText("URLAllowlist", "2", "file://*"),
		// The file dialog is the other way to reach a file:// address without an address
		// bar, and Ctrl+O opens it inside a kiosk window.
		platform.PolicyNumber("", "AllowFileSelectionDialogs", 0),
		// No search provider: this is what removes « Rechercher sur le web » from the
		// context menu itself, instead of letting it be clicked and refused. A refusal
		// page is still a page that is not the grid.
		platform.PolicyNumber("", "DefaultSearchProviderEnabled", 0),
		// 2 = disallowed. F12 in front of a customer is a black panel over half the grid.
		platform.PolicyNumber("", "DeveloperToolsAvailability", 2),
		// 1 = incognito disabled. A private window is a second window the supervisor does
		// not watch.
		platform.PolicyNumber("", "IncognitoModeAvailability", 1),
		// Nobody signs a station into a browser account, and the invitation to do it is a
		// bubble over the grid.
		platform.PolicyNumber("", "BrowserSignin", 0),
		// 3 = all downloads blocked. The station downloads nothing; the journal and the
		// configuration are exported from the administration screen, which builds them in
		// the page (§14.4) — a Blob, and never a fetch a download policy would see.
		platform.PolicyNumber("", "DownloadRestrictions", 3),
		// Ctrl+P. The labels leave through the queue the SERVICE holds (§8.4): a print
		// dialog on this screen can only print the screen itself.
		platform.PolicyNumber("", "PrintingEnabled", 0),
		// « Voulez-vous enregistrer ce mot de passe ? » over the administration password
		// field, on a profile that is wiped at every start anyway.
		platform.PolicyNumber("", "PasswordManagerEnabled", 0),
	}
}

// policyPattern turns the station's address into a URL filter.
//
// Chromium matches a pattern with no path against every path of that host, which is what
// « this station, all of it » means. The trailing slash is cut because the address comes
// from network.listen through the composition root and a station configured with one would
// otherwise allow that exact URL and nothing under it.
func policyPattern(url string) string {
	return strings.TrimSuffix(url, "/")
}
