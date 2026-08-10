// This file holds WHAT AN IMPORTED FILE IS ALLOWED TO MOVE on the station that reads it.
//
// The export of §11.5 exists to seed a fleet from one reference station. What it must
// never seed is the address a station answers on: that address designates THE MACHINE,
// the way the station number and the two secrets do.

package web

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"openscale/internal/domain"
)

// TestImportingAnExportNeverMovesTheListener is the guard rail that used to be a side
// effect, and stopped being one.
//
// Until the decoder learned to read an empty listen address as the neutral one, an
// export carried from the reference station was refused OUTRIGHT: `Config.Export(false)`
// clears the whole network block, the address came back empty, and control 6 turned the
// whole import into a 422. That refusal was the only thing keeping a clone from moving
// the socket of the station reading it.
//
// The decoder now fills that emptiness in — it has to, or a station installed from such a
// file could never be administered at all — so the file passes the controls, and
// `127.0.0.1:8085` would be applied to a station listening on the whole network: the
// administration screen would close onto a loopback nobody can reach from the shop floor,
// on the one gesture whose whole point is to configure a station remotely.
//
// The two keys go together and the composition root says why (`fallbackProfile`): half a
// network block — an address open to the network behind an administration closed to it —
// is harder to diagnose than either one being wrong on its own.
func TestImportingAnExportNeverMovesTheListener(t *testing.T) {
	const fleetAddress = "0.0.0.0:8085"
	b := adminBench(t, func(o *benchOptions) {
		o.config = func(c *domain.Config) {
			c.Network.Listen = fleetAddress
			c.Network.AdminOnLAN = true
		}
	})

	// The file a volunteer carries from the reference station: the export WITHOUT the
	// hardware, which is the one §11.5 clones with.
	reference := b.hub.Config()
	carried := reference.Export(false)

	proposed, changed := importedConfig(t, b, carried)

	if proposed.Network.Listen != fleetAddress {
		t.Errorf("l'import propose l'adresse d'écoute %q, le poste écoute %q : un fichier "+
			"apporté déplacerait l'écoute de ce poste", proposed.Network.Listen, fleetAddress)
	}
	if !proposed.Network.AdminOnLAN {
		t.Error("l'import referme l'administration sur la boucle locale : le bénévole qui " +
			"a apporté le fichier depuis le réseau perd l'écran en l'enregistrant")
	}
	// And the screen is told nothing moved, or a volunteer would read « le réseau change »
	// on a block the station has just decided to keep.
	if slices.Contains(changed, "network") {
		t.Errorf("l'import annonce le bloc network parmi %v alors qu'il le reprend du poste",
			changed)
	}
}

// TestImportingAFileThatNAMESAnotherAddressStillDoesNotMoveTheListener: the emptiness of
// an export is not what this guard rests on.
//
// A backup taken WITH the hardware carries a real address, and it is the file somebody
// reaches for when a station has to be rebuilt — from the machine next to it, as often as
// not. Deciding the block only when it arrives empty would leave that file free to move
// the socket, which is the same accident by a different door.
func TestImportingAFileThatNAMESAnotherAddressStillDoesNotMoveTheListener(t *testing.T) {
	const fleetAddress = "0.0.0.0:8085"
	b := adminBench(t, func(o *benchOptions) {
		o.config = func(c *domain.Config) {
			c.Network.Listen = fleetAddress
			c.Network.AdminOnLAN = true
		}
	})

	other := b.hub.Config()
	other.Network = domain.NetworkConfig{Listen: "127.0.0.1:8086", AdminOnLAN: false}

	proposed, _ := importedConfig(t, b, other)

	if proposed.Network.Listen != fleetAddress {
		t.Errorf("l'import propose %q, le poste écoute %q", proposed.Network.Listen, fleetAddress)
	}
	if !proposed.Network.AdminOnLAN {
		t.Error("l'import a refermé l'administration sur la boucle locale")
	}
}

// TestImportingStillCarriesTheBusinessSettingsOver keeps the guard above from becoming a
// refusal of everything: what a clone EXISTS to carry must still cross.
func TestImportingStillCarriesTheBusinessSettingsOver(t *testing.T) {
	b := adminBench(t)

	carried := b.hub.Config()
	carried.Limits.MaxWeight = 12_345
	carried.Journal.MaxDays = 30

	proposed, changed := importedConfig(t, b, carried)

	if proposed.Limits.MaxWeight != 12_345 {
		t.Errorf("poids maximum proposé %d, attendu 12345 : le fichier ne passe plus",
			proposed.Limits.MaxWeight)
	}
	if proposed.Journal.MaxDays != 30 {
		t.Errorf("rétention proposée %d jour(s), attendu 30", proposed.Journal.MaxDays)
	}
	if len(changed) == 0 {
		t.Error("l'import ne dit plus ce qui changerait")
	}
}

// importedConfig posts one candidate to the import route and returns what the station
// says it would apply, with the blocks it says would move.
func importedConfig(t *testing.T, b *bench, candidate domain.Config) (domain.Config, []string) {
	t.Helper()
	answer := decodeStatus[struct {
		Config  json.RawMessage `json:"config"`
		Changed []string        `json:"changed_blocks"`
	}](t, b.post("/admin/api/config/import", marshal(t, candidate)), http.StatusOK)

	var proposed domain.Config
	if err := json.Unmarshal(answer.Config, &proposed); err != nil {
		t.Fatalf("configuration proposée illisible : %v", err)
	}
	return proposed, answer.Changed
}
