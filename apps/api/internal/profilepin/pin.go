// Package profilepin contains the deliberately narrow, one-way PIN primitive
// for entering reader child mode. It is not an authentication mechanism.
package profilepin

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

const (
	Length = 4
	// Cost 10 is bcrypt's standard interactive cost. Database-backed lockout
	// limits the online search space for the short, child-friendly PIN.
	Cost = bcrypt.DefaultCost
)

var ErrInvalid = errors.New("profile PIN must be exactly four ASCII digits")

// Valid reports whether pin has the exact canonical wire format. In
// particular, whitespace and non-ASCII numerals are rejected rather than
// normalized.
func Valid(pin string) bool {
	if len(pin) != Length {
		return false
	}
	for _, char := range []byte(pin) {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

// Hash returns a salted bcrypt encoding. The caller must validate first so
// malformed PINs are never hashed or treated as alternate spellings.
func Hash(pin string) (string, error) {
	if !Valid(pin) {
		return "", ErrInvalid
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), Cost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// Matches compares a candidate with an encoded bcrypt hash. It intentionally
// exposes only a boolean match result, never the hash or derived bytes.
func Matches(encodedHash, candidate string) (bool, error) {
	err := bcrypt.CompareHashAndPassword([]byte(encodedHash), []byte(candidate))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return false, nil
	}
	return false, err
}
