package main

import (
	"net"
	"testing"
)

func TestUIURL(t *testing.T) {
	tests := []struct {
		addr string
		want string
	}{
		{"127.0.0.1:9090", "http://127.0.0.1:9090/"},
		{"0.0.0.0:9090", "http://localhost:9090/"},
		{"[::]:9090", "http://localhost:9090/"},
		{"[::1]:9090", "http://[::1]:9090/"},
		{"192.168.1.20:8080", "http://192.168.1.20:8080/"},
	}
	for _, tt := range tests {
		addr, err := net.ResolveTCPAddr("tcp", tt.addr)
		if err != nil {
			t.Fatalf("ResolveTCPAddr(%q): %v", tt.addr, err)
		}
		if got := uiURL(addr); got != tt.want {
			t.Errorf("uiURL(%s) = %q, want %q", tt.addr, got, tt.want)
		}
	}
}

func TestResolveToken(t *testing.T) {
	tests := []struct {
		flag, env, cfg   string
		wantTok, wantSrc string
	}{
		{"f", "e", "c", "f", "-api-token"},
		{"", "e", "c", "e", "MDNOTE_API_TOKEN"},
		{"", "", "c", "c", "config file"},
		{" ", " ", " c ", "c", "config file"},
		{"", "", "", "", ""},
	}
	for _, tt := range tests {
		tok, src := resolveToken(tt.flag, tt.env, tt.cfg)
		if tok != tt.wantTok || src != tt.wantSrc {
			t.Errorf("resolveToken(%q, %q, %q) = %q, %q; want %q, %q", tt.flag, tt.env, tt.cfg, tok, src, tt.wantTok, tt.wantSrc)
		}
	}
}
