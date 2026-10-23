// Package config liest die Konfiguration aus Umgebungsvariablen.
package config

import (
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
}

// Load liest die Konfiguration und meldet fehlende Pflichtwerte.
func Load() (Config, error) {
	c := Config{
		Addr:       getenv("APP_ADDR", ":8080"),
		DBHost:     getenv("DB_HOST", "db"),
		DBPort:     getenv("DB_PORT", "3306"),
		DBName:     os.Getenv("DB_NAME"),
		DBUser:     os.Getenv("DB_USER"),
		DBPassword: os.Getenv("DB_PASSWORD"),
	}
	for name, v := range map[string]string{"DB_NAME": c.DBName, "DB_USER": c.DBUser, "DB_PASSWORD": c.DBPassword} {
		if v == "" {
			return c, fmt.Errorf("Umgebungsvariable %s fehlt", name)
		}
	}
	return c, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
