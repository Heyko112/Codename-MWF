package httpserver

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Heyko112/codename-mwf/internal/scripts"
	"github.com/Heyko112/codename-mwf/internal/tables"
	"github.com/Heyko112/codename-mwf/internal/widgets"
)

// Server-Skripte (Schritt E3). Das Widget ruft mwf.run(name, params) auf, der Server führt die
// Funktion aus server/main.js des Pakets aus. Im Skript gelten dieselben Rechte wie in der
// Daten-Brücke: Recht des Benutzers ∩ Freigabe des Widgets. Mit mwf.asService gelten die Rechte
// der Service-Rolle des Widgets (vom Verwalter gesetzt), ebenfalls nur innerhalb der Freigaben.

type runParams struct {
	Name   string          `json:"name"`
	Params json.RawMessage `json:"params"`
}

func (s *Server) apiRun(r *http.Request, wd *widgets.Widget, raw json.RawMessage) (any, error) {
	var p runParams
	if err := json.Unmarshal(raw, &p); err != nil || !scripts.ValidName(p.Name) {
		return nil, apiErr(http.StatusBadRequest, "bad_request", "run braucht einen gültigen Funktionsnamen.")
	}
	if !wd.HasScripts {
		return nil, apiErr(http.StatusNotFound, "not_found", "Das Widget hat keine Server-Skripte (server/main.js).")
	}
	ctx := r.Context()
	u := CurrentUser(ctx)
	g, acc := CurrentAccess(ctx)
	info := map[string]any{
		"user":   map[string]any{"username": u.Username, "displayName": u.DisplayName, "roles": visibleRoleNames(g, acc)},
		"widget": map[string]any{"id": wd.Slug, "name": wd.Name, "version": wd.Version},
	}
	api := &scriptAPI{s: s, r: r, wd: wd}
	src := func(ctx context.Context, p string) ([]byte, error) {
		_, data, err := s.widgets.File(ctx, wd.ID, p)
		return data, err
	}
	start := time.Now()
	out, err := scripts.Run(ctx, src, p.Name, p.Params, info, api)
	code := ""
	var se *scripts.Error
	if errors.As(err, &se) {
		code = se.Code
	} else if err != nil {
		code = "server"
	}
	slog.Info("Widget-Skript", "id", RequestIDFrom(ctx), "widget", wd.Slug, "funktion", p.Name, "benutzer", u.Username,
		"dauer", time.Since(start).Round(time.Millisecond).String(), "service", api.usedService, "fehler", code)
	if se != nil {
		return nil, apiErr(scriptStatus(se.Code), se.Code, se.Message)
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func scriptStatus(code string) int {
	switch code {
	case "not_found", "not_mapped":
		return http.StatusNotFound
	case "forbidden":
		return http.StatusForbidden
	case "bad_request":
		return http.StatusBadRequest
	case "timeout":
		return http.StatusGatewayTimeout
	case "server":
		return http.StatusInternalServerError
	}
	return http.StatusUnprocessableEntity
}

// scriptAPI verbindet das Skript mit der Daten-Brücke.
type scriptAPI struct {
	s           *Server
	r           *http.Request
	wd          *widgets.Widget
	userTables  []apiTable
	svcTables   []apiTable
	usedService bool
}

func (a *scriptAPI) Call(ctx context.Context, service bool, method string, params json.RawMessage) (json.RawMessage, error) {
	r := a.r.WithContext(ctx)
	call := apiCall{Method: method, Params: params}
	var res any
	var err error
	switch method {
	case "list", "get", "create", "update", "delete":
		var list []apiTable
		list, err = a.tables(r, service)
		if err == nil {
			via := ""
			if service {
				via = " (Service-Rolle, Skript)"
			}
			res, err = a.s.recordCall(r, a.wd, call, list, via)
			if err == nil && service && (method == "create" || method == "update") {
				var p recordParams
				json.Unmarshal(params, &p)
				a.s.auditDetail(r, 0, "record_written_service", p.Table+" "+method+" durch "+CurrentUser(ctx).Username+" über Widget "+a.wd.Slug+via)
			}
		}
	case "users", "roles", "settings":
		res, err = a.s.widgetMethod(r, a.wd, call)
	default:
		return nil, &scripts.Error{Code: "bad_request", Message: "Unbekannte Funktion „" + method + "“."}
	}
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			return nil, &scripts.Error{Code: ae.Code, Message: ae.Message}
		}
		return nil, err
	}
	return json.Marshal(res)
}

// tables liefert die Tabellen mit Benutzerrechten bzw. mit den Rechten der Service-Rolle.
func (a *scriptAPI) tables(r *http.Request, service bool) ([]apiTable, error) {
	if !service {
		if a.userTables == nil {
			list, err := a.s.apiTables(r, a.wd)
			if err != nil {
				return nil, err
			}
			a.userTables = list
		}
		return a.userTables, nil
	}
	if a.svcTables == nil {
		if !a.wd.ServiceRoleID.Valid {
			return nil, apiErr(http.StatusForbidden, "forbidden", "Für dieses Widget ist keine Service-Rolle eingerichtet (Widgets verwalten → Server-Skripte).")
		}
		ctx := r.Context()
		g, err := a.s.roles.Graph(ctx)
		if err != nil {
			return nil, err
		}
		acc := g.Resolve([]int64{a.wd.ServiceRoleID.Int64})
		if acc.IsAdmin(g) {
			return nil, apiErr(http.StatusForbidden, "forbidden", "Die Rolle „Administratoren“ ist als Service-Rolle nicht erlaubt.")
		}
		ids := make([]int64, 0, len(acc.Roles))
		for id := range acc.Roles {
			ids = append(ids, id)
		}
		access, err := a.s.tables.UserAccess(ctx, ids)
		if err != nil {
			return nil, err
		}
		list, err := a.s.apiTablesFor(ctx, a.wd, access, false)
		if err != nil {
			return nil, err
		}
		a.svcTables = list
	}
	a.usedService = true
	return a.svcTables, nil
}

// Verschlüsselung je Widget: Schlüssel aus dem App-Schlüssel (TOTP_KEY) abgeleitet, AES-256-GCM.
// Ein Widget kann nur entschlüsseln, was es selbst verschlüsselt hat.
const encPrefix = "enc1:"

func (a *scriptAPI) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(a.s.secrets.MAC("widget-crypto", a.wd.Slug))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (a *scriptAPI) Encrypt(plain string) (string, error) {
	aead, err := a.aead()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return encPrefix + base64.RawURLEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(plain), []byte(a.wd.Slug))), nil
}

func (a *scriptAPI) Decrypt(sealed string) (string, error) {
	bad := &scripts.Error{Code: "invalid", Message: "Wert lässt sich nicht entschlüsseln."}
	if !strings.HasPrefix(sealed, encPrefix) {
		return "", bad
	}
	b, err := base64.RawURLEncoding.DecodeString(sealed[len(encPrefix):])
	if err != nil {
		return "", bad
	}
	aead, err := a.aead()
	if err != nil {
		return "", err
	}
	n := aead.NonceSize()
	if len(b) < n {
		return "", bad
	}
	plain, err := aead.Open(nil, b[:n], b[n:], []byte(a.wd.Slug))
	if err != nil {
		return "", bad
	}
	return string(plain), nil
}

func (a *scriptAPI) HMAC(msg string) string {
	return hex.EncodeToString(a.s.secrets.MAC("widget-hmac:"+a.wd.Slug, msg))
}

func (a *scriptAPI) Log(msg string) {
	if len(msg) > 2000 {
		msg = msg[:2000] + " …"
	}
	slog.Info("Widget-Skript-Log", "id", RequestIDFrom(a.r.Context()), "widget", a.wd.Slug, "text", msg)
}

// serviceRoleChoice prüft die Auswahl der Service-Rolle (0 = keine).
func (s *Server) serviceRoleChoice(ctx context.Context, raw string) (int64, error) {
	id, _ := strconv.ParseInt(raw, 10, 64)
	if id == 0 {
		return 0, nil
	}
	role, err := s.roles.RoleByID(ctx, id)
	if err != nil {
		return 0, tables.InputError("Unbekannte Rolle.")
	}
	if role.System {
		return 0, tables.InputError("„Administratoren“ ist als Service-Rolle nicht erlaubt.")
	}
	return id, nil
}
