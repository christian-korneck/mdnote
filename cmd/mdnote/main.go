// Command mdnote serves the notes web UI and REST API over HTTP.
// It is pure Go and builds as a fully static binary (CGO_ENABLED=0).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/christian-korneck/mdnote/internal/app"
	"github.com/christian-korneck/mdnote/internal/browser"
	"github.com/christian-korneck/mdnote/internal/server"
)

func main() {
	var cfg app.Config
	cfg.RegisterFlags(flag.CommandLine)
	addrFlag := flag.String("addr", "127.0.0.1:9090", "HTTP listen address (e.g. 127.0.0.1:9090 or 0.0.0.0:9090)")
	openFlag := flag.Bool("open", true, "Open the web UI in the default browser on start")
	allowHostFlag := flag.String("allow-host", "", "Extra host names the server may be reached by, comma-separated (e.g. notes.example.com behind a reverse proxy); IP addresses, localhost and this machine's hostname are always allowed")
	apiTokenFlag := flag.String("api-token", "", "API token for the web UI and REST API (default: $MDNOTE_API_TOKEN, then \"apiToken\" in the config file, else a random token on every start)")
	cfg.ParseArgs(flag.CommandLine, os.Args[1:])

	st, handler, err := app.Open(cfg)
	if err != nil {
		log.Fatal(err)
	}

	// Bind before opening the browser: a busy port fails here, and the URL gets the real port.
	ln, err := net.Listen("tcp", *addrFlag)
	if err != nil {
		log.Fatalf("http server error: %v", err)
	}

	var extraHosts []string
	for _, h := range strings.Split(*allowHostFlag, ",") {
		if h = strings.TrimSpace(h); h != "" {
			extraHosts = append(extraHosts, h)
		}
	}

	cfgToken, err := cfg.SettingsFile().APIToken()
	if err != nil {
		log.Printf("config file: %v (its apiToken is ignored)", err)
	}
	token, tokenSource := resolveToken(*apiTokenFlag, os.Getenv("MDNOTE_API_TOKEN"), cfgToken)
	randomToken := token == ""
	if randomToken {
		token = server.RandomToken()
	}
	// The cookie name includes the port: browsers share cookies between ports.
	auth := server.NewAuth(token, fmt.Sprintf("mdnote_auth_%d", ln.Addr().(*net.TCPAddr).Port))

	httpServer := &http.Server{
		// Reject DNS-rebinding and cross-site requests (server.Protect), then require
		// the API token (server.Auth).
		Handler:           server.Protect(auth.Handler(handler), extraHosts),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server error: %v", err)
		}
	}()

	url := uiURL(ln.Addr())
	scope := ""
	if tcp, ok := ln.Addr().(*net.TCPAddr); ok && tcp.IP.IsUnspecified() {
		scope = " (all interfaces)"
	}
	log.Printf("mdnote listening on %s%s (notes dir: %s)", url, scope, st.RootDir())
	if randomToken {
		log.Printf("API token (random, new on every start): %s", token)
	} else {
		log.Printf("API token: from %s", tokenSource)
	}

	// The browser gets a one-time login code, not the token: a browser's command line
	// is visible to other local users.
	opened := false
	if *openFlag {
		if err := browser.Open(url + "?login=" + auth.LoginCode()); err != nil {
			log.Printf("could not open a browser (%v)", err)
		} else {
			opened = true
		}
	}
	if !opened {
		shown := token
		if !randomToken {
			shown = "<your API token>"
		}
		log.Printf("log in at %s?token=%s", url, shown)
	}

	<-ctx.Done()
	log.Println("Shutting down mdnote...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
}

// resolveToken picks the API token: the -api-token flag, then $MDNOTE_API_TOKEN, then
// "apiToken" in the config file. It returns "" if none is set (mdnote then uses a
// random token), and the source for the startup log.
func resolveToken(flagVal, envVal, configVal string) (token, source string) {
	for _, c := range []struct{ val, src string }{
		{flagVal, "-api-token"},
		{envVal, "MDNOTE_API_TOKEN"},
		{configVal, "config file"},
	} {
		if t := strings.TrimSpace(c.val); t != "" {
			return t, c.src
		}
	}
	return "", ""
}

// uiURL returns the URL of the web UI for a listener address. A wildcard bind
// (0.0.0.0 or ::) is opened via localhost.
func uiURL(addr net.Addr) string {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok {
		return "http://" + addr.String() + "/"
	}
	host := "localhost"
	if !tcp.IP.IsUnspecified() {
		host = tcp.IP.String()
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(tcp.Port)) + "/"
}
