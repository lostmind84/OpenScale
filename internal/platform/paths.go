package platform

import (
	"path/filepath"
	"runtime"
)

// The default locations of §11.1, and the ONLY place in the repository that spells
// them.
//
// « Aucun chemin en dur dans le code » is the sentence of §11.1 that follows the
// table, and it is not a contradiction of the table: a default has to exist
// somewhere, and what the rule forbids is a second place. Everything else — the
// station, the store, the web layer, the drivers — receives a path.
//
// THE NAME OF THE PRODUCT IS `openscale` AND NOT `Balance`. §11.1 still writes
// C:\ProgramData\Balance and /var/lib/openscale in the same table, which is the
// rename of 25/07/2026 caught half-way; docs/03-glossaire.md, which is the naming
// authority, renames every one of them — openscale.db, openscale.log,
// openscale-kiosk.xml, /etc/openscale, /var/lib/openscale — and internal/store
// already writes openscale.db. The Windows directory follows.
const (
	// DatabaseName is the file the station's journal, catalog and counters live in.
	DatabaseName = "openscale.db"
	// ConfigName is the configuration file of §11.1, next to its five rotating
	// versions config.json.1 … .5.
	ConfigName = "config.json"
	// KioskLogName is where the browser supervisor's French lines are kept, next to
	// its own previous generation kiosk.log.1.
	KioskLogName = "kiosk.log"

	windowsRoot = `C:\ProgramData\OpenScale`
	linuxConfig = "/etc/openscale"
	linuxData   = "/var/lib/openscale"
)

// DefaultConfigPath reports where the configuration file lives when neither --config
// nor OPENSCALE_CONFIG says otherwise.
func DefaultConfigPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(windowsRoot, ConfigName)
	}
	return filepath.Join(linuxConfig, ConfigName)
}

// DefaultDataDir reports where the database, the images and the captured labels live
// when neither --data nor OPENSCALE_DATA says otherwise.
//
// It is a DIFFERENT directory from the configuration on Linux and a subdirectory of
// the same root on Windows, and that follows §11.1 rather than a preference: an
// administrator backs up /etc and /var separately, and the update procedure of §15.5
// rests on the configuration and the database NOT living beside the binary.
func DefaultDataDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(windowsRoot, "data")
	}
	return linuxData
}

// DefaultKioskLogPath reports where the browser supervisor writes what it did.
//
// Beside the configuration and NOT inside the data directory: the kiosk runs as the
// unprivileged station account, this file is a diagnostic aid rather than data of the
// station, and putting it next to the database would put the two in the same disk-space
// alert (§10.4). On Windows the installer already grants that directory to the account;
// on Linux the unit's stdout goes to the journal as well, and this file is the copy that
// survives a journal nobody kept.
func DefaultKioskLogPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(windowsRoot, KioskLogName)
	}
	return filepath.Join(linuxData, KioskLogName)
}

// DatabasePath reports the database file inside a data directory.
func DatabasePath(dataDir string) string { return filepath.Join(dataDir, DatabaseName) }
