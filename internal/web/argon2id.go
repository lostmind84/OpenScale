// This file holds the CRYPTOGRAPHY of the administration: how a secret becomes a
// PHC string, how one is read back, and the eight-character code printed on the
// installation sheet.
//
// The two lengths a secret is held to are declared here as well, next to one another,
// and the password floor is deliberately NOT a cryptographic quantity: it is here
// because every door that sets a password has to compare against the same one.
//
// The cost parameters are read FROM THE STORED HASH and never assumed, which is
// what lets a cooperative raise them without locking itself out of the stations
// that were installed before.

package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"golang.org/x/crypto/argon2"
	"math/big"
	"strings"
)

// The argon2id parameters used when THIS binary hashes a password.
//
// They are the cost of one login on the target hardware (an i3 of 2015), and they
// are deliberately not configurable: an operator has no legitimate choice to make
// about a key derivation cost, and a station where it could be lowered would be a
// station where it WOULD be lowered. Verification reads the parameters from the
// stored string, so raising them later keeps every existing hash valid.
const (
	argonMemory  = 64 * 1024 // KiB
	argonTime    = 3
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

// errBadHash reports a stored hash this binary cannot read.
var errBadHash = errors.New("web: empreinte argon2id illisible")

// HashSecret produces the PHC string a configuration file carries.
//
// The format is the one §11.2 shows and the one Config.Validate checks the shape of:
// $argon2id$v=19$m=…,t=…,p=…$salt$hash, both parts in unpadded base64.
//
// It is EXPORTED for one caller outside this package: `openscale config password`, the
// command line §14.4 keeps beside the screen for a station in Assigned Access whose
// wizard was never run. Two implementations of this format would be two ways of writing
// the same field, and the day they drifted the station would refuse a password nobody
// mistyped.
func HashSecret(secret string) (string, error) {
	return hashWithCost(secret, argonMemory, argonTime, argonThreads)
}

// hashWithCost is HashSecret with the cost spelled out.
//
// Production has exactly one caller and it passes the constants above. The parameters
// exist so that a test can produce a hash written by an OLDER binary — which is the
// case VerifySecret has to keep opening.
func hashWithCost(secret string, memory, iterations uint32, threads uint8) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("web: tirage du sel impossible : %w", err)
	}
	key := argon2.IDKey([]byte(secret), salt, iterations, memory, threads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memory, iterations, threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// RecoveryCodeLength is the eight characters §14.4 prints on the installation sheet.
const RecoveryCodeLength = 8

// MinPasswordLength is the floor an administration password is held to, counted in CODE
// POINTS and not in bytes.
//
// It is an arbitrage of USE, made by the product owner, and not a cryptographic one: the
// password is typed on the station's touch screen, on its on-screen keyboard, by a
// volunteer standing in a shop on a Saturday with a queue behind them. A floor that makes
// that gesture long enough to give up on is a floor that gets written on a sticker beside
// the screen.
//
// It is EXPORTED and it is the only place the figure exists, because two doors set a
// password — `openscale config password` and the recovery form of §14.4 — and a value
// spelled out twice is how one of them ends up refusing what the other has just accepted.
const MinPasswordLength = 4

// recoveryAlphabet is what those eight characters are drawn from.
//
// Neither I, L, O, U, 0 nor 1. This code is not typed by whoever generated it: it is read
// off a sheet of paper filed in the shop's folder, months later, by a volunteer who is
// already having a bad morning. The pair O/0 alone accounts for most of what a printed
// code loses on its way back to a keyboard, and U leaves with them so that eight random
// characters never spell a word somebody would then keep in their head instead of the
// folder.
const recoveryAlphabet = "ABCDEFGHJKMNPQRSTVWXYZ23456789"

// NewRecoveryCode draws the recovery code of §14.4, in clear, ONCE.
//
// The station never stores it: what goes into the configuration is its argon2id hash, and
// the only copy in existence is the one printed on the installation sheet. That is the
// whole point — it is a possession factor, and a possession factor a machine can read
// back is not one.
func NewRecoveryCode() (string, error) {
	code := make([]byte, RecoveryCodeLength)
	for i := range code {
		// rand.Int and not a modulo of one byte: the alphabet has 30 characters, 256 is
		// not a multiple of 30, and the bias that follows would make six of them a third
		// more likely than the rest.
		drawn, err := rand.Int(rand.Reader, big.NewInt(int64(len(recoveryAlphabet))))
		if err != nil {
			return "", fmt.Errorf("web: tirage du code de secours impossible : %w", err)
		}
		code[i] = recoveryAlphabet[drawn.Int64()]
	}
	return string(code), nil
}

// NormalizeRecoveryCode is what both ends apply before hashing or comparing.
//
// The alphabet is upper case, so a code copied in lower case out of the folder is the
// SAME code and must open the same door. Refusing it would be refusing a volunteer for a
// shift key.
func NormalizeRecoveryCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

// VerifySecret reports whether secret is the one behind encoded.
//
// The cost parameters come from the STORED string and not from the constants above:
// raising the cost of new hashes must never invalidate the ones already written, and
// a station whose password was set by an older binary has to keep opening.
func VerifySecret(encoded, secret string) bool {
	salt, want, memory, iterations, threads, err := parsePHC(encoded)
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(secret), salt, iterations, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// parsePHC takes a stored argon2id string apart.
func parsePHC(encoded string) (salt, key []byte, memory, iterations uint32, threads uint8, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return nil, nil, 0, 0, 0, errBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return nil, nil, 0, 0, 0, errBadHash
	}
	var parallelism int
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return nil, nil, 0, 0, 0, errBadHash
	}
	if parallelism < 1 || parallelism > 255 {
		return nil, nil, 0, 0, 0, errBadHash
	}
	if salt, err = decodeBase64(parts[4]); err != nil {
		return nil, nil, 0, 0, 0, errBadHash
	}
	if key, err = decodeBase64(parts[5]); err != nil {
		return nil, nil, 0, 0, 0, errBadHash
	}
	return salt, key, memory, iterations, uint8(parallelism), nil
}

// decodeBase64 accepts the padded and the unpadded spelling: a hash written by hand,
// or by another tool, must not be refused over a trailing equals sign.
func decodeBase64(s string) ([]byte, error) {
	if raw, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return raw, nil
	}
	return base64.StdEncoding.DecodeString(s)
}
