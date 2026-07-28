package platform

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"openscale/internal/domain"
)

// witnessName is the file the drop probe writes and removes.
//
// It is named after the product and starts with a dot: whoever finds it in a producer's
// directory must be able to tell what wrote it, and a probe that crashed between the
// write and the remove must not look like a catalog.
const witnessName = ".openscale-write-test"

// pathChecker answers, from the context of the SERVICE ACCOUNT, the two questions a pure
// validation cannot ask.
//
// The distinction matters on Windows and it is the whole reason this type exists: a Z:
// drive is a mapping made by a user SESSION and a service does not see it. The
// configuration screen therefore learns at the moment somebody types the path, and not at
// the first import that never comes.
type pathChecker struct{ dataDir string }

// NewPathChecker returns the checker of a running station.
//
// dataDir is what lets it recognise the station's own archive directory, which is the one
// directory that must never be watched: the station would read back the copies it just
// made, for ever.
func NewPathChecker(dataDir string) domain.PathChecker { return pathChecker{dataDir: dataDir} }

// Readable reports whether the service can list that path.
func (c pathChecker) Readable(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%s : %w", path, err)
	}
	defer func() { _ = directory.Close() }()

	// One name is enough to prove the listing is allowed, and an empty directory is a
	// legitimate answer: a station that has never received a catalog has one.
	if _, err := directory.Readdirnames(1); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s : %w", path, err)
	}
	return nil
}

// Droppable reports whether the service can really work in that directory.
func (c pathChecker) Droppable(path string) error {
	if c.isArchiveDirectory(path) {
		return fmt.Errorf(
			"%s est le répertoire d'archives de ce poste : il y relirait en boucle les copies "+
				"qu'il vient d'y faire", path)
	}
	info, err := os.Stat(path)
	switch {
	case err != nil:
		return fmt.Errorf(
			"le poste ne trouve pas le répertoire %s. Un service Windows ne voit pas les "+
				`lecteurs réseau montés par une session : écrivez le chemin complet `+
				`(\\serveur\partage\dossier), ou choisissez la source WebDAV`, path)
	case !info.IsDir():
		return fmt.Errorf("%s est un fichier, pas un répertoire", path)
	}

	witness := filepath.Join(path, witnessName)
	if err := os.WriteFile(witness, nil, 0o644); err != nil {
		return fmt.Errorf(
			"le poste peut lire %s mais pas y écrire. Il doit pouvoir y supprimer le fichier "+
				"qu'il vient de lire : c'est ce qui acquitte un import", path)
	}
	if err := os.Remove(witness); err != nil {
		return fmt.Errorf(
			"le poste ne peut pas supprimer un fichier dans %s. Sans cela, il relirait le "+
				"même catalogue indéfiniment", path)
	}
	return nil
}

// isArchiveDirectory compares by INODE and not by string: a path may reach the same
// directory through a symlink, a junction, or a different case on Windows.
func (c pathChecker) isArchiveDirectory(path string) bool {
	archives, err := os.Stat(filepath.Join(c.dataDir, "catalog", "archives"))
	if err != nil {
		return false
	}
	candidate, err := os.Stat(path)
	if err != nil {
		return false
	}
	return os.SameFile(archives, candidate)
}
