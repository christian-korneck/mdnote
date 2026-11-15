package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuth(t *testing.T) {
	a := NewAuth("s3cret", "mdnote_auth_9090")
	h := a.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	do := func(method, target string, setup func(*http.Request)) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		if setup != nil {
			setup(req)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	bearer := func(tok string) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) }
	}

	if rec := do("GET", "/api/notes", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("no credentials: %d, want 401", rec.Code)
	}
	if rec := do("GET", "/api/notes", bearer("s3cret")); rec.Code != http.StatusOK {
		t.Errorf("valid bearer: %d, want 200", rec.Code)
	}
	if rec := do("POST", "/api/notes", func(r *http.Request) { r.Header.Set("Authorization", "bearer s3cret") }); rec.Code != http.StatusOK {
		t.Errorf("lowercase bearer scheme: %d, want 200", rec.Code)
	}
	if rec := do("GET", "/api/notes", bearer("wrong")); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong bearer: %d, want 401", rec.Code)
	}

	// ?token= sets the cookie and redirects to the same URL without the token.
	rec := do("GET", "/notes?popout=a.md&token=s3cret", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/notes?popout=a.md" {
		t.Fatalf("?token=: %d Location %q, want 303 to /notes?popout=a.md", rec.Code, rec.Header().Get("Location"))
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "mdnote_auth_9090" || !cookies[0].HttpOnly || strings.Contains(cookies[0].Value, "s3cret") {
		t.Fatalf("login cookie = %+v", cookies)
	}
	withCookie := func(r *http.Request) { r.AddCookie(cookies[0]) }
	if rec := do("GET", "/api/notes", withCookie); rec.Code != http.StatusOK {
		t.Errorf("valid cookie: %d, want 200", rec.Code)
	}
	if rec := do("GET", "/api/notes", func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: "mdnote_auth_9090", Value: "forged"})
	}); rec.Code != http.StatusUnauthorized {
		t.Errorf("forged cookie: %d, want 401", rec.Code)
	}
	if rec := do("GET", "/?token=wrong", nil); rec.Code != http.StatusUnauthorized || len(rec.Result().Cookies()) != 0 {
		t.Errorf("wrong ?token=: %d, want 401 without cookie", rec.Code)
	}
	if rec := do("POST", "/api/notes?token=s3cret", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("POST with ?token=: %d, want 401", rec.Code)
	}

	// One-time login codes: usable once, and only within the TTL.
	code := a.LoginCode()
	if code == "" || strings.Contains(code, "s3cret") {
		t.Fatalf("login code %q", code)
	}
	if rec := do("GET", "/?login="+code, nil); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" || len(rec.Result().Cookies()) != 1 {
		t.Errorf("first use of login code: %d Location %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := do("GET", "/?login="+code, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("second use of login code: %d, want 401", rec.Code)
	}
	now := time.Now()
	a.now = func() time.Time { return now }
	old := a.LoginCode()
	a.now = func() time.Time { return now.Add(loginCodeTTL + time.Second) }
	if rec := do("GET", "/?login="+old, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("expired login code: %d, want 401", rec.Code)
	}
}
