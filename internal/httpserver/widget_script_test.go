package httpserver

import (
	"strings"
	"testing"

	"github.com/Heyko112/codename-mwf/internal/auth"
	"github.com/Heyko112/codename-mwf/internal/widgets"
)

func TestScriptCrypto(t *testing.T) {
	box, err := auth.NewSecretBox([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{secrets: box}
	a := &scriptAPI{s: s, wd: &widgets.Widget{Slug: "hr"}}
	b := &scriptAPI{s: s, wd: &widgets.Widget{Slug: "anderes"}}
	enc, err := a.Encrypt("IBAN DE12")
	if err != nil || !strings.HasPrefix(enc, "enc1:") || strings.Contains(enc, "IBAN") {
		t.Fatal(enc, err)
	}
	if plain, err := a.Decrypt(enc); err != nil || plain != "IBAN DE12" {
		t.Fatal(plain, err)
	}
	if _, err := b.Decrypt(enc); err == nil {
		t.Fatal("anderes Widget kann entschlüsseln")
	}
	if _, err := a.Decrypt("enc1:kaputt"); err == nil {
		t.Fatal("kaputter Wert entschlüsselt")
	}
	if a.HMAC("1234") == b.HMAC("1234") || a.HMAC("1234") != a.HMAC("1234") {
		t.Fatal("HMAC")
	}
}
