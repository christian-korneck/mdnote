package main

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/christian-korneck/mdnote/internal/store"
)

// openWindowHandler serves POST /api/open-window ({"path": "folder/note.md"}), a
// desktop-only route. The webview can't open windows itself, so app.js asks here to
// pop a note out: open gets the cleaned note path and the page URL for the new
// window (/?popout=<path>, which shows just that note).
func openWindowHandler(st *store.Store, open func(path, pageURL string)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON payload")
			return
		}
		clean, _, err := st.CleanNotePath(req.Path)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		open(clean, "/?popout="+url.QueryEscape(clean))
		w.WriteHeader(http.StatusNoContent)
	})
}
