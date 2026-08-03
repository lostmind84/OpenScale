package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// This file holds the eight characters a volunteer compares by eye, and nothing
// else: the digest of one configuration block, and the digest of a whole station.

// fingerprintLength is how many hexadecimal characters the dashboard shows.
//
// Eight is what makes "do the four stations display the same string?" a check
// anybody can do by eye -- which the 227 _Poste1..4 columns of the legacy
// application never allowed.
const fingerprintLength = 8

// BlockFingerprint reports the SHA-256 of the canonical JSON of one configuration
// block, as eight hexadecimal characters.
//
// It is what Station.Reload compares to decide whether a block REALLY changed
// (§11.4): a normalised comparison and not reflect.DeepEqual over raw JSON, so that
// a reformatted file does not close the serial port under a customer.
func BlockFingerprint(block any) string {
	canonical, err := CanonicalJSON(block)
	if err != nil {
		return strings.Repeat("?", fingerprintLength)
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])[:fingerprintLength]
}

// Fingerprint reports the eight characters the dashboard shows, so that "do the
// four stations display the same string?" is a check anybody can do by eye (§11.5).
//
// IT IS COMPUTED ON THE HARDWARE-FREE VIEW, Export(false), with modified_at and
// _readme cleared -- and it has to be, otherwise it could never do the one job it
// exists for: four stations of one homogeneous fleet differ by their number, their
// name, their COM port and their print queue, and each file was written at a
// different instant. What the figure compares is what MUST be identical: the price
// grid, the safeguards, the template, the categories, the retention -- and, since the
// export stopped dropping the three option maps whole, the label offset, the
// darkness, the speed, the serial settings of the scale and the import guards of the
// catalog.
//
// That second half of the list was never decided HERE, and it must not be: the
// fingerprint FOLLOWS the export, because what two stations must have in common is
// exactly what a clone carries over, and one definition of that is worth more than
// two that drift apart. Widening what travels widens the digest with it -- a station
// whose darkness alone was raised now shows a different string, and it should: it
// does not print like the other three.
func (c *Config) Fingerprint() string {
	subject := c.Export(false)
	subject.ModifiedAt, subject.Readme = time.Time{}, ""
	return BlockFingerprint(subject)
}
