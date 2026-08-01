package platform

import (
	"fmt"
	"os"

	"openscale/internal/domain"
)

// LoadConfig reads config.json, brings it up to the schema this binary speaks, and reports
// everything it had to do to get there.
//
// It is the ONLY place the bytes of a configuration file become a domain.Config. There
// used to be four -- serve, this package, `openscale doctor` and `openscale config` -- and
// that duplication is what let the defect of 01/08/2026 exist: a guard rail put in one of
// them left the other three open.
//
// The error is reserved for "there is no readable FILE at that path", which stays fatal
// for the reason NewConfigStore gives: a wrong path in a service unit must not hide behind
// a configuration that appeared out of nowhere. Everything else -- a truncated document, an
// undecodable block, a key this binary refuses -- comes back as faults, which is what puts
// it on the ERR-CFG-01 path of §11.3 instead of killing the process.
//
// It NEVER writes. `openscale config migrate` is what fixes the file, and it is called by
// update.ps1 and update.sh once the station has answered.
func LoadConfig(path string) (domain.Config, []domain.MigrationNote, []domain.Fault, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return domain.Config{}, nil, nil, fmt.Errorf("%s ne peut pas être lu : %w", path, err)
	}

	migrated, notes, err := domain.Migrate(raw)
	if err != nil {
		// The document is not an object at all. Decoding it block by block says so in the
		// French a volunteer can act on, and names config.json.1.
		cfg, faults := domain.DecodeConfigBlockByBlock(raw)
		return cfg, notes, faults, nil
	}

	cfg, faults := domain.DecodeConfigBlockByBlock(migrated)
	return cfg, notes, faults, nil
}
