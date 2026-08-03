package domain

import (
	"bytes"
	"encoding/json"
	"strings"
)

// This file holds WHAT NEVER LEAVES THE STATION, and the walk that takes it out:
// the option names that carry a secret, the ones that designate one station or one
// site, and Export, which is the only door they are checked at.

// secretOptionKeys names the option keys whose VALUE never leaves the station, in ANY
// of the three option maps, at ANY depth, in BOTH modes of includeHardware.
//
// It is the list internal/diag/redact.go redacts by, and it is deliberately the SAME
// list: an export and a diagnostic archive are two doors to the outside, and two doors
// must not have two levels of rigour. redact.go owns the reason the match is on the NAME
// and not on a path -- « a driver option added in two years and called `token` is caught
// without anybody remembering to come back here » -- and it now reads this list instead
// of keeping its own copy. The list lives HERE, in the package that depends on nothing,
// because that is the only direction the dependency can go: internal/diag imports
// internal/domain, never the reverse (§5.2).
//
// What is NOT in it, and must not be: `url`. redact.go removes an address because an
// archive is handed to whoever offers to help, and the private host of a cooperative is
// not ours to publish. A catalog URL is not a secret, it designates a SITE -- so it is
// stationSpecificOptions that names it, and a HARDWARE export, which is the backup of one
// station, legitimately keeps it.
var secretOptionKeys = map[string]bool{
	"password":           true,
	"password_hash":      true,
	"recovery_code_hash": true,
	"passphrase":         true,
	"secret":             true,
	"token":              true,
	"api_key":            true,
	"apikey":             true,
	"credential":         true,
	"credentials":        true,
	"private_key":        true,
}

// IsSecretOptionKey reports whether a driver option under this name carries a secret.
//
// Exported so that the archive redacts exactly what the export refuses to carry. The
// match is case-insensitive: a file written by hand may well spell `Password`, and a
// secret that leaves because of a capital letter is still a secret that left.
func IsSecretOptionKey(key string) bool {
	return secretOptionKeys[strings.ToLower(key)]
}

// stationSpecificOptions names the driver option keys an export must not carry when
// it is meant to seed ANOTHER station.
//
// Everything else in the three option maps travels, and that default is deliberate: a
// driver option is a setting the parc SHARES until somebody proves otherwise, and the
// proof is written here. Dropping the maps whole was the opposite default, and it made
// INSTALLATION.md lie -- it promises the label offset travels with the cloned
// configuration, and printer.options went out with it.
//
// Two kinds of key are named, and only those two: what designates ONE station (a
// serial port, a Windows queue), and what designates ONE SITE's infrastructure (a
// host, an account, a path). A value that is neither belongs to the parc.
//
// It names KEYS OF A MAP, and it can name nothing else. A site value that lives in a
// TYPED field -- catalog.images.path -- is out of reach of withoutKeys and is dropped
// by Export itself; that is where to look before adding a name here.
//
// It names a key and never a PATH: each list applies to its whole option tree, so a
// serial port under `gateway` and a print queue under `fallback.deeper` go the same way
// as the ones at the first level. The previous version named the group « fallback » in
// the code, which meant one nested object out of all the ones a driver may declare.
var stationSpecificOptions = struct {
	scale   []string
	printer []string
	catalog []string
}{
	// COM8 on this station, something else on the next one.
	scale: []string{"port"},
	// A Windows queue name differs per machine: the « _2 » of « SATO WS408_2 » is a
	// duplicate suffix Windows added, measured on PC-RECEPTION. And `address` is a
	// HOST -- 192.168.0.43:9100 on the bench -- which this repository never ships
	// (docs/00-donnees-retirees.md).
	printer: []string{"queue", "address", "path"},
	// The share and the account belong to one site. The password leaves in NO mode,
	// and that is handled before this list, unconditionally.
	catalog: []string{"url", "username", "directory"},
}

// oneOf reports the membership test of a strip list, in the shape withoutKeys takes.
func oneOf(keys []string) func(string) bool {
	return func(key string) bool { return known(keys, key) }
}

// withoutKeys returns the options minus every key drop names, AT ANY DEPTH.
//
// Depth is the whole point. An option map is free-form -- the administration screen
// builds its form from the schema the DRIVER declares (§9.3) -- so a driver is free to
// nest a gateway, a proxy or a second fallback under any name it invents, and a strip
// that only visited the ground floor let a password walk out from the first. Nothing
// here names a group: only leaf keys are named, and every object is visited.
//
// An absent block stays absent: returning an empty map where there was none would
// turn « ce poste ne déclare pas d'imprimante » into « ce poste déclare une
// imprimante sans rien dedans », which validates differently.
func withoutKeys(options DriverOptions, drop func(key string) bool) DriverOptions {
	if options == nil {
		return nil
	}
	out := options.clone()
	for key, raw := range options {
		if drop(key) {
			delete(out, key)
			continue
		}
		if stripped, changed := strippedValue(raw, drop); changed {
			out[key] = stripped
		}
	}
	return out
}

// strippedValue returns one raw option value minus what drop names anywhere inside it,
// and whether anything moved.
//
// The « whether » is not a convenience: re-encoding a value reorders its keys and drops
// the whitespace the file spelled it with, so an untouched value is handed back BYTE FOR
// BYTE instead of being rewritten. A value that does not decode is left alone rather than
// dropped, for the same reason a malformed group used to be: hiding it would send the
// operator looking for a key the file still carries.
//
// It walks a generic tree, and it duplicates fifteen lines of internal/diag's redactTree
// on purpose, because the two cannot be one function. That one REPLACES a value with a
// visible marker and keeps the key, so a reader can tell « ce poste n'a pas de mot de
// passe » from « le mot de passe a été retiré » ; this one DELETES the key, because an
// export is merged field by field into a target (§11.5) and a marker would overwrite the
// target's own secret with the word « [caviardé] ». What the two do share is the thing
// that rots -- the list of names -- and secretOptionKeys is where they share it.
func strippedValue(raw json.RawMessage, drop func(key string) bool) (json.RawMessage, bool) {
	// UseNumber, so that a baud rate re-encodes as 9600 and never as 9.6e+03: decoding
	// a number into `any` yields a float64, and no float carries a quantity here.
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var node any
	if err := decoder.Decode(&node); err != nil {
		return raw, false
	}
	stripped, changed := strippedTree(node, drop)
	if !changed {
		return raw, false
	}
	encoded, err := json.Marshal(stripped)
	if err != nil {
		return raw, false
	}
	return encoded, true
}

// strippedTree walks a decoded JSON value and removes every member drop names.
//
// Lists are walked too, and that is not zeal: a driver that declares its mirrors as a
// list of objects puts one set of credentials per entry, and a walk that only knew about
// objects would ship all of them.
func strippedTree(node any, drop func(key string) bool) (any, bool) {
	switch value := node.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		changed := false
		for key, child := range value {
			if drop(key) {
				changed = true
				continue
			}
			stripped, touched := strippedTree(child, drop)
			out[key] = stripped
			changed = changed || touched
		}
		return out, changed
	case []any:
		out := make([]any, len(value))
		changed := false
		for i, child := range value {
			stripped, touched := strippedTree(child, drop)
			out[i] = stripped
			changed = changed || touched
		}
		return out, changed
	}
	return node, false
}

// Export returns a copy of the configuration fit to leave the station.
//
// With includeHardware false it drops station.number, station.name, network, the
// admin fingerprints, catalog.images.path and the option keys of
// stationSpecificOptions -- a serial port, a print queue, a host, an account, a
// path. What is left is what four stations of one fleet share, and it is what "clone
// a station" copies (§11.5).
//
// NO SECRET EVER LEAVES, whatever includeHardware says: the admin password, and every
// option secretOptionKeys names, in the three option maps and at any depth. On import a
// station without a password runs the "first access" journey, which IMPOSES setting one
// -- an exported password would turn a fleet into four stations sharing one secret
// nobody chose. The promise used to be written as "two secrets" and enforced by one
// delete on one key of one map: a password under scale.options.gateway went out in clear
// text, and so did anything a driver called `token`.
//
// The result is NOT a loadable configuration: without a station number it fails
// control 1. It is meant to be MERGED into a target, field by field, with the diff
// preview of §11.5.
func (c *Config) Export(includeHardware bool) Config {
	out := *c
	out.retired = nil
	out.Admin.PasswordHash = ""

	out.Scale.Options = withoutKeys(c.Scale.Options, IsSecretOptionKey)
	out.Printer.Options = withoutKeys(c.Printer.Options, IsSecretOptionKey)
	out.Catalog.Options = withoutKeys(c.Catalog.Options, IsSecretOptionKey)

	// Copies, so that a caller editing the export cannot reach into the
	// configuration the station is running on.
	out.Pricing.Tiers = append([]PriceTier(nil), c.Pricing.Tiers...)
	out.Pricing.SecondaryCodes = append([]string(nil), c.Pricing.SecondaryCodes...)
	out.Catalog.Categories = append([]Category(nil), c.Catalog.Categories...)

	if includeHardware {
		return out
	}
	out.Station.Number, out.Station.Name = 0, ""
	out.Network = NetworkConfig{}
	out.Admin.RecoveryCodeHash = ""
	out.Scale.Options = withoutKeys(out.Scale.Options, oneOf(stationSpecificOptions.scale))
	out.Printer.Options = withoutKeys(out.Printer.Options, oneOf(stationSpecificOptions.printer))
	out.Catalog.Options = withoutKeys(out.Catalog.Options, oneOf(stationSpecificOptions.catalog))
	// catalog.images.path designates ONE SITE just as catalog.options.url does -- a
	// share on the NAS, a letter mapped on this machine -- and it left with the export
	// for as long as it existed, because the strip list only knows how to delete a KEY
	// and this is a FIELD. images.source stays: "the pictures come with the CSV" is an
	// answer the whole fleet shares, and a clone that lost it would fall back on the
	// names of the products.
	out.Catalog.Images.Path = ""
	return out
}
