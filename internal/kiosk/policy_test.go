package kiosk

import (
	"strings"
	"testing"

	"openscale/internal/platform"
)

// TestEveryBrowserOfTheSearchOrderKnowsWhereItsPoliciesLive holds the one property that
// makes the mechanism worth anything : un navigateur que §15.2 peut retenir et dont on ne
// saurait pas poser la stratégie serait un poste verrouillé sur trois machines et ouvert
// sur la quatrième, sans que rien ne le dise.
func TestEveryBrowserOfTheSearchOrderKnowsWhereItsPoliciesLive(t *testing.T) {
	for _, candidate := range append(append([]string{}, WindowsCandidates...), LinuxCandidates...) {
		if root := PolicyRoot(Browser{Name: candidate, Path: candidate}); root == "" {
			t.Errorf("%s est dans l'ordre de recherche et n'a pas de racine de stratégie", candidate)
		}
	}
}

// TestNoPolicyRootLivesOutsideTheVendorTable : le kiosque ÉCRIT ces clés et « openscale
// doctor » les RELIT. Le jour où les deux listes divergent, un poste se déclare verrouillé
// sans l'être — la panne exacte que ce contrôle est censé voir.
func TestNoPolicyRootLivesOutsideTheVendorTable(t *testing.T) {
	known := map[string]bool{}
	for _, vendor := range PolicyVendors {
		known[vendor.Root] = true
	}
	for candidate, root := range PolicyRoots {
		if !known[root] {
			t.Errorf("%s pointe sur %q, absent de PolicyVendors : doctor ne relira jamais cette clé",
				candidate, root)
		}
	}
}

// TestThePolicyRootFollowsTheVendor : Edge et Chrome lisent deux clés différentes, et
// écrire celle de l'un pour l'autre est une stratégie qui ne s'applique jamais — sans
// erreur, sans trace.
func TestThePolicyRootFollowsTheVendor(t *testing.T) {
	edge := PolicyRoot(Browser{Path: `C:\Program Files\Microsoft\Edge\Application\msedge.exe`})
	chrome := PolicyRoot(Browser{Path: `C:\Program Files\Google\Chrome\Application\chrome.exe`})
	if edge != `Software\Policies\Microsoft\Edge` {
		t.Errorf("racine d'Edge = %q", edge)
	}
	if chrome != `Software\Policies\Google\Chrome` {
		t.Errorf("racine de Chrome = %q", chrome)
	}
}

// TestAnUnknownBrowserPosesNoPolicy : ne rien savoir est une réponse. Deviner un chemin de
// registre pour un navigateur qu'on ne connaît pas écrit une clé que personne ne lit.
func TestAnUnknownBrowserPosesNoPolicy(t *testing.T) {
	if root := PolicyRoot(Browser{Path: "/usr/bin/firefox"}); root != "" {
		t.Errorf("racine %q inventée pour un navigateur inconnu", root)
	}
}

// TestNothingIsAllowedExceptTheStation est le mécanisme lui-même : tout interdire, puis
// rouvrir l'adresse du poste. Une liste de moteurs de recherche à interdire serait une
// liste à tenir à jour ; celle-ci reste vraie.
func TestNothingIsAllowedExceptTheStation(t *testing.T) {
	values := Policies("http://127.0.0.1:8085")

	if got := textOf(t, values, "URLBlocklist", "1"); got != "*" {
		t.Fatalf("URLBlocklist\\1 = %q, attendu « * » : sans cette valeur rien n'est interdit", got)
	}
	if got := textOf(t, values, "URLAllowlist", "1"); got != "http://127.0.0.1:8085" {
		t.Fatalf("URLAllowlist\\1 = %q : le poste ne s'autorise pas lui-même", got)
	}
}

// TestTheRescuePageIsNotBlocked : la page de secours est ouverte en file:// PARCE QUE le
// poste ne répond pas. Une liste noire qui l'avale remplace « Le poste redémarre… » par
// « Bloqué par votre administrateur » sur le seul écran dont c'est tout le métier d'être
// lisible quand rien ne marche.
func TestTheRescuePageIsNotBlocked(t *testing.T) {
	values := Policies("http://127.0.0.1:8085")
	allowed := []string{}
	for _, value := range values {
		if value.Key == "URLAllowlist" {
			allowed = append(allowed, value.Text)
		}
	}
	for _, pattern := range allowed {
		if strings.HasPrefix(pattern, "file:") {
			return
		}
	}
	t.Fatalf("aucun motif file:// dans la liste blanche %v : la page de secours est bloquée", allowed)
}

// TestATrailingSlashDoesNotNarrowTheAllowlist : network.listen s'écrit avec ou sans barre
// finale, et un motif « http://poste:8085/ » n'autorise que cette URL-là — pas la grille,
// pas /admin, pas /assets. Le poste s'ouvrirait sur une page blanche.
func TestATrailingSlashDoesNotNarrowTheAllowlist(t *testing.T) {
	values := Policies("http://127.0.0.1:8085/")
	if got := textOf(t, values, "URLAllowlist", "1"); got != "http://127.0.0.1:8085" {
		t.Fatalf("URLAllowlist\\1 = %q : la barre finale n'a pas été coupée", got)
	}
}

// TestTheContextMenuLosesItsSearchEntry : c'est le geste EXACT qui a sorti un poste de
// l'application le 31/07/2026. Sans fournisseur de recherche, l'entrée n'est plus dessinée
// — au lieu d'être cliquée puis refusée, ce qui laisse encore une page qui n'est pas la
// grille devant le client.
func TestTheContextMenuLosesItsSearchEntry(t *testing.T) {
	assertNumber(t, Policies("http://127.0.0.1:8085"), "DefaultSearchProviderEnabled", 0)
}

// TestTheOtherWaysOutAreClosed énumère les portes qui restent quand la barre d'adresse
// n'existe pas : la boîte de dialogue de fichier (Ctrl+O), les outils de développement
// (F12) et la fenêtre privée, que le superviseur ne surveille pas.
func TestTheOtherWaysOutAreClosed(t *testing.T) {
	values := Policies("http://127.0.0.1:8085")
	assertNumber(t, values, "AllowFileSelectionDialogs", 0)
	assertNumber(t, values, "DeveloperToolsAvailability", 2)
	assertNumber(t, values, "IncognitoModeAvailability", 1)
}

// textOf lit la donnée d'une valeur texte, et échoue en la nommant.
func textOf(t *testing.T, values []platform.PolicyValue, key, name string) string {
	t.Helper()
	for _, value := range values {
		if value.Key == key && value.Name == name {
			if value.Kind != platform.PolicyString {
				t.Fatalf("la stratégie %s\\%s n'est pas une chaîne : une valeur du mauvais "+
					"type est ignorée en silence par le navigateur", key, name)
			}
			return value.Text
		}
	}
	t.Fatalf("la stratégie %s\\%s n'est pas posée", key, name)
	return ""
}

// assertNumber vérifie qu'une stratégie numérique porte la valeur attendue.
func assertNumber(t *testing.T, values []platform.PolicyValue, name string, expected uint32) {
	t.Helper()
	for _, value := range values {
		if value.Key == "" && value.Name == name {
			if value.Kind != platform.PolicyDWord {
				t.Errorf("la stratégie %s n'est pas un nombre", name)
				return
			}
			if value.Number != expected {
				t.Errorf("%s = %d, attendu %d", name, value.Number, expected)
			}
			return
		}
	}
	t.Errorf("la stratégie %s n'est pas posée", name)
}
