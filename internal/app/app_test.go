package app

import (
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenWritesWelcomeNoteIntoEmptyDir(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := Open(Config{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "welcome.md")); err != nil {
		t.Fatalf("expected welcome.md in empty notes dir: %v", err)
	}

	// A second Open on a non-empty dir must not touch it again.
	if err := os.Remove(filepath.Join(dir, "welcome.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mine.md"), []byte("# Mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(Config{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "welcome.md")); !os.IsNotExist(err) {
		t.Fatalf("expected no welcome.md in non-empty notes dir, stat err: %v", err)
	}
}

func TestOpenSeedsNotesAndServesAPI(t *testing.T) {
	dir := t.TempDir()
	st, handler, err := Open(Config{Dir: dir, Seed: 5})
	if err != nil {
		t.Fatal(err)
	}
	if st.RootDir() == "" {
		t.Fatal("expected store root dir")
	}
	if _, err := os.Stat(filepath.Join(dir, "welcome.md")); !os.IsNotExist(err) {
		t.Fatalf("expected no welcome.md when seeding, stat err: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/notes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/notes: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Total int `json:"total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	if res.Total != 5 {
		t.Fatalf("expected 5 seeded notes, got %d", res.Total)
	}
}

func TestParseArgs(t *testing.T) {
	tests := []struct {
		args    []string
		wantErr bool
		dir     string
		seed    int
	}{
		{args: nil, wantErr: true},
		{args: []string{"./notes"}, dir: "./notes"},
		{args: []string{"-seed", "5", "."}, dir: ".", seed: 5},
		{args: []string{"./notes", "-seed", "5"}, wantErr: true},
		{args: []string{"a", "b"}, wantErr: true},
	}
	for _, tt := range tests {
		var cfg Config
		fs := flag.NewFlagSet("mdnote", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		cfg.RegisterFlags(fs)
		err := cfg.ParseArgs(fs, tt.args)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseArgs(%q) error = %v, wantErr %v", tt.args, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && (cfg.Dir != tt.dir || cfg.Seed != tt.seed) {
			t.Errorf("ParseArgs(%q) = Dir %q, Seed %d; want Dir %q, Seed %d", tt.args, cfg.Dir, cfg.Seed, tt.dir, tt.seed)
		}
	}
}

func TestOpenNoWelcome(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := Open(Config{Dir: dir, NoWelcome: true}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty notes dir with NoWelcome, found %v", entries)
	}
}

// A notes folder behind a symlink isn't empty: Open must not add welcome.md to it.
func TestOpenSymlinkedDir(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "note.md"), []byte("# Note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("can't create a symlink: %v", err)
	}
	if _, _, err := Open(Config{Dir: link, SettingsPath: filepath.Join(t.TempDir(), "config.json")}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "welcome.md")); !os.IsNotExist(err) {
		t.Fatalf("Open wrote welcome.md into a non-empty folder behind a symlink (stat: %v)", err)
	}
}
