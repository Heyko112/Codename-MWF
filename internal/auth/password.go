// Package auth enthält Passwort-Hashing, TOTP und den Zugriff auf Benutzer.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id-Parameter nach OWASP-Empfehlung (mindestens 19 MiB, 2 Durchläufe).
const (
	argonMemory  = 64 * 1024 // KiB
	argonTime    = 3
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

// MinPasswordLen ist die Mindestlänge für neue Passwörter.
const MinPasswordLen = 12

var errBadHash = errors.New("ungültiges Hash-Format")

// HashPassword erzeugt einen Argon2id-Hash im PHC-Format.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword prüft ein Passwort gegen einen Hash in konstanter Zeit.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errBadHash
	}
	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, errBadHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, errBadHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, errBadHash
	}
	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// UnusablePasswordHash liefert einen Hash zu einem zufälligen, nie bekannten Passwort (für Service-User).
func UnusablePasswordHash() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return HashPassword(base64.RawStdEncoding.EncodeToString(b))
}

// dummyHash wird geprüft, wenn es den Benutzer nicht gibt, damit die Antwortzeit nichts verrät.
var dummyHash, _ = HashPassword("dummy-password-for-timing")

// BurnTime rechnet einen Hash durch, ohne ein Ergebnis zu liefern.
func BurnTime(password string) {
	VerifyPassword(password, dummyHash)
}
