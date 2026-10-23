// Package config liest die Konfiguration aus Umgebungsvariablen.
package config

import (
	"encoding/base64"
	"fmt"
	"os"
)

type Config struct {
	Addr       string // Listen-Adresse der App, z. B. ":8080"
	DBHost     string
	DBPort     string
	DBName     string
	DBUser     string
	DBPassword string
	// Session-Cookie nur über HTTPS senden. Ab Schritt 6 (TLS) standardmäßig an.
	CookieSecure bool
	// 32-Byte-Schlüssel (Base64) zum Verschlüsseln der TOTP-Secrets
	TOTPKey []byte
}

// Load liest die Konfiguration und meldet fehlende Pflichtwerte.
func Load() (Config, error) {
	c := Config{
		Addr:         getenv("APP_ADDR", ":8080"),
		DBHost:       getenv("DB_HOST", "db"),
		DBPort:       getenv("DB_PORT", "3306"),
		DBName:       os.Getenv("DB_NAME"),
		DBUser:       os.Getenv("DB_USER"),
		DBPassword:   os.Getenv("DB_PASSWORD"),
		CookieSecure: os.Getenv("COOKIE_SECURE") == "true",
	}
	for name, v := range map[string]string{"DB_NAME": c.DBName, "DB_USER": c.DBUser, "DB_PASSWORD": c.DBPassword} {
		if v == "" {
			return c, fmt.Errorf("Umgebungsvariable %s fehlt", name)
		}
	}
	key, err := base64.StdEncoding.DecodeString(os.Getenv("TOTP_KEY"))
	if err != nil || len(key) != 32 {
		return c, fmt.Errorf("TOTP_KEY fehlt oder ist kein Base64-Wert mit 32 Byte (erzeugen mit: openssl rand -base64 32)")
	}
	c.TOTPKey = key
	return c, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
