// Package browser opens a URL in the user's default browser. It is pure Go (os/exec),
// so the static mdnote server can use it; the desktop app uses Wails' own opener.
package browser

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
)

// ErrNoDisplay is returned on Linux and other Unix systems when there is no
// graphical session (neither DISPLAY nor WAYLAND_DISPLAY is set), e.g. over SSH.
// xdg-open could otherwise fall back to a terminal browser.
var ErrNoDisplay = errors.New("no graphical session (DISPLAY and WAYLAND_DISPLAY are unset)")

// Open starts the system's URL opener for url and returns without waiting for it.
func Open(url string) error {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" &&
		os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return ErrNoDisplay
	}
	name, args := command(runtime.GOOS, url)
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() //nolint:errcheck // only reaps the child process
	return nil
}

// command returns the opener command for goos (same commands as Wails' internal/browser).
func command(goos, url string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{url}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		return "xdg-open", []string{url}
	}
}
