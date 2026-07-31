package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"

	"openscale/internal/domain"
)

// The two separators that make the fingerprint unambiguous.
//
// Unit and record separators, which no product field can contain: they come out of a
// CSV column, a JSON string or an XML-RPC answer, and none of the three carries a
// control character. Joining on a comma would let « AIL, VIOLET »/« SAF » and
// « AIL »/« VIOLET, SAF » hash to the same catalog.
const (
	unitSeparator   = "\x1f"
	recordSeparator = "\x1e"
)

// Fingerprint is the content identity of a batch nobody could hash as it was read.
//
// A file source hashes the bytes that went past and is done. A source that received
// OBJECTS has no such bytes, and inventing them is a trap: hashing a JSON body makes the
// digest depend on the key order a server chose, on its whitespace and on any field it
// adds that this station ignores. The same catalog would then arrive with a new identity
// every night — « le même catalogue deux fois » would stop being the nominal case of
// §10.5, every poll would rewrite the whole grid under a customer's finger, and the
// quarantine would never see one content refused three times.
//
// So the digest is computed over the PRODUCTS, in an order this function imposes rather
// than one a producer chose. What goes into it is what a customer or a till would notice
// changing; what stays out is everything this binary derives from those products.
// Findings, counts and photos are consequences, and a French sentence reworded in a
// release must not make four stations believe their catalog changed overnight.
//
// A source is free not to use it: an ERP that publishes a write-date or an ETag it can be
// held to has a better answer, because it identifies the content WITHOUT reading it all.
func Fingerprint(products []domain.Product) string {
	lines := make([]string, 0, len(products))
	for _, p := range products {
		lines = append(lines, strings.Join([]string{
			p.ID,
			p.Name,
			string(p.Reference),
			p.Mode.String(),
			strconv.FormatInt(int64(p.UnitPrice), 10),
			p.PriceSuffix,
			p.CategoryCode,
			p.Qualification.String(),
			p.Reason,
			p.ImageSHA,
		}, unitSeparator))
	}
	// Sorted, so that a producer who paginates differently on Tuesday does not publish a
	// different catalog. The line begins with the id, which §10.9 guarantees unique, so
	// the order is total and two runs cannot disagree.
	sort.Strings(lines)

	hash := sha256.New()
	for _, line := range lines {
		hash.Write([]byte(line))
		hash.Write([]byte(recordSeparator))
	}
	return hex.EncodeToString(hash.Sum(nil))
}
