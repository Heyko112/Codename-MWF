package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChainOrder(t *testing.T) {
	var order []string
	mw := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}
	h := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { order = append(order, "handler") }), mw("a"), mw("b"))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))

	want := []string{"a", "b", "handler"}
	if len(order) != len(want) {
		t.Fatalf("Reihenfolge %v, erwartet %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("Reihenfolge %v, erwartet %v", order, want)
		}
	}
}

func TestRecover(t *testing.T) {
	h := Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Status %d, erwartet 500", rec.Code)
	}
}

func TestRequestIDAndSecurityHeaders(t *testing.T) {
	var seen string
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = RequestIDFrom(r.Context()) }), RequestID, SecurityHeaders)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if seen == "" || rec.Header().Get("X-Request-ID") != seen {
		t.Fatalf("Request-ID fehlt oder stimmt nicht: Header %q, Kontext %q", rec.Header().Get("X-Request-ID"), seen)
	}
	for _, name := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options"} {
		if rec.Header().Get(name) == "" {
			t.Errorf("Header %s fehlt", name)
		}
	}
}

func TestCSRFBlocksCrossSitePost(t *testing.T) {
	h := CSRF(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Cross-Site-POST: Status %d, erwartet 403", rec.Code)
	}

	req = httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Same-Origin-POST: Status %d, erwartet 200", rec.Code)
	}
}
