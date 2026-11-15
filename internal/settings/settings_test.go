package settings

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	f := NewFile(filepath.Join(t.TempDir(), "mdnote", "config.json"))
	s, err := f.Load()
	if err != nil || s != Defaults() {
		t.Fatalf("Load() = %+v, %v; want defaults", s, err)
	}
}

func TestUpdateKeepsOtherFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mdnote", "config.json")
	f := NewFile(path)
	if _, err := f.Update(Patch{Theme: new("dark")}); err != nil {
		t.Fatal(err)
	}
	// The other binary changes autosave in the meantime.
	if err := os.WriteFile(path, []byte(`{"theme":"dark","splitRatio":50,"autosave":false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := f.Update(Patch{SplitRatio: new(40.5)})
	if err != nil {
		t.Fatal(err)
	}
	want := Settings{Theme: "dark", SplitRatio: 40.5, Autosave: false}
	if s != want {
		t.Fatalf("Update() = %+v, want %+v", s, want)
	}
	if got, _ := NewFile(path).Load(); got != want {
		t.Fatalf("file contains %+v, want %+v", got, want)
	}
}

func TestNoOpUpdateDoesNotWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	f := NewFile(path)
	if _, err := f.Update(Patch{Theme: new("system"), Autosave: new(true)}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a no-op update created the file (stat err %v)", err)
	}

	if _, err := f.Update(Patch{Theme: new("light")}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Update(Patch{Theme: new("light")}); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); !fi.ModTime().Equal(old) {
		t.Fatalf("a no-op update rewrote the file")
	}
}

func TestUpdateRejectsInvalidValues(t *testing.T) {
	f := NewFile(filepath.Join(t.TempDir(), "config.json"))
	for _, p := range []Patch{{Theme: new("blue")}, {SplitRatio: new(2.0)}, {SplitRatio: new(99.0)}} {
		if _, err := f.Update(p); !errors.Is(err, ErrInvalid) {
			t.Errorf("Update(%+v) error = %v, want ErrInvalid", p, err)
		}
	}
}

func TestUpdateDoesNotOverwriteUnparsableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	broken := []byte(`{"theme": "dark",`)
	if err := os.WriteFile(path, broken, 0o644); err != nil {
		t.Fatal(err)
	}
	f := NewFile(path)
	if s, err := f.Load(); err == nil || s != Defaults() {
		t.Fatalf("Load() = %+v, %v; want defaults and an error", s, err)
	}
	if _, err := f.Update(Patch{Autosave: new(false)}); err == nil {
		t.Fatal("Update on an unparsable file succeeded")
	}
	if data, _ := os.ReadFile(path); string(data) != string(broken) {
		t.Fatalf("unparsable file was changed to %q", data)
	}
}

func TestLoadNormalizesInvalidValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"theme":"blue","splitRatio":120,"autosave":false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewFile(path).Load()
	want := Settings{Theme: "system", SplitRatio: 50, Autosave: false}
	if err != nil || s != want {
		t.Fatalf("Load() = %+v, %v; want %+v", s, err, want)
	}
}

func TestEmptyPath(t *testing.T) {
	f := NewFile("")
	if s, err := f.Load(); err != nil || s != Defaults() {
		t.Fatalf("Load() = %+v, %v; want defaults", s, err)
	}
	if _, err := f.Update(Patch{Theme: new("dark")}); !errors.Is(err, ErrNoPath) {
		t.Fatalf("Update() error = %v, want ErrNoPath", err)
	}
}

func TestAPITokenAndUpdateKeepsOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	f := NewFile(path)
	if tok, err := f.APIToken(); err != nil || tok != "" {
		t.Fatalf("APIToken() without a file = %q, %v", tok, err)
	}
	if err := os.WriteFile(path, []byte(`{"apiToken":"s3cret","custom":[1,2],"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if tok, err := f.APIToken(); err != nil || tok != "s3cret" {
		t.Fatalf("APIToken() = %q, %v; want s3cret", tok, err)
	}
	s, err := f.Update(Patch{Autosave: new(false)})
	if err != nil {
		t.Fatal(err)
	}
	if s != (Settings{Theme: "dark", SplitRatio: 50, Autosave: false}) {
		t.Fatalf("Update() = %+v", s)
	}
	data, _ := os.ReadFile(path)
	for _, want := range []string{`"apiToken": "s3cret"`, `"custom": [`, `"autosave": false`, `"theme": "dark"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("config file lost %s:\n%s", want, data)
		}
	}
	if tok, _ := f.APIToken(); tok != "s3cret" {
		t.Errorf("apiToken after Update = %q", tok)
	}
}
