package station

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// This file answers the one question a reload asks of every configuration block: did
// this block MOVE, or was it only serialized differently?

// BlockFingerprint is the SHA-256 of the CANONICAL JSON of one configuration
// block, in eight hexadecimal characters.
//
// Canonical means: keys sorted, no spaces, numbers re-read literally. That is what
// makes the comparison semantic — two files that differ only by their key order
// must not cut a serial port in the middle of a service — and it is also what the
// administration screen shows to answer « quels blocs ont bougé ? ».
func BlockFingerprint(block any) string {
	raw, err := json.Marshal(block)
	if err != nil {
		// A block that cannot be serialized cannot be compared either. Returning a
		// value that is never equal to anything makes the change VISIBLE, which is
		// the safe direction: a restart too many beats a port left on a stale
		// setting.
		return fmt.Sprintf("unmarshalable-%p", block)
	}
	canonical, err := canonicalJSON(raw)
	if err != nil {
		return fmt.Sprintf("unmarshalable-%p", block)
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:4])
}

// canonicalJSON re-reads and re-writes a JSON document so that two semantically
// identical documents produce the same bytes.
//
// Numbers go through json.Number, so 1605 stays 1605 and does not become 1.605e3
// through a float64 — a fingerprint that changes with a serialization detail is a
// fingerprint that restarts hardware for nothing.
func canonicalJSON(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}
