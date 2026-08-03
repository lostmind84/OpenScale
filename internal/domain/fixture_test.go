// This file holds what EVERY configuration test starts from: the delivered file
// itself, the driver registries a running binary would carry, and the four helpers
// that read a fault back.
//
// The delivered configuration is READ from testdata rather than reproduced in Go:
// reproducing it would reintroduce exactly the second source of truth ADR-026
// removes.

package domain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// deliveredConfigPath is the file lot L9 ships and the installer copies. It is read
// from the tests rather than reproduced in Go, because reproducing it would
// reintroduce exactly the second source of truth ADR-026 removes.
var deliveredConfigPath = filepath.Join("..", "..", "testdata", "config-lacagette.json")

// loadDelivered returns a FRESH copy of the delivered configuration.
//
// Fresh for every case, and it matters: DriverOptions is a map, so a struct copy
// would let one broken case leak its mutation into the next one.
func loadDelivered(t *testing.T) Config {
	t.Helper()
	raw, err := os.ReadFile(deliveredConfigPath)
	if err != nil {
		t.Fatalf("lecture de %s : %v", deliveredConfigPath, err)
	}
	var config Config
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("décodage de %s : %v", deliveredConfigPath, err)
	}
	return config
}

// testRegistries declares the drivers the shipped configuration names, with the
// schema each of them would declare.
//
// The bounds of the options a NUMBERED CONTROL already owns -- roll_capacity, the two
// offsets, poll_interval_s, the two ratios, the two size ceilings -- are deliberately
// left open here: the control names the field and the reason, and declaring the same
// bound twice would report one mistake as two faults.
//
// `copies` is the exception, and it is the one option whose bound NO control owns any
// more: it belongs to the driver, raster.MaxConfiguredCopies, and the schema is the only
// place it is stated. What is declared here is that same [1, 10], as the real driver
// declares it -- a bound left open here would make control 7 look toothless on the one
// key it is now solely responsible for.
func testRegistries() Registries {
	serial := []OptionSchema{
		{Key: "port", Kind: OptionText, Required: true},
		{Key: "baud", Kind: OptionInt},
		{Key: "bits", Kind: OptionInt},
		{Key: "parity", Kind: OptionEnum, Values: []string{"N", "E", "O"}},
		{Key: "stop", Kind: OptionInt},
		{Key: "backoff_min_ms", Kind: OptionInt},
		{Key: "backoff_max_ms", Kind: OptionInt},
	}
	printerOptions := []OptionSchema{
		{Key: "transport", Kind: OptionEnum, Values: []string{
			TransportWinspool, TransportDevfile, TransportTCP, TransportFile}},
		{Key: "queue", Kind: OptionText},
		{Key: "path", Kind: OptionText},
		{Key: "address", Kind: OptionHostPort},
		{Key: "fallback", Kind: OptionGroup, Options: []OptionSchema{
			{Key: "enabled", Kind: OptionBool},
			{Key: "transport", Kind: OptionEnum, Values: []string{
				TransportWinspool, TransportDevfile, TransportTCP, TransportFile}},
			{Key: "queue", Kind: OptionText},
		}},
		{Key: "darkness", Kind: OptionInt},
		{Key: "speed", Kind: OptionInt},
		{Key: "offset_x", Kind: OptionInt},
		{Key: "offset_y", Kind: OptionInt},
		{Key: "invert_bits", Kind: OptionBool},
		{Key: "copies", Kind: OptionInt, Min: 1, Max: 10},
		{Key: "roll_capacity", Kind: OptionInt},
	}
	commonCatalog := []OptionSchema{
		{Key: "separator", Kind: OptionText},
		{Key: "poll_interval_s", Kind: OptionInt},
		{Key: "stable_polls", Kind: OptionInt},
		{Key: "max_file_size_mb", Kind: OptionInt},
		{Key: "max_image_size_kb", Kind: OptionInt},
		{Key: "min_readable_ratio", Kind: OptionRatio},
		{Key: "max_weighable_drop", Kind: OptionRatio},
		{Key: "max_archives", Kind: OptionInt},
		{Key: "archive_days", Kind: OptionInt},
		{Key: "failures_before_reject", Kind: OptionInt},
	}
	webdav := append([]OptionSchema{
		{Key: "url", Kind: OptionURL, Required: true},
		{Key: "username", Kind: OptionText},
		{Key: "password", Kind: OptionText},
	}, commonCatalog...)
	// `directory` belongs to local_drop ALONE, exactly as the real descriptor declares
	// it: it is the one source that watches a directory of this machine. Declaring it
	// here is what makes control 46 the only voice on that field -- an undeclared key
	// would already be refused by control 9, and the case would prove nothing.
	localDrop := append([]OptionSchema{
		{Key: "directory", Kind: OptionText, Use: UseDropDirectory},
	}, commonCatalog...)

	return Registries{
		Scales: []DriverDescriptor{
			{ID: "gram-xfoc-rs", Label: "GRAM XFOC RS", Options: serial},
			{ID: "gram-xfoc-plus", Label: "GRAM XFOC +", Options: serial},
		},
		Printers: []DriverDescriptor{
			// The raster driver declares the head of the parc, exactly as
			// cmd/openscale does: it is what rules 3 and 4 measure a template against.
			// `preview` declares nothing, because it inks no paper.
			{ID: PrinterRaster, Label: "Raster", Options: printerOptions, Capabilities: ReferenceHead()},
			{ID: PrinterSBPL, Label: "SBPL", Options: printerOptions, Capabilities: ReferenceHead()},
			{ID: PrinterPreview, Label: "Aperçu"},
		},
		Transports: []DriverDescriptor{
			{ID: TransportWinspool, Label: "file Windows"},
			{ID: TransportDevfile, Label: "nœud d'impression"},
			{ID: TransportTCP, Label: "imprimante réseau"},
			{ID: TransportFile, Label: "fichier"},
		},
		CatalogSources: []DriverDescriptor{
			{ID: CatalogSourceLocalDrop, Label: "répertoire de dépôt", Options: localDrop},
			{ID: CatalogSourceWebDAV, Label: "partage WebDAV", Options: webdav},
		},
	}
}

// unreadablePaths is the PathChecker of a service that cannot see a path.
type unreadablePaths struct{}

func (unreadablePaths) Readable(string) error  { return fmt.Errorf("accès refusé") }
func (unreadablePaths) Droppable(string) error { return fmt.Errorf("accès refusé") }

// setOption writes one driver option the way a file would carry it.
func setOption(t *testing.T, options DriverOptions, key string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encodage de l'option %s : %v", key, err)
	}
	options[key] = raw
}

// fieldsOf reports the faulty fields, for a failure message that names them all.
func fieldsOf(faults []Fault) []string {
	out := make([]string, 0, len(faults))
	for _, fault := range faults {
		out = append(out, fault.String())
	}
	return out
}

// findFault returns the first fault on a field, or nil.
func findFault(faults []Fault, field string) *Fault {
	for i := range faults {
		if faults[i].Field == field {
			return &faults[i]
		}
	}
	return nil
}
