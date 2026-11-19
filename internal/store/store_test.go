package store

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestStoreCRUDAndManualFilesystemSync(t *testing.T) {
	tmpDir := t.TempDir()
	st, err := New(tmpDir)
	if err != nil {
		t.Fatalf("New store failed: %v", err)
	}

	// 1. Save a note via Store
	content1 := []byte("---\ntitle: Architecture Overview\ntags: [go, backend]\nstatus: draft\n---\n\n# Ignored Heading\n\nBody with inline #distributed-systems and `#not-a-tag`.\n```go\n// #also-not-a-tag\n```\n")
	meta, err := st.Save("eng/arch.md", content1, true)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if meta.Title != "Architecture Overview" {
		t.Errorf("expected title 'Architecture Overview', got %q", meta.Title)
	}
	if meta.Folder != "eng" {
		t.Errorf("expected folder 'eng', got %q", meta.Folder)
	}
	expectedTags := []string{"backend", "distributed-systems", "go"}
	if len(meta.Tags) != len(expectedTags) {
		t.Fatalf("expected tags %v, got %v", expectedTags, meta.Tags)
	}
	for i, et := range expectedTags {
		if meta.Tags[i] != et {
			t.Errorf("tag[%d]: expected %q, got %q", i, et, meta.Tags[i])
		}
	}

	// 2. Add a Markdown file manually on disk (simulating user dropping a file in folder)
	manualPath := filepath.Join(tmpDir, "manual-note.md")
	manualContent := []byte("# Manual Disk Note\n\nCreated outside the app with #manual and #go tags.\nSecret keyword: supercalifragilistic.\n")
	if err := os.WriteFile(manualPath, manualContent, 0o644); err != nil {
		t.Fatalf("WriteFile manual failed: %v", err)
	}

	// 3. Verify Search immediately discovers the manually added file
	res, err := st.Search(QueryOptions{Mode: ModeFullText})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if res.Total != 2 {
		t.Fatalf("expected 2 total notes (1 API + 1 manual), got %d", res.Total)
	}

	// 4. Test Full-Text Grep vs Title/Metadata Search
	ftRes, err := st.Search(QueryOptions{Query: "supercalifragilistic", Mode: ModeFullText})
	if err != nil {
		t.Fatalf("Full-text search failed: %v", err)
	}
	if ftRes.Total != 1 || ftRes.Items[0].Path != "manual-note.md" {
		t.Fatalf("expected full-text search to match manual-note.md, got %+v", ftRes.Items)
	}
	if len(ftRes.Items[0].Matches) == 0 || ftRes.Items[0].Matches[0].LineNumber != 4 {
		t.Errorf("expected line 4 snippet match, got %+v", ftRes.Items[0].Matches)
	}

	// Searching body text in ModeMeta should NOT match
	metaRes, err := st.Search(QueryOptions{Query: "supercalifragilistic", Mode: ModeMeta})
	if err != nil {
		t.Fatalf("Meta search failed: %v", err)
	}
	if metaRes.Total != 0 {
		t.Errorf("expected 0 matches for body-only keyword in ModeMeta, got %d", metaRes.Total)
	}

	// Searching frontmatter metadata or title in ModeMeta SHOULD match
	metaRes2, err := st.Search(QueryOptions{Query: "draft", Mode: ModeMeta})
	if err != nil {
		t.Fatalf("Meta search for frontmatter failed: %v", err)
	}
	if metaRes2.Total != 1 || metaRes2.Items[0].Path != "eng/arch.md" {
		t.Errorf("expected ModeMeta to match frontmatter status:draft on eng/arch.md, got %+v", metaRes2.Items)
	}

	// 5. Test Tag Filtering & Tag Counts
	tags, err := st.ListTags()
	if err != nil {
		t.Fatalf("ListTags failed: %v", err)
	}
	if len(tags) == 0 || tags[0].Tag != "go" || tags[0].Count != 2 {
		t.Errorf("expected top tag 'go' with count 2, got %+v", tags)
	}

	tagFiltered, err := st.Search(QueryOptions{Tag: "manual"})
	if err != nil {
		t.Fatalf("Tag filter search failed: %v", err)
	}
	if tagFiltered.Total != 1 || tagFiltered.Items[0].Path != "manual-note.md" {
		t.Errorf("expected tag=manual to return manual-note.md, got %+v", tagFiltered.Items)
	}
}

func TestPathTraversalProtection(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	unsafePaths := []string{
		"../outside.md",
		"../../etc/passwd",
		"folder/../../../escape.md",
		".git/config.md",
		"/etc/hosts",
	}
	for _, p := range unsafePaths {
		if _, _, err := st.CleanNotePath(p); err == nil {
			t.Errorf("expected unsafe path %q to be rejected, but it succeeded", p)
		}
	}
}

func TestSaveAsset(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	relPath, localName, err := st.SaveAsset("projects/roadmap.md", "clipboard.png", strings.NewReader("fake-png-bytes"))
	if err != nil {
		t.Fatalf("SaveAsset failed: %v", err)
	}
	if !strings.HasPrefix(relPath, "projects/") || !strings.HasSuffix(relPath, ".png") {
		t.Errorf("expected projects/*.png path, got %q", relPath)
	}
	if strings.Contains(localName, "/") || !strings.HasSuffix(localName, ".png") {
		t.Errorf("expected local filename without slash, got %q", localName)
	}

	fullPath := filepath.Join(st.RootDir(), filepath.FromSlash(relPath))
	data, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("Read saved asset failed: %v", err)
	}
	if string(data) != "fake-png-bytes" {
		t.Errorf("unexpected asset bytes: %q", string(data))
	}
}

// makeLinkTarget returns a notes folder holding a.md and sub/b.md, for opening
// through a symlink or junction.
func makeLinkTarget(t *testing.T) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "real")
	if err := os.MkdirAll(filepath.Join(target, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"a.md": "# A\n", "sub/b.md": "# B\n"} {
		if err := os.WriteFile(filepath.Join(target, filepath.FromSlash(name)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return target
}

// checkLinkedRoot opens a store at link, a symlink or junction to a makeLinkTarget
// folder, and checks that it sees and changes the folder's notes.
func checkLinkedRoot(t *testing.T, link string) {
	t.Helper()
	st, err := New(link)
	if err != nil {
		t.Fatal(err)
	}
	if st.RootDir() != link {
		t.Errorf("RootDir() = %q, want the link path %q", st.RootDir(), link)
	}
	notePaths := func() string {
		res, err := st.Search(QueryOptions{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		var paths []string
		for _, n := range res.Items {
			paths = append(paths, n.Path)
		}
		sort.Strings(paths)
		return strings.Join(paths, ",")
	}
	if got := notePaths(); got != "a.md,sub/b.md" {
		t.Errorf("notes = %q, want a.md,sub/b.md", got)
	}
	if folders, err := st.ListFolders(); err != nil || strings.Join(folders, ",") != "sub" {
		t.Errorf("ListFolders() = %v, %v; want [sub]", folders, err)
	}
	if note, err := st.Get("sub/b.md"); err != nil || note.Content != "# B\n" {
		t.Errorf("Get(sub/b.md) = %v, %v", note, err)
	}
	if _, err := st.Save("c.md", []byte("# C\n"), true); err != nil {
		t.Fatal(err)
	}
	if got := notePaths(); got != "a.md,c.md,sub/b.md" {
		t.Errorf("notes after Save = %q, want a.md,c.md,sub/b.md", got)
	}
}

func TestSymlinkedRoot(t *testing.T) {
	target := makeLinkTarget(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("can't create a symlink: %v", err)
	}
	checkLinkedRoot(t, link)
}
