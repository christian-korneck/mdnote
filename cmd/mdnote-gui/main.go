// Command mdnote-gui is the mdnote desktop app. It shows the embedded web UI in a
// native Wails window. Wails' asset server hands every request from the window
// (UI files and /api calls alike) to the same http.Handler that cmd/mdnote serves
// over HTTP, so this process never listens on a port.
//
// Wails needs cgo on Linux (GTK4 + WebKitGTK 6.0) and macOS, but not on Windows.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/christian-korneck/mdnote/internal/app"
)

func main() {
	var cfg app.Config
	cfg.RegisterFlags(flag.CommandLine)
	cfg.ParseArgs(flag.CommandLine, os.Args[1:])

	st, handler, err := app.Open(cfg)
	if err != nil {
		log.Fatal(err)
	}

	// gui is assigned below; the open-url route only runs once the window is up.
	var gui *application.App
	mux := http.NewServeMux()
	mux.Handle("/", handler)
	mux.Handle("POST /api/open-url", openURLHandler(func(u string) error {
		return gui.Browser.OpenURL(u)
	}))
	mux.Handle("POST /api/copy-text", copyTextHandler(func(text string) bool {
		return gui.Clipboard.SetText(text)
	}))
	// Pop-out windows. Wails creates the window on the main thread and quits only
	// after the last window is closed.
	mux.Handle("POST /api/open-window", openWindowHandler(st, func(path, pageURL string) {
		gui.Window.NewWithOptions(application.WebviewWindowOptions{
			Title:  path + " – mdnote",
			Width:  1000,
			Height: 800,
			URL:    pageURL,
		})
	}))

	gui = application.New(application.Options{
		Name:   "mdnote",
		Assets: application.AssetOptions{Handler: mux},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		Linux: application.LinuxOptions{
			ProgramName: "mdnote-gui",
		},
	})

	gui.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "mdnote",
		Width:  1400,
		Height: 900,
		URL:    "/",
	})

	if err := gui.Run(); err != nil {
		log.Fatal(err)
	}
}
