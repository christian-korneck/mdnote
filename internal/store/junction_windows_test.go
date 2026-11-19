package store

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// An NTFS junction as the notes folder (Go reports junctions as irregular files, not
// as directories or symlinks).
func TestJunctionRoot(t *testing.T) {
	target := makeLinkTarget(t)
	link := filepath.Join(t.TempDir(), "junction")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("can't create a junction: %v: %s", err, out)
	}
	checkLinkedRoot(t, link)
}
