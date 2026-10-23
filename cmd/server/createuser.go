package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/Heyko112/codename-mwf/internal/auth"
	"github.com/Heyko112/codename-mwf/internal/authz"
	"github.com/Heyko112/codename-mwf/internal/config"
	"github.com/Heyko112/codename-mwf/internal/db"
)

// createUser legt einen Benutzer an. Aufruf im Container:
//
//	docker compose exec app /server create-user <benutzername> [Anzeigename]
//
// Das Passwort wird interaktiv abgefragt (oder aus stdin gelesen) und nie als Argument übergeben.
// Benutzer, die so angelegt werden, kommen in die Rolle „Administratoren“. Weitere Benutzer legt ein Admin in der Oberfläche an.
func createUser(args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return errors.New("Aufruf: create-user <benutzername> [Anzeigename]")
	}
	username := strings.TrimSpace(args[0])
	displayName := ""
	if len(args) == 2 {
		displayName = strings.TrimSpace(args[1])
	}
	if username == "" || utf8.RuneCountInString(username) > 64 {
		return errors.New("Benutzername muss 1 bis 64 Zeichen lang sein")
	}

	password, err := readPassword()
	if err != nil {
		return err
	}
	if utf8.RuneCountInString(password) < auth.MinPasswordLen {
		return fmt.Errorf("Passwort muss mindestens %d Zeichen lang sein", auth.MinPasswordLen)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx := context.Background()
	database, err := db.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer database.Close()
	if err := db.Migrate(ctx, database); err != nil {
		return err
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	id, err := auth.NewStore(database).Create(ctx, username, displayName, hash, false, false)
	if err != nil {
		return fmt.Errorf("Benutzer anlegen fehlgeschlagen: %w", err)
	}
	roles := authz.NewStore(database)
	adminRole, err := roles.SystemRoleID(ctx)
	if err != nil {
		return err
	}
	if err := roles.AddMember(ctx, adminRole, id); err != nil {
		return err
	}
	fmt.Printf("Admin %q angelegt (ID %d). 2FA wird beim ersten Login eingerichtet.\n", username, id)
	return nil
}

func readPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", errors.New("kein Passwort auf stdin")
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	fmt.Fprint(os.Stderr, "Passwort: ")
	p1, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	fmt.Fprint(os.Stderr, "Passwort wiederholen: ")
	p2, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if string(p1) != string(p2) {
		return "", errors.New("Passwörter stimmen nicht überein")
	}
	return string(p1), nil
}
