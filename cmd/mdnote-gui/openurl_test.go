package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenURLHandler(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		openErr    error
		wantStatus int
		wantOpened string
	}{
		{"https", `{"url":"https://example.com/a?b=c"}`, nil, http.StatusNoContent, "https://example.com/a?b=c"},
		{"http", `{"url":"http://example.com"}`, nil, http.StatusNoContent, "http://example.com"},
		{"mailto", `{"url":"mailto:someone@example.com"}`, nil, http.StatusNoContent, "mailto:someone@example.com"},
		{"file scheme rejected", `{"url":"file:///etc/passwd"}`, nil, http.StatusBadRequest, ""},
		{"javascript rejected", `{"url":"javascript:alert(1)"}`, nil, http.StatusBadRequest, ""},
		{"relative rejected", `{"url":"other-note.md"}`, nil, http.StatusBadRequest, ""},
		{"hostless https rejected", `{"url":"https:///path"}`, nil, http.StatusBadRequest, ""},
		{"bad json", `{`, nil, http.StatusBadRequest, ""},
		{"opener error", `{"url":"https://example.com"}`, errors.New("no browser"), http.StatusInternalServerError, "https://example.com"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var opened string
			h := openURLHandler(func(u string) error {
				opened = u
				return tc.openErr
			})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/open-url", strings.NewReader(tc.body)))
			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if opened != tc.wantOpened {
				t.Errorf("opened %q, want %q", opened, tc.wantOpened)
			}
		})
	}
}
