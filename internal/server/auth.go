package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"
)

// loginCodeTTL is how long a one-time login code stays valid.
const loginCodeTTL = 2 * time.Minute

// Auth requires the API token on every request to the mdnote server (the desktop
// app opens no port and doesn't use it). A request passes with either
//
//   - an "Authorization: Bearer <token>" header (scripts, curl), or
//   - the login cookie, which a browser gets by opening /?token=<token> or a
//     one-time /?login=<code> URL.
//
// mdnote opens the browser with a one-time code rather than the token, because a
// browser's command line (URL included) is visible to other local users.
type Auth struct {
	token      []byte
	cookieName string
	cookieVal  string // hex SHA-256 of the token: valid in any cookie, and not the token itself
	now        func() time.Time

	mu    sync.Mutex
	codes map[string]time.Time // one-time login code -> expiry
}

// NewAuth returns an Auth for token. cookieName should be unique per server
// (browsers share cookies between ports of the same host).
func NewAuth(token, cookieName string) *Auth {
	sum := sha256.Sum256([]byte(token))
	return &Auth{
		token:      []byte(token),
		cookieName: cookieName,
		cookieVal:  hex.EncodeToString(sum[:]),
		now:        time.Now,
		codes:      map[string]time.Time{},
	}
}

// LoginCode returns a new one-time login code for /?login=<code>. It can be used
// once, within two minutes.
func (a *Auth) LoginCode() string {
	code := RandomToken()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dropExpired()
	a.codes[code] = a.now().Add(loginCodeTTL)
	return code
}

// RandomToken returns 16 random bytes as hex.
func RandomToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return hex.EncodeToString(b)
}

// Handler wraps h so that only authenticated requests reach it.
func (a *Auth) Handler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && (q.Has("token") || q.Has("login")) {
			if (q.Has("token") && a.validToken(q.Get("token"))) || (q.Has("login") && a.useLoginCode(q.Get("login"))) {
				a.setCookie(w)
				q.Del("token")
				q.Del("login")
				u := *r.URL
				u.RawQuery = q.Encode()
				http.Redirect(w, r, u.RequestURI(), http.StatusSeeOther)
				return
			}
			unauthorized(w)
			return
		}
		if a.validBearer(r) || a.validCookie(r) {
			h.ServeHTTP(w, r)
			return
		}
		unauthorized(w)
	})
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="mdnote"`)
	http.Error(w, "unauthorized: open /?token=<API token> in the browser, or send \"Authorization: Bearer <API token>\" (mdnote prints the token on start)", http.StatusUnauthorized)
}

func (a *Auth) validToken(t string) bool {
	return t != "" && subtle.ConstantTimeCompare([]byte(t), a.token) == 1
}

func (a *Auth) validBearer(r *http.Request) bool {
	scheme, t, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	return ok && strings.EqualFold(scheme, "Bearer") && a.validToken(strings.TrimSpace(t))
}

func (a *Auth) validCookie(r *http.Request) bool {
	c, err := r.Cookie(a.cookieName)
	return err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(a.cookieVal)) == 1
}

func (a *Auth) setCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     a.cookieName,
		Value:    a.cookieVal,
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// useLoginCode consumes a valid, unexpired one-time code.
func (a *Auth) useLoginCode(code string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dropExpired()
	for c := range a.codes {
		if subtle.ConstantTimeCompare([]byte(c), []byte(code)) == 1 {
			delete(a.codes, c)
			return true
		}
	}
	return false
}

func (a *Auth) dropExpired() {
	now := a.now()
	for c, exp := range a.codes {
		if now.After(exp) {
			delete(a.codes, c)
		}
	}
}
