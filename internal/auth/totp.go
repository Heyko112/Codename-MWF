package auth

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"image/png"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const (
	totpIssuer = "MWF"
	totpPeriod = 30
	// Erlaubte Abweichung in Zeitschritten (±30 s) für ungenaue Uhren.
	totpSkew = 1
)

// TOTPSetup enthält alles, was die Einrichtungsseite anzeigen muss.
type TOTPSetup struct {
	Secret string // Base32, zum manuellen Abtippen
	QRCode string // PNG als data:-URL
}

// NewTOTP erzeugt ein neues Secret samt QR-Code für die Authenticator-App.
func NewTOTP(username string) (TOTPSetup, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      totpIssuer,
		AccountName: username,
		Period:      totpPeriod,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1, // SHA1 ist das, was alle gängigen Apps unterstützen
	})
	if err != nil {
		return TOTPSetup{}, err
	}
	img, err := key.Image(240, 240)
	if err != nil {
		return TOTPSetup{}, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return TOTPSetup{}, err
	}
	return TOTPSetup{
		Secret: key.Secret(),
		QRCode: "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()),
	}, nil
}

// CheckTOTP prüft einen Code und liefert den passenden Zeitschritt.
// Ein Code wird nur akzeptiert, wenn sein Zeitschritt nach lastStep liegt (Schutz vor Wiederverwendung).
func CheckTOTP(secret, code string, lastStep int64, now time.Time) (step int64, ok bool) {
	current := now.Unix() / totpPeriod
	for d := int64(-totpSkew); d <= totpSkew; d++ {
		s := current + d
		if s <= lastStep {
			continue
		}
		ok, err := totp.ValidateCustom(code, secret, time.Unix(s*totpPeriod, 0), totp.ValidateOpts{
			Period:    totpPeriod,
			Skew:      0,
			Digits:    otp.DigitsSix,
			Algorithm: otp.AlgorithmSHA1,
		})
		if err == nil && ok {
			return s, true
		}
	}
	return 0, false
}

// SecretBox verschlüsselt TOTP-Secrets mit AES-256-GCM, bevor sie in die Datenbank gehen.
// Außerdem signiert sie kurzlebige Zugangsmarken (MAC) mit einem davon abgeleiteten Schlüssel.
type SecretBox struct {
	aead   cipher.AEAD
	macKey []byte
}

// NewSecretBox erwartet einen 32-Byte-Schlüssel.
func NewSecretBox(key []byte) (*SecretBox, error) {
	if len(key) != 32 {
		return nil, errors.New("TOTP-Schlüssel muss 32 Byte lang sein")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	derive := hmac.New(sha256.New, key)
	derive.Write([]byte("mwf-mac-v1"))
	return &SecretBox{aead: aead, macKey: derive.Sum(nil)}, nil
}

// MAC signiert msg für einen Zweck, damit eine Marke nicht für etwas anderes taugt.
func (b *SecretBox) MAC(purpose, msg string) []byte {
	m := hmac.New(sha256.New, b.macKey)
	m.Write([]byte(purpose))
	m.Write([]byte{0})
	m.Write([]byte(msg))
	return m.Sum(nil)
}

// Seal liefert Nonce + Chiffrat.
func (b *SecretBox) Seal(plain string) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, []byte(plain), nil), nil
}

// Open entschlüsselt, was Seal erzeugt hat.
func (b *SecretBox) Open(sealed []byte) (string, error) {
	n := b.aead.NonceSize()
	if len(sealed) < n {
		return "", errors.New("Chiffrat zu kurz")
	}
	plain, err := b.aead.Open(nil, sealed[:n], sealed[n:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
