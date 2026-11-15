// Package settings keeps the UI settings (theme, split divider position, autosave)
// in one JSON config file in the user's profile. mdnote and mdnote-gui share the
// file. It is read fresh on every Load, so a change made by one binary shows up in
// the other after a page reload or restart, and it is written only when a setting
// actually changes.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Settings are the persisted UI settings.
type Settings struct {
	Theme      string  `json:"theme"`      // "system", "light" or "dark"
	SplitRatio float64 `json:"splitRatio"` // editor pane width in percent of the split view (5–95)
	Autosave   bool    `json:"autosave"`
}

// Defaults returns the settings used when the config file or a field is missing.
func Defaults() Settings {
	return Settings{Theme: "system", SplitRatio: 50, Autosave: true}
}

// Patch is a partial update: only non-nil fields are changed.
type Patch struct {
	Theme      *string  `json:"theme"`
	SplitRatio *float64 `json:"splitRatio"`
	Autosave   *bool    `json:"autosave"`
}

var (
	// ErrInvalid is returned by Update for out-of-range or unknown values.
	ErrInvalid = errors.New("invalid setting")
	// ErrNoPath is returned by Update when there is no config file location.
	ErrNoPath = errors.New("no config file location (user config directory not found)")
)

// DefaultPath returns <user config dir>/mdnote/config.json, e.g.
// ~/.config/mdnote/config.json on Linux.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mdnote", "config.json"), nil
}

// File is the config file at a fixed path. An empty path means settings can't
// be saved: Load returns the defaults and Update returns ErrNoPath.
type File struct {
	path string
	mu   sync.Mutex
}

// NewFile returns the config file at path. Nothing is read or created yet.
func NewFile(path string) *File {
	return &File{path: path}
}

// Path returns the config file path ("" if settings can't be saved).
func (f *File) Path() string {
	return f.path
}

// Load reads the config file. A missing file gives the defaults. An unreadable
// or unparsable file gives the defaults and an error. Invalid values fall back
// to their defaults.
func (f *File) Load() (Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.load()
}

func (f *File) load() (Settings, error) {
	s := Defaults()
	if f.path == "" {
		return s, nil
	}
	data, err := os.ReadFile(f.path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return Defaults(), err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return Defaults(), fmt.Errorf("parse %s: %w", f.path, err)
	}
	if !validTheme(s.Theme) {
		s.Theme = Defaults().Theme
	}
	if !validRatio(s.SplitRatio) {
		s.SplitRatio = Defaults().SplitRatio
	}
	return s, nil
}

// Update applies p to the settings currently in the file and returns the result.
// The file is re-read first, so fields changed by the other binary are kept, and
// it is only written if a value actually changes. An unparsable file is never
// overwritten.
func (f *File) Update(p Patch) (Settings, error) {
	if p.Theme != nil && !validTheme(*p.Theme) {
		return Settings{}, fmt.Errorf("%w: theme must be system, light or dark", ErrInvalid)
	}
	if p.SplitRatio != nil && !validRatio(*p.SplitRatio) {
		return Settings{}, fmt.Errorf("%w: splitRatio must be between 5 and 95", ErrInvalid)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.path == "" {
		return Defaults(), ErrNoPath
	}
	cur, err := f.load()
	if err != nil {
		return cur, err
	}
	next := cur
	if p.Theme != nil {
		next.Theme = *p.Theme
	}
	if p.SplitRatio != nil {
		next.SplitRatio = *p.SplitRatio
	}
	if p.Autosave != nil {
		next.Autosave = *p.Autosave
	}
	if next == cur {
		return cur, nil
	}
	if err := write(f.path, next); err != nil {
		return cur, err
	}
	return next, nil
}

// APIToken returns the optional "apiToken" that the user wrote into the config file
// for the mdnote server. mdnote never writes it, and it is not part of Settings, so
// it is never sent to the browser. A missing file or key gives "".
func (f *File) APIToken() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.path == "" {
		return "", nil
	}
	data, err := os.ReadFile(f.path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var v struct {
		APIToken string `json:"apiToken"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return "", fmt.Errorf("parse %s: %w", f.path, err)
	}
	return v.APIToken, nil
}

// write sets theme, splitRatio and autosave in the file and keeps every other key
// (such as a hand-written apiToken). It replaces the file atomically: a temp file in
// the same directory, then rename.
func write(path string, s Settings) error {
	raw := map[string]json.RawMessage{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if raw == nil { // the file contained just "null"
			raw = map[string]json.RawMessage{}
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for k, v := range map[string]any{"theme": s.Theme, "splitRatio": s.SplitRatio, "autosave": s.Autosave} {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		raw[k] = b
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func validTheme(t string) bool {
	return t == "system" || t == "light" || t == "dark"
}

func validRatio(r float64) bool {
	return r >= 5 && r <= 95 // false for NaN
}
