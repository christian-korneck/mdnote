package main

import (
	"encoding/json"
	"net/http"
	"net/url"
)

// openURLHandler serves POST /api/open-url ({"url": "..."}), a desktop-only route.
// The webview can't open links itself (a plain <a href> would navigate the app
// window away), so app.js sends external links here and they open in the
// system browser. Only http(s) and mailto URLs are accepted.
func openURLHandler(open func(string) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON payload")
			return
		}
		u, err := url.Parse(req.URL)
		if err != nil || !allowedExternalURL(u) {
			writeError(w, http.StatusBadRequest, "only http, https and mailto URLs can be opened")
			return
		}
		if err := open(u.String()); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func allowedExternalURL(u *url.URL) bool {
	switch u.Scheme {
	case "http", "https":
		return u.Host != ""
	case "mailto":
		return u.Opaque != ""
	default:
		return false
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
