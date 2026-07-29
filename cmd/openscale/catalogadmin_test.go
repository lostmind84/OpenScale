package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openscale/internal/fake"
	"openscale/internal/station/ports"
)

// reloadEpoch is the instant the fake clock of this file starts from.
var reloadEpoch = time.Date(2026, 7, 29, 9, 0, 0, 0, time.UTC)

// TestTheReloadRefusalNamesAScreenThatExists: « réglages avancés » was removed by the
// administration rework of 27/07/2026, and the settings of the catalog now live on the
// Catalogue page. A refusal that sends somebody to a screen that is not there is worse
// than one that says nothing.
//
// An empty liveCatalog is the station of guiding principle 7: no source could be built, the
// station runs anyway, and this is the sentence the volunteer gets when they press the
// button.
func TestTheReloadRefusalNamesAScreenThatExists(t *testing.T) {
	_, err := adminCatalog{source: &liveCatalog{}}.Reload(context.Background())
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

// TestAReloadWithNoFileWhereTheStationWatchesSaysSo.
//
// This is the dominant case and the one the station never had a word for: the watch does
// its poll, finds nothing, forgets and returns in silence. « Le catalogue va être relu. »
// was therefore followed by nothing at all, for ever, and the volunteer had no way to
// tell that from an import still running.
func TestAReloadWithNoFileWhereTheStationWatchesSaysSo(t *testing.T) {
	directory := t.TempDir()
	source := &watchedSource{path: filepath.Join(directory, "flv_2.csv")}

	seen, err := reloadOn(source).Reload(context.Background())
	if err != nil {
		t.Fatalf("relecture = %v, attendu acceptée", err)
	}
	if !strings.Contains(seen, "flv_2.csv") || !strings.Contains(seen, directory) {
		t.Fatalf("phrase = %q, attendu qu'elle nomme le fichier et le répertoire", seen)
	}
	if !strings.Contains(seen, "Aucun fichier") {
		t.Fatalf("phrase = %q, attendu qu'elle dise l'absence", seen)
	}
	// La veille est réveillée quand même : c'est ce que le bouton annonce, et un fichier
	// posé entre le regard et l'appel serait lu tout de suite plutôt qu'au tour suivant.
	if !source.woken {
		t.Fatal("la veille n'a pas été réveillée")
	}
}

// TestAReloadWithTheFileInPlaceSaysItIsThere.
func TestAReloadWithTheFileInPlaceSaysItIsThere(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "flv_2.csv")
	if err := os.WriteFile(path, []byte("id;nom\n"), 0o600); err != nil {
		t.Fatalf("écriture du fichier surveillé : %v", err)
	}
	source := &watchedSource{path: path}

	seen, err := reloadOn(source).Reload(context.Background())
	if err != nil {
		t.Fatalf("relecture = %v, attendu acceptée", err)
	}
	if strings.Contains(seen, "Aucun fichier") {
		t.Fatalf("phrase = %q : le fichier est là", seen)
	}
	if !strings.Contains(seen, "flv_2.csv") {
		t.Fatalf("phrase = %q, attendu qu'elle nomme le fichier", seen)
	}
	if !source.woken {
		t.Fatal("la veille n'a pas été réveillée alors que le fichier est là")
	}
}

// TestAReloadOnASourceWithNoLocalFileClaimsNothingAboutOne.
//
// A share is watched over the network and has no file of this machine to look at. The
// answer must not assert an absence nobody checked: that is how a screen learns to say
// « il n'y a rien » about a catalog sitting on a server.
func TestAReloadOnASourceWithNoLocalFileClaimsNothingAboutOne(t *testing.T) {
	source := &remoteSource{}

	seen, err := reloadOn(source).Reload(context.Background())
	if err != nil {
		t.Fatalf("relecture = %v, attendu acceptée", err)
	}
	if seen != "" {
		t.Fatalf("phrase = %q, attendu rien : aucun fichier local n'a été regardé", seen)
	}
	if !source.woken {
		t.Fatal("la veille n'a pas été réveillée")
	}
}

// reloadOn builds the adapter over one source, with the injected clock the budget needs.
func reloadOn(source ports.CatalogSource) adminCatalog {
	live := &liveCatalog{}
	live.hold(source)
	return adminCatalog{source: live, clock: fake.NewClock(reloadEpoch)}
}

// watchedSource is a local drop: it can be woken, and it names a file of this machine.
type watchedSource struct {
	path  string
	woken bool
}

func (s *watchedSource) Name() string { return "local_drop" }
func (s *watchedSource) Path() string { return s.path }
func (s *watchedSource) Wake()        { s.woken = true }

func (s *watchedSource) Next(ctx context.Context) (*ports.Batch, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *watchedSource) Acknowledge(context.Context, *ports.Batch, ports.BatchResult) error {
	return nil
}

func (s *watchedSource) Close() error { return nil }

// remoteSource is a share: wakeable, but with no file this machine can look at.
type remoteSource struct{ woken bool }

func (s *remoteSource) Name() string { return "webdav" }
func (s *remoteSource) Wake()        { s.woken = true }

func (s *remoteSource) Next(ctx context.Context) (*ports.Batch, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *remoteSource) Acknowledge(context.Context, *ports.Batch, ports.BatchResult) error {
	return nil
}

func (s *remoteSource) Close() error { return nil }

var (
	_ ports.CatalogSource = (*watchedSource)(nil)
	_ ports.CatalogSource = (*remoteSource)(nil)
)
