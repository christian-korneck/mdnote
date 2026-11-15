package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func populateThousandsOfNotes(tb testing.TB, dir string, count int) {
	tb.Helper()
	folders := []string{"eng", "product", "ops", "research", "journal/2026"}
	for _, f := range folders {
		if err := os.MkdirAll(filepath.Join(dir, f), 0o755); err != nil {
			tb.Fatal(err)
		}
	}

	for i := 1; i <= count; i++ {
		folder := folders[i%len(folders)]
		p := filepath.Join(dir, folder, fmt.Sprintf("note-%05d.md", i))
		content := fmt.Sprintf("---\ntitle: Note Number %05d\ntags: [tag-%d, scale]\ncategory: cat-%d\n---\n\n# Note %05d\n\nBody text for note %d with inline #topic-%d.\nSpecial token: needle_%05d_target.\n",
			i, i%20, i%10, i, i, i%15, i)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			tb.Fatal(err)
		}
	}
}

func TestScaleThousandsOfNotes(t *testing.T) {
	tmpDir := t.TempDir()
	const totalNotes = 3000
	populateThousandsOfNotes(t, tmpDir, totalNotes)

	st, err := New(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Cold initial scan of 3,000 notes
	t0 := time.Now()
	res, err := st.Search(QueryOptions{Limit: 100, Offset: 0})
	if err != nil {
		t.Fatalf("Initial search failed: %v", err)
	}
	coldElapsed := time.Since(t0)
	if res.Total != totalNotes || len(res.Items) != 100 {
		t.Fatalf("expected total=%d and len(items)=100, got total=%d len=%d", totalNotes, res.Total, len(res.Items))
	}
	t.Logf("3,000 notes cold scan: %v", coldElapsed)

	// Warm metadata search across 3,000 notes
	t1 := time.Now()
	metaRes, err := st.Search(QueryOptions{Query: "Note Number 02500", Mode: ModeMeta})
	if err != nil {
		t.Fatal(err)
	}
	warmMetaElapsed := time.Since(t1)
	if metaRes.Total != 1 {
		t.Fatalf("expected 1 match in ModeMeta, got %d", metaRes.Total)
	}
	t.Logf("3,000 notes warm metadata search: %v", warmMetaElapsed)

	// Concurrent full-text grep across 3,000 files
	t2 := time.Now()
	grepRes, err := st.Search(QueryOptions{Query: "needle_01999_target", Mode: ModeFullText})
	if err != nil {
		t.Fatal(err)
	}
	grepElapsed := time.Since(t2)
	if grepRes.Total != 1 || len(grepRes.Items[0].Matches) != 1 {
		t.Fatalf("expected 1 full-text grep match, got %+v", grepRes)
	}
	t.Logf("3,000 notes concurrent full-text grep: %v", grepElapsed)

	// Tag aggregation across 3,000 files
	t3 := time.Now()
	tags, err := st.ListTags()
	if err != nil {
		t.Fatal(err)
	}
	tagsElapsed := time.Since(t3)
	if len(tags) == 0 || tags[0].Tag != "scale" || tags[0].Count != totalNotes {
		t.Fatalf("expected 'scale' tag with count %d, got %+v", totalNotes, tags)
	}
	t.Logf("3,000 notes tag aggregation: %v", tagsElapsed)
}
