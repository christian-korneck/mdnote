package main

import (
	"encoding/json"
	"net/http"
)

// copyTextHandler serves POST /api/copy-text ({"text": "..."}), a desktop-only route.
// WebKit lets page scripts write the clipboard only during a real user gesture, with
// differences between platforms, so app.js copies through Wails' native clipboard
// (set) instead.
func copyTextHandler(set func(text string) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON payload")
			return
		}
		if req.Text == "" {
			writeError(w, http.StatusBadRequest, "nothing to copy")
			return
		}
		if !set(req.Text) {
			writeError(w, http.StatusInternalServerError, "could not set the clipboard")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
