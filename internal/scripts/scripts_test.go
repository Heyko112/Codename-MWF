package scripts

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeAPI struct {
	calls []string
	logs  []string
}

func (f *fakeAPI) Call(_ context.Context, service bool, method string, params json.RawMessage) (json.RawMessage, error) {
	f.calls = append(f.calls, method+" "+string(params)+map[bool]string{true: " service", false: ""}[service])
	switch method {
	case "list":
		return json.RawMessage(`{"total":1,"records":[{"id":7,"values":{"person":"anna"}}]}`), nil
	case "delete":
		return nil, &Error{Code: "forbidden", Message: "Keine Berechtigung"}
	}
	return json.RawMessage(`null`), nil
}
func (f *fakeAPI) Encrypt(p string) (string, error) { return "enc:" + p, nil }
func (f *fakeAPI) Decrypt(s string) (string, error) { return strings.TrimPrefix(s, "enc:"), nil }
func (f *fakeAPI) HMAC(m string) string             { return "h" + m }
func (f *fakeAPI) Log(m string)                     { f.logs = append(f.logs, m) }

func files(m map[string]string) Source {
	return func(_ context.Context, p string) ([]byte, error) {
		if s, ok := m[p]; ok {
			return []byte(s), nil
		}
		return nil, errors.New("fehlt")
	}
}

func run(t *testing.T, src map[string]string, name, params string, api *fakeAPI) (string, error) {
	t.Helper()
	out, err := Run(context.Background(), files(src), name, json.RawMessage(params), map[string]any{"user": map[string]any{"username": "anna"}}, api)
	return string(out), err
}

func TestRun(t *testing.T) {
	api := &fakeAPI{}
	src := map[string]string{
		"server/main.js": `
			const util = require("./util.js");
			exports.hallo = function (p) {
				const r = mwf.list("urlaub", { where: [{ col: "person", op: "eq", value: "{ich}" }] });
				console.log("gelesen", r.total);
				return { gruss: util.gruss(mwf.user.username), anzahl: r.total, x: p.x, enc: mwf.crypto.encrypt("geheim") };
			};
			exports.service = function () { return mwf.asService.get("org", 3); };
			exports.nichts = function () {};
			exports.fehler = function () { mwf.error("invalid", "Zu viele Tage"); };
			exports.verboten = function () { mwf.remove("urlaub", 1); };
			exports.gefangen = function () { try { mwf.remove("urlaub", 1); } catch (e) { return e.code; } };
			exports.wurf = function () { throw new Error("kaputt"); };
			exports.ewig = function () { for (;;) {} };`,
		"server/util.js": `exports.gruss = function (n) { return "Hallo " + n; };`,
	}
	out, err := run(t, src, "hallo", `{"x":5}`, api)
	if err != nil || out != `{"gruss":"Hallo anna","anzahl":1,"x":5,"enc":"enc:geheim"}` {
		t.Fatalf("%s %v", out, err)
	}
	if len(api.calls) != 1 || !strings.HasPrefix(api.calls[0], `list {"table":"urlaub","where":`) || api.logs[0] != "gelesen 1" {
		t.Fatalf("%v %v", api.calls, api.logs)
	}
	if _, err := run(t, src, "service", "", api); err != nil || !strings.HasSuffix(api.calls[1], " service") {
		t.Fatalf("%v %v", err, api.calls)
	}
	if out, err := run(t, src, "nichts", "", api); err != nil || out != "null" {
		t.Fatalf("%s %v", out, err)
	}
	if out, err := run(t, src, "gefangen", "", api); err != nil || out != `"forbidden"` {
		t.Fatalf("%s %v", out, err)
	}
	for name, want := range map[string]string{
		"fehler": "invalid", "verboten": "forbidden", "wurf": "script", "gibtsnicht": "not_found", "ewig": "timeout",
	} {
		_, err := run(t, src, name, "", api)
		var se *Error
		if !errors.As(err, &se) || se.Code != want {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestCheck(t *testing.T) {
	if err := Check(map[string][]byte{"index.html": nil}); err != nil {
		t.Fatal(err)
	}
	if err := Check(map[string][]byte{"server/x.js": []byte("1")}); err == nil || !strings.Contains(err.Error(), "main.js") {
		t.Fatal(err)
	}
	if err := Check(map[string][]byte{"server/main.js": []byte("exports.a = function( {")}); err == nil {
		t.Fatal("Syntaxfehler akzeptiert")
	}
	if err := Check(map[string][]byte{"server/main.js": []byte("exports.a = 1"), "server/a.json": nil}); err == nil {
		t.Fatal("json in server/ akzeptiert")
	}
}
