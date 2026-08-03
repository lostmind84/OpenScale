package domain

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// This file holds the SHAPE predicates the controls lean on: is this a host:port,
// an absolute web address, a #RRGGBB colour, an argon2id fingerprint?
//
// They answer about a value alone, knowing nothing of the block that carries it,
// which is why several controls and one option schema can share the same one --
// network.listen and an OptionHostPort are judged by the very same function, and a
// station must never refuse an address its own administration screen accepts.

// CheckListenAddress reports why an address cannot be listened on, and nil when it can.
//
// It is exported so that whoever accepts a listening address from OUTSIDE the file —
// `serve --listen`, and nothing else so far — judges it by the very rule control 2
// judges network.listen by. A second implementation in the command layer would drift,
// and the station would end up refusing an address its own administration screen
// accepts, or the other way round.
func CheckListenAddress(address string) error { return checkHostPort(address) }

// checkHostPort reports why an address is not a usable host:port.
func checkHostPort(address string) error {
	if address == "" {
		return fmt.Errorf("adresse vide")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("port %q hors bornes [1, 65535]", port)
	}
	// An empty host is legitimate: ":8085" listens on every interface, which is what
	// admin_on_lan describes.
	_ = host
	return nil
}

// isHTTPURL reports whether a value is an absolute http or https URL.
func isHTTPURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

// wellFormedColor reports whether a colour is spelled #RRGGBB.
func wellFormedColor(color string) bool {
	if len(color) != 7 || color[0] != '#' {
		return false
	}
	for i := 1; i < len(color); i++ {
		c := color[i]
		hex := c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
		if !hex {
			return false
		}
	}
	return true
}

// wellFormedArgon2id reports whether a hash is an argon2id PHC string.
//
// The shape is checked, never the cost: raising m, t or p is a legitimate
// hardening, and a validation that froze them would refuse a configuration that is
// SAFER than the one it was written against.
func wellFormedArgon2id(hash string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false
	}
	if !strings.HasPrefix(parts[2], "v=") {
		return false
	}
	for _, parameter := range []string{"m=", "t=", "p="} {
		if !strings.Contains(parts[3], parameter) {
			return false
		}
	}
	return isBase64Raw(parts[4], 8) && isBase64Raw(parts[5], 16)
}

// usableArgon2id reports whether a hash could have come out of argon2id at all.
//
// Being well formed is not enough, and the delivered configuration is the proof: its
// payload decoded to « for-the-delivered-configurationg », thirty-two bytes of typed
// text where argon2id writes thirty-two bytes drawn at random. What gives a placeholder
// away is therefore not its length but its ALPHABET — thirty-two random bytes are all
// printable ASCII once in 10^14, which is never.
//
// It is not this check that repairs the defect: emptying the field does. This is what
// stops the same gesture from coming back without a sound.
func usableArgon2id(hash string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 {
		return false
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return false
	}
	for _, b := range key {
		if b < 0x20 || b > 0x7e {
			return true
		}
	}
	return false
}

// isBase64Raw reports whether s is unpadded base64 of at least minimum characters.
func isBase64Raw(s string, minimum int) bool {
	if len(s) < minimum {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '+' || c == '/' || c == '-' || c == '_'
		if !ok {
			return false
		}
	}
	return true
}
