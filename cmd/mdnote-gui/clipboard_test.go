package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCopyTextHandler(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		setOK      bool
		wantStatus int
		wantSet    string
	}{
		{"text", `{"text":"/home/me/notes/a.md"}`, true, http.StatusNoContent, "/home/me/notes/a.md"},
		{"clipboard error", `{"text":"x"}`, false, http.StatusInternalServerError, "x"},
		{"empty", `{"text":""}`, true, http.StatusBadRequest, ""},
		{"bad json", `{`, true, http.StatusBadRequest, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			h := copyTextHandler(func(text string) bool {
				got = text
				return tc.setOK
			})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/copy-text", strings.NewReader(tc.body)))
			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if got != tc.wantSet {
				t.Errorf("clipboard set to %q, want %q", got, tc.wantSet)
			}
		})
	}
}
