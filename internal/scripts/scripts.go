// Package scripts führt die Server-Skripte eines Widgets aus (Schritt E3).
//
// Ein Widget bringt im Ordner server/ JavaScript mit. server/main.js legt Funktionen über
// exports an, z. B. exports.genehmiger = function (params) { … }. Jeder Aufruf läuft in einer
// frischen, leeren JavaScript-Umgebung (goja) ohne Dateien, Netz oder Datenbank. Erreichbar ist
// nur das Objekt mwf, dessen Funktionen der Server bereitstellt (API) und dort die Rechte prüft.
package scripts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/dop251/goja"
)

const (
	// Dir ist der Ordner der Server-Skripte im Paket.
	Dir = "server/"
	// Main ist die Startdatei.
	Main = Dir + "main.js"
	// Timeout begrenzt die Laufzeit eines Aufrufs.
	Timeout = 5 * time.Second
	// MaxResult begrenzt das Ergebnis (JSON).
	MaxResult = 1 << 20
	// MaxCalls begrenzt die Aufrufe von mwf-Funktionen je Lauf.
	MaxCalls = 2000
)

var nameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

// ValidName prüft den Namen einer Funktion.
func ValidName(name string) bool { return nameRe.MatchString(name) }

// Error ist ein Fehler, den das Widget zu sehen bekommt.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func scriptErr(code, msg string) *Error { return &Error{Code: code, Message: msg} }

// API stellt dem Skript die Funktionen von MWF bereit. Ergebnisse sind JSON.
// method: list, get, create, update, delete, users, roles, settings.
// service: true für mwf.asService (Rechte der Service-Rolle des Widgets).
// Fehler mit Code (z. B. forbidden) liefert die API als *Error.
type API interface {
	Call(ctx context.Context, service bool, method string, params json.RawMessage) (json.RawMessage, error)
	Encrypt(plain string) (string, error)
	Decrypt(sealed string) (string, error)
	HMAC(msg string) string
	Log(msg string)
}

// Source liefert eine Datei aus dem Paket (Pfad relativ zum Paket, z. B. server/main.js).
type Source func(ctx context.Context, path string) ([]byte, error)

// Check prüft die Skripte beim Hochladen: Gibt es Dateien in server/, muss server/main.js da sein,
// und jede Datei muss sich übersetzen lassen.
func Check(files map[string][]byte) error {
	any := false
	for p, b := range files {
		if !strings.HasPrefix(p, Dir) {
			continue
		}
		any = true
		if path.Ext(p) != ".js" {
			return fmt.Errorf("In %s sind nur .js-Dateien erlaubt: „%s“.", Dir, p)
		}
		if _, err := goja.Compile(p, wrap(string(b)), true); err != nil {
			return fmt.Errorf("Server-Skript „%s“ lässt sich nicht übersetzen: %v", p, err)
		}
	}
	if any {
		if _, ok := files[Main]; !ok {
			return fmt.Errorf("Im Ordner %s fehlt die Startdatei main.js.", Dir)
		}
	}
	return nil
}

func wrap(src string) string {
	return "(function (exports, require, module) {" + src + "\n})"
}

// Run ruft die Funktion name aus server/main.js mit params auf und liefert ihr Ergebnis als JSON.
// info steht im Skript als mwf.user und mwf.widget.
func Run(ctx context.Context, src Source, name string, params json.RawMessage, info map[string]any, api API) (json.RawMessage, error) {
	if !ValidName(name) {
		return nil, scriptErr("bad_request", "Ungültiger Funktionsname.")
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	vm := goja.New()
	vm.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
	stop := context.AfterFunc(ctx, func() { vm.Interrupt("timeout") })
	defer stop()

	r := &runner{ctx: ctx, vm: vm, src: src, api: api, modules: map[string]*goja.Object{}}
	var out json.RawMessage
	err := r.guard(func() error {
		if err := r.setup(info); err != nil {
			return err
		}
		exports, err := r.require(Main)
		if err != nil {
			return err
		}
		fn, ok := goja.AssertFunction(exports.Get(name))
		if !ok {
			return scriptErr("not_found", "Das Widget hat keine Server-Funktion „"+name+"“.")
		}
		var p any
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return scriptErr("bad_request", "Parameter sind kein gültiges JSON.")
			}
		}
		res, err := fn(goja.Undefined(), vm.ToValue(p))
		if err != nil {
			return err
		}
		if res == nil || goja.IsUndefined(res) {
			out = json.RawMessage("null")
			return nil
		}
		stringify, _ := goja.AssertFunction(vm.Get("JSON").ToObject(vm).Get("stringify"))
		js, err := stringify(goja.Undefined(), res)
		if err != nil {
			return err
		}
		if goja.IsUndefined(js) {
			out = json.RawMessage("null")
			return nil
		}
		str := js.String()
		if len(str) > MaxResult {
			return scriptErr("too_large", "Das Ergebnis des Skripts ist größer als 1 MB.")
		}
		out = json.RawMessage(str)
		return nil
	})
	if err != nil {
		return nil, r.convert(err)
	}
	return out, nil
}

type runner struct {
	ctx     context.Context
	vm      *goja.Runtime
	src     Source
	api     API
	modules map[string]*goja.Object
	calls   int
}

// guard fängt Panics aus Go-Funktionen ab, die goja als Ausnahme weiterreicht.
func (r *runner) guard(f func() error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			if e, ok := p.(error); ok {
				err = e
				return
			}
			err = fmt.Errorf("%v", p)
		}
	}()
	return f()
}

// convert macht aus einem Fehler im Skript einen *Error mit Code und Meldung.
func (r *runner) convert(err error) error {
	var se *Error
	if errors.As(err, &se) {
		return se
	}
	var ie *goja.InterruptedError
	if errors.As(err, &ie) {
		return scriptErr("timeout", fmt.Sprintf("Das Server-Skript hat länger als %d Sekunden gebraucht und wurde abgebrochen.", int(Timeout.Seconds())))
	}
	var ex *goja.Exception
	if errors.As(err, &ex) {
		code, msg := "script", ex.Error()
		if obj, ok := ex.Value().(*goja.Object); ok {
			if m := obj.Get("message"); m != nil && !goja.IsUndefined(m) {
				msg = m.String()
			}
			if c := obj.Get("code"); c != nil && !goja.IsUndefined(c) && c.String() != "" {
				code = c.String()
			}
		}
		return scriptErr(code, msg)
	}
	var cerr *goja.CompilerSyntaxError
	if errors.As(err, &cerr) {
		return scriptErr("script", "Fehler im Server-Skript: "+cerr.Error())
	}
	return err
}

// throw wirft im Skript einen Fehler mit code und message.
func (r *runner) throw(err error) {
	var se *Error
	if !errors.As(err, &se) {
		// interne Fehler nicht im Detail an das Skript geben
		se = scriptErr("server", "Interner Fehler.")
		r.api.Log("interner Fehler: " + err.Error())
	}
	obj := r.vm.NewGoError(se)
	obj.Set("code", se.Code)
	obj.Set("message", se.Message)
	panic(obj)
}

// require lädt ein Modul aus server/ (CommonJS-Stil, jedes Modul einmal je Lauf).
func (r *runner) require(p string) (*goja.Object, error) {
	if m, ok := r.modules[p]; ok {
		return m.Get("exports").ToObject(r.vm), nil
	}
	b, err := r.src(r.ctx, p)
	if err != nil {
		return nil, scriptErr("script", "Server-Skript „"+p+"“ nicht gefunden.")
	}
	prog, err := goja.Compile(p, wrap(string(b)), true)
	if err != nil {
		return nil, scriptErr("script", "Fehler im Server-Skript: "+err.Error())
	}
	fnVal, err := r.vm.RunProgram(prog)
	if err != nil {
		return nil, err
	}
	fn, _ := goja.AssertFunction(fnVal)
	module := r.vm.NewObject()
	exports := r.vm.NewObject()
	module.Set("exports", exports)
	r.modules[p] = module
	dir := path.Dir(p)
	req := func(call goja.FunctionCall) goja.Value {
		name := call.Argument(0).String()
		target := path.Clean(path.Join(dir, name))
		if !strings.HasPrefix(target, Dir) || path.Ext(target) != ".js" {
			r.throw(scriptErr("script", "require: nur .js-Dateien aus "+Dir+" („"+name+"“)."))
		}
		m, err := r.require(target)
		if err != nil {
			var ex *goja.Exception
			if errors.As(err, &ex) {
				panic(ex.Value())
			}
			r.throw(r.convert(err))
		}
		return m
	}
	if _, err := fn(goja.Undefined(), exports, r.vm.ToValue(req), module); err != nil {
		return nil, err
	}
	return module.Get("exports").ToObject(r.vm), nil
}

// call ruft eine API-Funktion auf und gibt ihr Ergebnis als JS-Wert zurück.
func (r *runner) call(service bool, method string, params any) goja.Value {
	r.calls++
	if r.calls > MaxCalls {
		r.throw(scriptErr("too_many_calls", fmt.Sprintf("Mehr als %d Zugriffe in einem Aufruf.", MaxCalls)))
	}
	b, err := json.Marshal(params)
	if err != nil {
		r.throw(scriptErr("bad_request", "Parameter lassen sich nicht umwandeln."))
	}
	res, err := r.api.Call(r.ctx, service, method, b)
	if err != nil {
		if r.ctx.Err() != nil {
			panic(&goja.InterruptedError{})
		}
		r.throw(err)
	}
	var v any
	if err := json.Unmarshal(res, &v); err != nil {
		r.throw(err)
	}
	return r.vm.ToValue(v)
}

func export(v goja.Value) any {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil
	}
	return v.Export()
}

// dataFuncs sind list, get, create, update, remove, users, roles für mwf bzw. mwf.asService.
func (r *runner) dataFuncs(obj *goja.Object, service bool) {
	set := func(name string, f func(c goja.FunctionCall) goja.Value) { obj.Set(name, f) }
	set("list", func(c goja.FunctionCall) goja.Value {
		p := map[string]any{}
		if o, ok := export(c.Argument(1)).(map[string]any); ok {
			p = o
		}
		p["table"] = c.Argument(0).String()
		return r.call(service, "list", p)
	})
	set("get", func(c goja.FunctionCall) goja.Value {
		return r.call(service, "get", map[string]any{"table": c.Argument(0).String(), "id": export(c.Argument(1))})
	})
	set("create", func(c goja.FunctionCall) goja.Value {
		return r.call(service, "create", map[string]any{"table": c.Argument(0).String(), "values": export(c.Argument(1))})
	})
	set("update", func(c goja.FunctionCall) goja.Value {
		return r.call(service, "update", map[string]any{"table": c.Argument(0).String(), "id": export(c.Argument(1)), "values": export(c.Argument(2))})
	})
	set("remove", func(c goja.FunctionCall) goja.Value {
		return r.call(service, "delete", map[string]any{"table": c.Argument(0).String(), "id": export(c.Argument(1))})
	})
	set("users", func(c goja.FunctionCall) goja.Value {
		p := map[string]any{}
		if o, ok := export(c.Argument(0)).(map[string]any); ok {
			p = o
		}
		return r.call(service, "users", p)
	})
	set("roles", func(c goja.FunctionCall) goja.Value { return r.call(service, "roles", map[string]any{}) })
}

func (r *runner) setup(info map[string]any) error {
	vm := r.vm
	mwf := vm.NewObject()
	for k, v := range info {
		b, _ := json.Marshal(v)
		var x any
		json.Unmarshal(b, &x)
		mwf.Set(k, vm.ToValue(x))
	}
	r.dataFuncs(mwf, false)
	svc := vm.NewObject()
	r.dataFuncs(svc, true)
	mwf.Set("asService", svc)
	mwf.Set("settings", func(goja.FunctionCall) goja.Value { return r.call(false, "settings", map[string]any{}) })

	crypto := vm.NewObject()
	crypto.Set("encrypt", func(c goja.FunctionCall) goja.Value {
		if v := export(c.Argument(0)); v == nil {
			return goja.Null()
		}
		s, err := r.api.Encrypt(c.Argument(0).String())
		if err != nil {
			r.throw(err)
		}
		return vm.ToValue(s)
	})
	crypto.Set("decrypt", func(c goja.FunctionCall) goja.Value {
		if v := export(c.Argument(0)); v == nil {
			return goja.Null()
		}
		s, err := r.api.Decrypt(c.Argument(0).String())
		if err != nil {
			r.throw(err)
		}
		return vm.ToValue(s)
	})
	crypto.Set("hmac", func(c goja.FunctionCall) goja.Value { return vm.ToValue(r.api.HMAC(c.Argument(0).String())) })
	mwf.Set("crypto", crypto)

	// mwf.error("Meldung") oder mwf.error("code", "Meldung") bricht ab und meldet dem Widget den Fehler
	mwf.Set("error", func(c goja.FunctionCall) goja.Value {
		code, msg := "script", c.Argument(0).String()
		if len(c.Arguments) > 1 {
			code, msg = c.Argument(0).String(), c.Argument(1).String()
		}
		r.throw(scriptErr(code, msg))
		return nil
	})
	vm.Set("mwf", mwf)

	console := vm.NewObject()
	logf := func(c goja.FunctionCall) goja.Value {
		parts := make([]string, len(c.Arguments))
		for i, a := range c.Arguments {
			parts[i] = a.String()
		}
		r.api.Log(strings.Join(parts, " "))
		return goja.Undefined()
	}
	console.Set("log", logf)
	console.Set("error", logf)
	vm.Set("console", console)
	return nil
}
