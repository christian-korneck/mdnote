package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestProtectHost(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	h := Protect(ok, []string{"notes.example.com", "Proxy.Example.org:8443"})
	hostname, _ := os.Hostname()

	tests := []struct {
		host string
		want int
	}{
		{"127.0.0.1:9090", http.StatusOK},
		{"localhost:9090", http.StatusOK},
		{"LOCALHOST", http.StatusOK},
		{"app.localhost:9090", http.StatusOK},
		{"[::1]:9090", http.StatusOK},
		{"192.168.1.20:9090", http.StatusOK},
		{"notes.example.com", http.StatusOK},
		{"notes.example.com.:9090", http.StatusOK},
		{"proxy.example.org", http.StatusOK},
		{"evil.example:9090", http.StatusForbidden},
		{"notes.example.com.evil.example", http.StatusForbidden},
		{"localhost.evil.example", http.StatusForbidden},
		{"", http.StatusForbidden},
	}
	if hostname != "" {
		tests = append(tests,
			struct {
				host string
				want int
			}{hostname + ":9090", http.StatusOK},
			struct {
				host string
				want int
			}{hostname + ".local", http.StatusOK},
		)
	}
	for _, tc := range tests {
		req := httptest.NewRequest(http.MethodGet, "/api/notes", nil)
		req.Host = tc.host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("Host %q: status %d, want %d", tc.host, rec.Code, tc.want)
		}
	}
}

func TestProtectCrossOrigin(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	h := Protect(ok, nil)

	tests := []struct {
		name    string
		method  string
		headers map[string]string
		want    int
	}{
		{"POST from another origin", http.MethodPost, map[string]string{"Origin": "http://evil.example"}, http.StatusForbidden},
		{"PUT marked cross-site", http.MethodPut, map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"DELETE marked same-site (other port)", http.MethodDelete, map[string]string{"Sec-Fetch-Site": "same-site"}, http.StatusForbidden},
		{"POST same origin", http.MethodPost, map[string]string{"Origin": "http://127.0.0.1:9090"}, http.StatusOK},
		{"PATCH marked same-origin", http.MethodPatch, map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://127.0.0.1:9090"}, http.StatusOK},
		{"POST without browser headers (curl)", http.MethodPost, nil, http.StatusOK},
		{"GET from another origin", http.MethodGet, map[string]string{"Origin": "http://evil.example", "Sec-Fetch-Site": "cross-site"}, http.StatusOK},
	}
	for _, tc := range tests {
		req := httptest.NewRequest(tc.method, "/api/notes", nil)
		req.Host = "127.0.0.1:9090"
		for k, v := range tc.headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s: status %d, want %d", tc.name, rec.Code, tc.want)
		}
	}
}
