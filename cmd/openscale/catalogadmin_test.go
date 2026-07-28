package main

import (
	"context"
	"strings"
	"testing"
)

// TestTheReloadRefusalNamesAScreenThatExists: « réglages avancés » was removed by the
// administration rework of 27/07/2026, and the settings of the catalog now live on the
// Catalogue page. A refusal that sends somebody to a screen that is not there is worse
// than one that says nothing.
//
// An empty liveCatalog is the station of guiding principle 7: no source could be built, the
// station runs anyway, and this is the sentence the volunteer gets when they press the
// button.
func TestTheReloadRefusalNamesAScreenThatExists(t *testing.T) {
	err := adminCatalog{source: &liveCatalog{}}.Reload(context.Background())
	if err == nil {
		t.Fatal("un poste sans source doit refuser de recharger")
	}
	if strings.Contains(err.Error(), "réglages avancés") {
		t.Error("le refus renvoie vers un écran supprimé")
	}
	if !strings.Contains(err.Error(), "Catalogue") {
		t.Errorf("le refus ne nomme pas la page où corriger : %s", err)
	}
}
