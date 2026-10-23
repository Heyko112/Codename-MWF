package auth

import (
	"bytes"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

func TestPassword(t *testing.T) {
	hash, err := HashPassword("richtig-und-lang")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := VerifyPassword("richtig-und-lang", hash); err != nil || !ok {
		t.Fatalf("richtiges Passwort abgelehnt: ok=%v err=%v", ok, err)
	}
	if ok, _ := VerifyPassword("falsch", hash); ok {
		t.Fatal("falsches Passwort akzeptiert")
	}
	other, _ := HashPassword("richtig-und-lang")
	if other == hash {
		t.Fatal("gleiches Passwort ergibt gleichen Hash, Salt fehlt")
	}
	if _, err := VerifyPassword("x", "kein-hash"); err == nil {
		t.Fatal("kaputter Hash ohne Fehler akzeptiert")
	}
}

func TestSecretBox(t *testing.T) {
	box, err := NewSecretBox(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Seal("JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("JBSWY3DPEHPK3PXP")) {
		t.Fatal("Secret steht im Klartext im Chiffrat")
	}
	plain, err := box.Open(sealed)
	if err != nil || plain != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("Entschlüsseln: %q, %v", plain, err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := box.Open(sealed); err == nil {
		t.Fatal("manipuliertes Chiffrat akzeptiert")
	}
	if _, err := NewSecretBox([]byte("zu kurz")); err == nil {
		t.Fatal("zu kurzer Schlüssel akzeptiert")
	}
}

func TestCheckTOTP(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXP"
	now := time.Unix(1_800_000_000, 0)
	code, err := totp.GenerateCodeCustom(secret, now, totp.ValidateOpts{Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		t.Fatal(err)
	}

	step, ok := CheckTOTP(secret, code, 0, now)
	if !ok || step != now.Unix()/30 {
		t.Fatalf("gültiger Code abgelehnt: ok=%v step=%d", ok, step)
	}
	if _, ok := CheckTOTP(secret, code, step, now); ok {
		t.Fatal("Code wurde zweimal akzeptiert")
	}
	if _, ok := CheckTOTP(secret, code, 0, now.Add(30*time.Second)); !ok {
		t.Fatal("Code 30 s später abgelehnt, Toleranz fehlt")
	}
	if _, ok := CheckTOTP(secret, code, 0, now.Add(2*time.Minute)); ok {
		t.Fatal("abgelaufener Code akzeptiert")
	}
	if _, ok := CheckTOTP(secret, "000000", 0, now); ok && code != "000000" {
		t.Fatal("falscher Code akzeptiert")
	}
}
