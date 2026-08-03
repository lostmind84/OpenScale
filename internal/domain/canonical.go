package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// This file holds the ONE spelling of a JSON value: keys sorted, no whitespace,
// whole numbers in plain decimal.
//
// It exists for the fingerprint (fingerprint.go), which is the only thing that
// needs two semantically identical configurations to produce the same bytes -- but
// nothing here knows about a configuration, and that is deliberate: canonicalising
// is a property of JSON, not of what the JSON happens to describe.

// CanonicalJSON returns the canonical JSON of a value: keys sorted, no whitespace,
// whole numbers in plain decimal.
//
// Canonical and not merely compact, and that is the point of §11.4: two
// configurations that are semantically identical but serialised with a different
// key order must NOT cut the serial port in the middle of a service. Whole numbers
// are re-emitted in decimal so that 9600 and 9.6e3 -- two spellings of the same
// baud rate -- cannot produce two fingerprints.
func CanonicalJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var generic any
	if err := decoder.Decode(&generic); err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	if err := writeCanonical(&buffer, generic); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// writeCanonical writes one decoded JSON value in canonical form.
func writeCanonical(buffer *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case nil:
		buffer.WriteString("null")
	case bool:
		if typed {
			buffer.WriteString("true")
		} else {
			buffer.WriteString("false")
		}
	case json.Number:
		buffer.WriteString(canonicalNumber(typed))
	case string:
		writeJSONString(buffer, typed)
	case []any:
		buffer.WriteByte('[')
		for i, item := range typed {
			if i > 0 {
				buffer.WriteByte(',')
			}
			if err := writeCanonical(buffer, item); err != nil {
				return err
			}
		}
		buffer.WriteByte(']')
	case map[string]any:
		buffer.WriteByte('{')
		for i, key := range sortedKeys(typed) {
			if i > 0 {
				buffer.WriteByte(',')
			}
			writeJSONString(buffer, key)
			buffer.WriteByte(':')
			if err := writeCanonical(buffer, typed[key]); err != nil {
				return err
			}
		}
		buffer.WriteByte('}')
	default:
		return fmt.Errorf("domain: valeur JSON non canonisable de type %T", value)
	}
	return nil
}

// writeJSONString writes one quoted JSON string.
//
// encoding/json cannot fail on a string -- invalid UTF-8 is replaced by U+FFFD, not
// rejected -- so there is no error to propagate, and no unreachable branch is left
// in a function the fingerprint of every configuration goes through.
func writeJSONString(buffer *bytes.Buffer, value string) {
	encoded, _ := json.Marshal(value)
	buffer.Write(encoded)
}

// exactIntegerFloat is 2^53, beyond which a float64 no longer holds every integer.
const exactIntegerFloat = 1 << 53

// canonicalNumber reports the one spelling of a JSON number.
//
// It exists so that 9600, 9.6e3 and 0.10 cannot produce three fingerprints of one
// configuration. The float64 detour is a canonicalisation of BYTES and never carries
// a quantity -- a mass, a price and a length are integers in this application, and
// the detour is refused past 2^53 rather than silently losing a digit.
func canonicalNumber(number json.Number) string {
	if whole, err := strconv.ParseInt(number.String(), 10, 64); err == nil {
		return strconv.FormatInt(whole, 10)
	}
	value, err := number.Float64()
	if err != nil {
		return number.String()
	}
	if value <= -exactIntegerFloat || value >= exactIntegerFloat {
		// Too big to be re-spelled without dropping a digit: the original wins.
		return number.String()
	}
	if value == float64(int64(value)) {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'g', -1, 64)
}

// sortedKeys reports the keys of a map in a stable order, which is what makes both
// the canonical JSON and the sequence of faults reproducible.
func sortedKeys[V any](m map[string]V) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
