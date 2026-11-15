package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/christian-korneck/mdnote/internal/store"
)

func TestOpenWindowHandler(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantPath   string
		wantURL    string
	}{
		{"note", `{"path":"projects/a b.md"}`, http.StatusNoContent, "projects/a b.md", "/?popout=projects%2Fa+b.md"},
		{"adds .md", `{"path":"todo"}`, http.StatusNoContent, "todo.md", "/?popout=todo.md"},
		{"empty", `{"path":""}`, http.StatusBadRequest, "", ""},
		{"traversal", `{"path":"../outside.md"}`, http.StatusBadRequest, "", ""},
		{"hidden dir", `{"path":".git/config.md"}`, http.StatusBadRequest, "", ""},
		{"bad json", `{`, http.StatusBadRequest, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath, gotURL string
			h := openWindowHandler(st, func(path, pageURL string) {
				gotPath, gotURL = path, pageURL
			})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/open-window", strings.NewReader(tc.body)))
			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if gotPath != tc.wantPath || gotURL != tc.wantURL {
				t.Errorf("opened (%q, %q), want (%q, %q)", gotPath, gotURL, tc.wantPath, tc.wantURL)
			}
		})
	}
}
