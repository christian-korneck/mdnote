package browser

import (
	"errors"
	"reflect"
	"runtime"
	"testing"
)

func TestCommand(t *testing.T) {
	const u = "http://127.0.0.1:9090/"
	tests := []struct {
		goos string
		name string
		args []string
	}{
		{"darwin", "open", []string{u}},
		{"windows", "rundll32", []string{"url.dll,FileProtocolHandler", u}},
		{"linux", "xdg-open", []string{u}},
		{"freebsd", "xdg-open", []string{u}},
	}
	for _, tt := range tests {
		name, args := command(tt.goos, u)
		if name != tt.name || !reflect.DeepEqual(args, tt.args) {
			t.Errorf("command(%q) = %q %q, want %q %q", tt.goos, name, args, tt.name, tt.args)
		}
	}
}

func TestOpenWithoutDisplay(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		t.Skip("display check only applies to Linux and other Unix systems")
	}
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	if err := Open("http://127.0.0.1:9090/"); !errors.Is(err, ErrNoDisplay) {
		t.Fatalf("Open without a display = %v, want ErrNoDisplay", err)
	}
}
