package store

import (
	"os"
	"path/filepath"
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
