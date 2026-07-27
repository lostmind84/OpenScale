package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"openscale/internal/domain"
)

// KeptVersions is how many previous versions of config.json stay on disk:
// config.json.1 … config.json.5 (§11.1, §11.4).
//
// Five, and the fifth is the one that gets dropped rather than a sixth being kept: the
// point of the rotation is « revenir à hier soir », not an archive. What holds the
// history of what a station was configured with is the fingerprint on the dashboard.
const KeptVersions = 5

// ConfigVersion is one restorable version of the file.
type ConfigVersion struct {
	// Version is 1 for the most recent backup, up to KeptVersions for the oldest.
	Version int
	// ModifiedAt is when that version was in force. It comes from the modified_at the
	// file carries, and falls back on the modification time of the file itself when the
	// backup predates that field.
	ModifiedAt time.Time
	// Fingerprint is the eight-character digest the administration screen shows, so that
	// « la version 2, c'est celle d'avant-hier » is decidable by eye.
	Fingerprint string
}

// ConfigStore is config.json on disk: the atomic write of §11.4 and the five rotating
// versions of §11.1.
//
// It is what makes the administration screen able to save. Everything about the CONTENT
// of a configuration — the 45 controls, the fingerprint, the export — belongs to
// domain.Config; this type only knows how to put bytes on a disk without ever leaving a
// station with half a configuration.
type ConfigStore struct {
	path string

	// rename is os.Rename, and it is a field for ONE reason: « the write was
	// interrupted between the temporary file and the rename » is the failure this whole
	// type exists to survive, and no portable test can produce it — a power cut is not
	// something `go test` arranges. The same seam exists for the same reason in
	// localdrop.Source.remove and in printing/transport/file.go.
	rename func(from, to string) error
}

// NewConfigStore opens the store over the file a station reads its configuration from.
//
// It creates nothing: the directory is laid down by the installer (§15.2, §15.3), and a
// service that created C:\ProgramData\Balance itself would hide a path typed wrong in a
// service unit behind a configuration that appeared out of nowhere.
func NewConfigStore(path string) (*ConfigStore, error) {
	if path == "" {
		return nil, errors.New("platform : aucun chemin de configuration n'est fourni")
	}
	return &ConfigStore{path: path, rename: os.Rename}, nil
}

// Path reports the file this store writes, which the administration screen displays on
// the Poste page (§14.4).
func (s *ConfigStore) Path() string { return s.path }

// Read returns the configuration as it stands ON DISK, which is not always the one in
// force: a station that fell back to manual entry, or that started on the neutral
// profile because the file was invalid, runs something else in memory (§11.3, §11.4).
//
// That difference is the whole reason this method exists: « ce que l'exploitant a
// demandé » is what a volunteer coming back from manual entry has to be given back.
func (s *ConfigStore) Read(_ context.Context) (domain.Config, error) {
	return readConfigFile(s.path)
}

// Save rotates the versions and writes the file ATOMICALLY (§11.4, steps 3 and 4).
//
// The order is the guarantee, and it is worth reading in the order it happens:
//
//  1. the document is serialised FIRST, so a configuration that cannot be marshalled
//     never touches the disk at all;
//  2. the bytes go into a temporary file IN THE SAME DIRECTORY, then fsync — a rename
//     across directories is not atomic, and on Windows it is not even the same call;
//  3. the versions rotate, .4 → .5 down to .1 → .2, and the file in force is COPIED to
//     .1 — copied and not renamed, so that config.json NEVER stops existing;
//  4. the temporary file is renamed over config.json, which is the one instant the
//     content changes, and it changes whole;
//  5. the directory is flushed, where the platform allows it.
//
// Interrupted anywhere before step 4, the configuration in force is untouched — that is
// the test that matters, and it is written. Interrupted between 3 and 4, config.json.1
// holds a copy of the configuration still in force, which is harmless and honest.
func (s *ConfigStore) Save(_ context.Context, cfg domain.Config) error {
	// This is where a Config becomes a file, which makes it the ONE place a retired
	// key can be stopped for every caller at once -- the ones already careful about
	// it (writeConfig, config.go) and the ones that never will be, because checking
	// first is not always possible: recoverSession reads whatever the file already
	// holds and cannot refuse to save on a rescue without also refusing the rescue.
	// Marshalling cfg is what LAUNDERS a retired key: encoding/json already dropped
	// it once, at decode, and with coef_num goes the discount it stood for -- the
	// file that comes out the other end decodes clean, control 20 finds nothing on
	// the next read, and every member pays full price with nothing on any screen to
	// say why (ADR-034).
	if err := cfg.RefuseIfRetired(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("configuration non sérialisable : %w", err)
	}
	// A JSON document ends with a newline: the file is read by human beings with a text
	// editor when everything else has failed.
	raw = append(raw, '\n')

	directory := filepath.Dir(s.path)
	tmp, err := writeTemporary(directory, raw)
	if err != nil {
		return err
	}
	// The temporary file is removed on EVERY path that does not rename it, including the
	// one where the rename itself fails: a directory slowly filling with config-*.json
	// is how a volunteer ends up unable to tell which file the station reads.
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tmp)
		}
	}()

	if err := s.rotate(); err != nil {
		return err
	}
	if err := s.rename(tmp, s.path); err != nil {
		return fmt.Errorf("configuration non remplacée (%s reste en service) : %w", s.path, err)
	}
	renamed = true
	return syncDirectory(directory)
}

// Versions lists the restorable versions, most recent first (§14.4, page Poste).
//
// A backup that cannot be parsed is LISTED, with no fingerprint: hiding it would make
// the screen say « 4 versions » about a directory holding five files, and the operator
// would restore the wrong one.
func (s *ConfigStore) Versions(_ context.Context) ([]ConfigVersion, error) {
	out := make([]ConfigVersion, 0, KeptVersions)
	for version := 1; version <= KeptVersions; version++ {
		path := s.versionPath(version)
		info, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("version %d illisible : %w", version, err)
		}
		entry := ConfigVersion{Version: version, ModifiedAt: info.ModTime()}
		if cfg, err := readConfigFile(path); err == nil {
			entry.Fingerprint = cfg.Fingerprint()
			if !cfg.ModifiedAt.IsZero() {
				entry.ModifiedAt = cfg.ModifiedAt
			}
		}
		out = append(out, entry)
	}
	return out, nil
}

// Restore reads one version back WITHOUT applying it.
//
// Applying is the caller's business, and it goes through the same validation and the
// same atomic write as any other save (§11.4): a version that was legitimate last month
// may name a print queue this station no longer has, and it must come back with the
// list of faults rather than take a station out of service.
func (s *ConfigStore) Restore(_ context.Context, version int) (domain.Config, error) {
	if version < 1 || version > KeptVersions {
		return domain.Config{}, fmt.Errorf(
			"version %d : les versions restaurables vont de 1 (la plus récente) à %d",
			version, KeptVersions)
	}
	return readConfigFile(s.versionPath(version))
}

// rotate shifts the backups by one and copies the file in force onto .1.
//
// It tolerates a missing file at every rank, and it has to: a station saved for the
// first time has no .1, and one saved three times has no .4. A rotation that refused
// there would make the second save of a fresh installation impossible.
func (s *ConfigStore) rotate() error {
	for version := KeptVersions - 1; version >= 1; version-- {
		from, to := s.versionPath(version), s.versionPath(version+1)
		if err := s.rename(from, to); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("rotation de %s vers %s : %w", from, to, err)
		}
	}
	// COPY, never rename: config.json has to exist at every instant of this function,
	// because the station reads it on the next start and a volunteer reads it when
	// nothing else works. A rename would open a window — short, real, and exactly the
	// one this whole file is written to avoid — where the station has no configuration
	// at all.
	current, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		// A first save: there is no previous version, and that is not a failure.
		return nil
	}
	if err != nil {
		return fmt.Errorf("configuration en service illisible (%s) : %w", s.path, err)
	}
	tmp, err := writeTemporary(filepath.Dir(s.path), current)
	if err != nil {
		return err
	}
	if err := s.rename(tmp, s.versionPath(1)); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("version précédente non conservée : %w", err)
	}
	return nil
}

// versionPath is config.json.<n>, the spelling §11.1 tabulates.
func (s *ConfigStore) versionPath(version int) string {
	return fmt.Sprintf("%s.%d", s.path, version)
}

// writeTemporary puts the bytes in a file of the target DIRECTORY and flushes them to
// the device before returning its name.
//
// The fsync is not decoration: without it the rename can reach the disk before the
// content does, and a power cut then leaves a config.json that exists, is named
// correctly, and is empty — which is worse than the old one, because it looks valid.
func writeTemporary(directory string, content []byte) (string, error) {
	file, err := os.CreateTemp(directory, "config-*.json")
	if err != nil {
		return "", fmt.Errorf("fichier temporaire dans %s : %w", directory, err)
	}
	name := file.Name()
	if _, err := file.Write(content); err != nil {
		file.Close()
		os.Remove(name)
		return "", fmt.Errorf("écriture de %s : %w", name, err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		os.Remove(name)
		return "", fmt.Errorf("vidage de %s sur le disque : %w", name, err)
	}
	if err := file.Close(); err != nil {
		os.Remove(name)
		return "", fmt.Errorf("fermeture de %s : %w", name, err)
	}
	return name, nil
}

// readConfigFile reads and parses one configuration file, and says which file it is
// talking about when it cannot.
//
// The path is in the message on purpose: a station has six of these files, and « JSON
// invalide » without a name sends a volunteer to the wrong one.
func readConfigFile(path string) (domain.Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return domain.Config{}, fmt.Errorf("%s ne peut pas être lu : %w", path, err)
	}
	var cfg domain.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return domain.Config{}, fmt.Errorf("%s n'est pas un JSON exploitable : %w", path, err)
	}
	return cfg, nil
}
