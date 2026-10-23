package httpserver

import (
	"context"
	"errors"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Heyko112/codename-mwf/internal/auth"
	"github.com/Heyko112/codename-mwf/internal/authz"
)

// Session-Schlüssel
const (
	sessUserID      = "user_id"      // vollständig angemeldet
	sessPendingID   = "pending_id"   // Passwort ok, 2FA steht noch aus
	sessPendingAt   = "pending_at"   // Unix-Zeit des Passwort-Schritts
	sessSetupSecret = "setup_secret" // TOTP-Secret während der Einrichtung
)

// Zeit zwischen Passwort und 2FA-Code
const pendingTimeout = 5 * time.Minute

const msgLoginFailed = "Anmeldung fehlgeschlagen. Nach mehreren Fehlversuchen wird das Konto für 15 Minuten gesperrt."

type userCtxKey struct{}

// CurrentUser liefert den angemeldeten Benutzer (nur hinter requireLogin gesetzt).
func CurrentUser(ctx context.Context) *auth.User {
	u, _ := ctx.Value(userCtxKey{}).(*auth.User)
	return u
}

type accessCtxKey struct{}

type accessInfo struct {
	graph  *authz.Graph
	access *authz.Access
}

// CurrentAccess liefert Rollen und Rechte des angemeldeten Benutzers (nur hinter requireLogin gesetzt).
func CurrentAccess(ctx context.Context) (*authz.Graph, *authz.Access) {
	a, _ := ctx.Value(accessCtxKey{}).(*accessInfo)
	if a == nil {
		return nil, nil
	}
	return a.graph, a.access
}

// requirePerm lässt nur angemeldete Benutzer mit dem Recht perm durch.
func (s *Server) requirePerm(perm string, next http.HandlerFunc) http.HandlerFunc {
	return s.requireLogin(func(w http.ResponseWriter, r *http.Request) {
		if _, acc := CurrentAccess(r.Context()); !acc.Has(perm) {
			s.render(w, r, http.StatusForbidden, "error", map[string]any{
				"User":    CurrentUser(r.Context()),
				"Status":  "403",
				"Message": "Dafür fehlt dir das Recht „" + authz.PermissionLabel(perm) + "“.",
			})
			return
		}
		next(w, r)
	})
}

// requireLogin leitet nicht angemeldete Besucher zur Login-Seite um.
func (s *Server) requireLogin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := s.sessions.GetInt64(r.Context(), sessUserID)
		if id == 0 {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		u, err := s.users.ByID(r.Context(), id)
		if err != nil || !u.IsActive || u.IsService {
			s.sessions.Destroy(r.Context())
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		g, acc, err := s.roles.Access(r.Context(), u.ID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), userCtxKey{}, u)
		ctx = context.WithValue(ctx, accessCtxKey{}, &accessInfo{graph: g, access: acc})
		w.Header().Set("Cache-Control", "no-store")
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	if s.sessions.GetInt64(r.Context(), sessUserID) != 0 {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, r, http.StatusOK, "login", nil)
}

func (s *Server) loginSubmit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")

	fail := func(userID int64, event string) {
		s.audit(r, userID, event)
		s.render(w, r, http.StatusUnauthorized, "login", map[string]any{"Error": msgLoginFailed, "Username": username})
	}

	u, err := s.users.ByUsername(ctx, username)
	if errors.Is(err, auth.ErrNotFound) {
		auth.BurnTime(password)
		fail(0, "login_failed")
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	// Service-User haben keinen Web-Login.
	if !u.IsActive || u.IsService || u.Locked(time.Now()) {
		auth.BurnTime(password)
		fail(u.ID, "login_blocked")
		return
	}
	ok, err := auth.VerifyPassword(password, u.PasswordHash)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if !ok {
		s.recordFailure(r, u.ID)
		fail(u.ID, "login_failed")
		return
	}

	// Passwort stimmt. Neue Session-ID gegen Session Fixation, dann weiter zur 2FA.
	if err := s.sessions.RenewToken(ctx); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.sessions.Put(ctx, sessPendingID, u.ID)
	s.sessions.Put(ctx, sessPendingAt, time.Now().Unix())
	if u.TOTPEnabled {
		http.Redirect(w, r, "/login/2fa", http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/login/2fa/einrichten", http.StatusSeeOther)
	}
}

// pendingUser liefert den Benutzer, der das Passwort schon richtig eingegeben hat.
func (s *Server) pendingUser(r *http.Request) *auth.User {
	ctx := r.Context()
	id := s.sessions.GetInt64(ctx, sessPendingID)
	at := s.sessions.GetInt64(ctx, sessPendingAt)
	if id == 0 || time.Since(time.Unix(at, 0)) > pendingTimeout {
		return nil
	}
	u, err := s.users.ByID(ctx, id)
	if err != nil || !u.IsActive || u.IsService || u.Locked(time.Now()) {
		return nil
	}
	return u
}

func (s *Server) totpForm(w http.ResponseWriter, r *http.Request) {
	u := s.pendingUser(r)
	if u == nil || !u.TOTPEnabled {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	s.render(w, r, http.StatusOK, "totp", nil)
}

func (s *Server) totpSubmit(w http.ResponseWriter, r *http.Request) {
	u := s.pendingUser(r)
	if u == nil || !u.TOTPEnabled {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	secret, err := s.secrets.Open(u.TOTPSecret)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	step, ok := auth.CheckTOTP(secret, strings.TrimSpace(r.PostFormValue("code")), u.TOTPLastStep, time.Now())
	if !ok {
		s.audit(r, u.ID, "2fa_failed")
		if locked := s.recordFailure(r, u.ID); locked {
			s.sessions.Destroy(r.Context())
			s.render(w, r, http.StatusUnauthorized, "login", map[string]any{"Error": msgLoginFailed})
			return
		}
		s.render(w, r, http.StatusUnauthorized, "totp", map[string]any{"Error": "Der Code ist ungültig oder wurde schon verwendet."})
		return
	}
	s.completeLogin(w, r, u.ID, step)
}

func (s *Server) totpSetupForm(w http.ResponseWriter, r *http.Request) {
	u := s.pendingUser(r)
	if u == nil || u.TOTPEnabled {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	setup, err := auth.NewTOTP(u.Username)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	// Das Secret bleibt bis zur Bestätigung nur in der Session.
	s.sessions.Put(r.Context(), sessSetupSecret, setup.Secret)
	s.render(w, r, http.StatusOK, "totp_setup", setupData(setup, ""))
}

func (s *Server) totpSetupSubmit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := s.pendingUser(r)
	secret := s.sessions.GetString(ctx, sessSetupSecret)
	if u == nil || u.TOTPEnabled || secret == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	step, ok := auth.CheckTOTP(secret, strings.TrimSpace(r.PostFormValue("code")), 0, time.Now())
	if !ok {
		s.audit(r, u.ID, "2fa_setup_failed")
		setup, err := auth.NewTOTP(u.Username)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		s.sessions.Put(ctx, sessSetupSecret, setup.Secret)
		s.render(w, r, http.StatusUnauthorized, "totp_setup",
			setupData(setup, "Der Code war ungültig. Bitte den neuen QR-Code scannen und erneut versuchen."))
		return
	}
	sealed, err := s.secrets.Seal(secret)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.users.EnableTOTP(ctx, u.ID, sealed, step); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.audit(r, u.ID, "2fa_enabled")
	s.completeLogin(w, r, u.ID, step)
}

// setupData bereitet die Einrichtungsseite vor. Die data:-URL des QR-Codes stammt aus
// unserem eigenen PNG-Encoder und wird deshalb als sichere URL markiert.
func setupData(setup auth.TOTPSetup, errMsg string) map[string]any {
	return map[string]any{
		"QRCode": template.URL(setup.QRCode),
		"Secret": setup.Secret,
		"Error":  errMsg,
	}
}

func (s *Server) completeLogin(w http.ResponseWriter, r *http.Request, userID, step int64) {
	ctx := r.Context()
	if err := s.users.RecordSuccess(ctx, userID, step); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.sessions.RenewToken(ctx); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.sessions.Remove(ctx, sessPendingID)
	s.sessions.Remove(ctx, sessPendingAt)
	s.sessions.Remove(ctx, sessSetupSecret)
	s.sessions.Put(ctx, sessUserID, userID)
	s.audit(r, userID, "login_ok")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if id := s.sessions.GetInt64(r.Context(), sessUserID); id != 0 {
		s.audit(r, id, "logout")
	}
	if err := s.sessions.Destroy(r.Context()); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// recordFailure zählt einen Fehlversuch und liefert true, wenn das Konto jetzt gesperrt ist.
func (s *Server) recordFailure(r *http.Request, userID int64) bool {
	locked, err := s.users.RecordFailure(r.Context(), userID)
	if err != nil {
		slog.Error("Fehlversuch nicht gespeichert", "id", RequestIDFrom(r.Context()), "fehler", err)
		return false
	}
	if locked {
		s.audit(r, userID, "account_locked")
	}
	return locked
}

func (s *Server) audit(r *http.Request, userID int64, event string) {
	s.auditDetail(r, userID, event, "")
}

func (s *Server) auditDetail(r *http.Request, userID int64, event, detail string) {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if err := s.users.Audit(r.Context(), userID, event, detail, ip, r.UserAgent()); err != nil {
		slog.Error("Audit-Eintrag fehlgeschlagen", "id", RequestIDFrom(r.Context()), "event", event, "fehler", err)
	}
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("Serverfehler", "id", RequestIDFrom(r.Context()), "fehler", err)
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}
