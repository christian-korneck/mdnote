// Package app holds the startup shared by the mdnote server and the mdnote-gui
// desktop app: common flags, opening (and optionally seeding) the notes
// directory, and building the http.Handler that serves the web UI and REST API.
package app

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/christian-korneck/mdnote/internal/render"
	"github.com/christian-korneck/mdnote/internal/server"
	"github.com/christian-korneck/mdnote/internal/settings"
	"github.com/christian-korneck/mdnote/internal/store"
)

// Config holds the options shared by every mdnote frontend.
type Config struct {
	Dir         string
	Seed        int
	SeedFolders int
	NoWelcome   bool
	// SettingsPath is the UI settings file shared by both frontends. Empty means
	// settings.DefaultPath(); tests set it to keep away from the user's real file.
	SettingsPath string
}

// RegisterFlags registers the shared command-line flags on fs and sets its usage
// text, which names the required notes directory argument.
func (c *Config) RegisterFlags(fs *flag.FlagSet) {
	fs.IntVar(&c.Seed, "seed", 0, "Optional number of sample notes to generate for scale testing")
	fs.IntVar(&c.SeedFolders, "seed-folders", 0, "Optional number of sample folders to generate for scale testing")
	fs.BoolVar(&c.NoWelcome, "no-welcome", false, "Don't create welcome.md in an empty notes directory")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: %s [flags] <notes-dir>\n\nFlags:\n", filepath.Base(fs.Name()))
		fs.PrintDefaults()
	}
}

// ParseArgs parses args (without the program name) into fs and takes the notes
// directory from the single positional argument after the flags. Like
// fs.Parse, a bad command line prints the error and usage and, for
// flag.ExitOnError, exits with status 2.
func (c *Config) ParseArgs(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return err
	}
	var err error
	switch fs.NArg() {
	case 1:
		c.Dir = fs.Arg(0)
		return nil
	case 0:
		err = errors.New("missing notes directory")
	default:
		err = fmt.Errorf("unexpected argument: %s", fs.Arg(1))
	}
	fmt.Fprintln(fs.Output(), err)
	fs.Usage()
	if fs.ErrorHandling() == flag.ExitOnError {
		os.Exit(2)
	}
	return err
}

// Open prepares the notes directory and returns its store together with the
// handler for the web UI and REST API. It seeds sample folders and notes when
// requested, and otherwise writes welcome.md into an empty notes directory
// (unless NoWelcome is set).
func Open(cfg Config) (*store.Store, http.Handler, error) {
	st, err := store.New(cfg.Dir)
	if err != nil {
		return nil, nil, fmt.Errorf("initialize store: %w", err)
	}

	var customFolders []string
	if cfg.SeedFolders > 0 {
		customFolders, err = seedFolders(st, cfg.SeedFolders)
		if err != nil {
			return nil, nil, fmt.Errorf("seed folders: %w", err)
		}
		log.Printf("Seeded %d folders in %s", len(customFolders), st.RootDir())
	}

	if cfg.Seed > 0 {
		if err := seedNotes(st, cfg.Seed, customFolders); err != nil {
			return nil, nil, fmt.Errorf("seed notes: %w", err)
		}
		log.Printf("Seeded %d notes in %s", cfg.Seed, st.RootDir())
	} else if cfg.SeedFolders == 0 && !cfg.NoWelcome {
		// Create welcome.md if the directory currently has 0 notes
		res, err := st.Search(store.QueryOptions{Limit: 1})
		if err == nil && res.Total == 0 {
			_, _ = st.Save("welcome.md", []byte(welcomeMarkdown), true)
		}
	}

	return st, server.New(st, render.New(), cfg.SettingsFile()), nil
}

// SettingsFile returns the settings file shared by both frontends: SettingsPath, or
// the default path. If no config directory can be found, settings can't be saved.
func (c Config) SettingsFile() *settings.File {
	path := c.SettingsPath
	if path == "" {
		var err error
		if path, err = settings.DefaultPath(); err != nil {
			log.Printf("settings won't be saved: %v", err)
		}
	}
	return settings.NewFile(path)
}
