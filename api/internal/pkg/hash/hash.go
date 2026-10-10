// Package hash implements argon2id password hashing in PHC string format
// (docs/04-auth.md §6).
package hash

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	memoryKiB = 65536
	timeCost  = 3
	threads   = 2
	saltLen   = 16
	keyLen    = 32
)

// ErrInvalidHash is returned when a stored hash is not a valid argon2id PHC string.
var ErrInvalidHash = errors.New("hash: invalid argon2id PHC string")

var b64 = base64.RawStdEncoding

// Hash returns the PHC encoding of the argon2id hash of password with a fresh salt.
func Hash(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("hash: salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, timeCost, memoryKiB, threads, keyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memoryKiB, timeCost, threads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Verify checks password against an encoded hash using a constant-time compare.
// It returns ErrInvalidHash when encoded cannot be parsed.
func Verify(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	// ["", "argon2id", "v=19", "m=..,t=..,p=..", salt, hash]
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, ErrInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, ErrInvalidHash
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil || m == 0 || t == 0 || p == 0 {
		return false, ErrInvalidHash
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, ErrInvalidHash
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, ErrInvalidHash
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want))) //nolint:gosec // len bounded by stored hash
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummy is a valid hash used to burn equal CPU time when no real hash exists.
var dummy, _ = Hash("athenaeum-dummy-password")

// DummyVerify performs a verification against a fixed hash so that unknown
// accounts take as long as wrong passwords (docs/04 §6 constant failure timing).
func DummyVerify(password string) {
	_, _ = Verify(password, dummy)
}
