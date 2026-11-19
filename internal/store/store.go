package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

var (
	ErrInvalidPath = errors.New("invalid or unsafe path")
	ErrNotFound    = errors.New("note not found")
	ErrAlreadyExists = errors.New("note already exists")
)

type cachedMeta struct {
	modTimeUnixNano int64
	size            int64
	meta            NoteMeta
}

// Store manages Markdown notes and assets directly on a filesystem directory.
type Store struct {
	rootDir string

	mu    sync.RWMutex
	cache map[string]cachedMeta
}

// New creates a Store rooted at rootDir, creating the directory if needed.
func New(rootDir string) (*Store, error) {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve root dir: %w", err)
	}
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create root dir: %w", err)
	}

	return &Store{
		rootDir: absRoot,
		cache:   make(map[string]cachedMeta, 1024),
	}, nil
}

// RootDir returns the absolute root path of the notes directory.
func (s *Store) RootDir() string {
	return s.rootDir
}

// Refresh clears the in-memory metadata cache and rescans the filesystem directory.
func (s *Store) Refresh() (int, error) {
	s.mu.Lock()
	clear(s.cache)
	s.mu.Unlock()

	metas, err := s.syncAndLoadAllMeta()
	if err != nil {
		return 0, err
	}
	return len(metas), nil
}

// CleanNotePath validates and normalizes a relative note path, ensuring a .md extension
// and preventing directory traversal outside rootDir.
func (s *Store) CleanNotePath(relPath string) (string, string, error) {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" || strings.HasPrefix(relPath, "/") || strings.HasPrefix(relPath, "\\") || filepath.IsAbs(relPath) {
		return "", "", ErrInvalidPath
	}
	relPath = filepath.ToSlash(relPath)
	if !strings.HasSuffix(strings.ToLower(relPath), ".md") {
		relPath += ".md"
	}

	cleaned := filepath.Clean(filepath.FromSlash(relPath))
	if cleaned == "." || strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) {
		return "", "", ErrInvalidPath
	}

	// Disallow writing notes inside hidden dot directories
	parts := strings.Split(filepath.ToSlash(cleaned), "/")
	for _, p := range parts {
		if p == "" || p == "." || p == ".." || strings.HasPrefix(p, ".") {
			return "", "", ErrInvalidPath
		}
	}

	absPath := filepath.Join(s.rootDir, cleaned)
	relCheck, err := filepath.Rel(s.rootDir, absPath)
	if err != nil || strings.HasPrefix(relCheck, "..") {
		return "", "", ErrInvalidPath
	}

	return filepath.ToSlash(cleaned), absPath, nil
}

// Get reads a note's content and metadata from disk.
func (s *Store) Get(relPath string) (*Note, error) {
	normRel, absPath, err := s.CleanNotePath(relPath)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if info.IsDir() {
		return nil, ErrNotFound
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}

	meta := ParseNoteMetaAndTags(normRel, content, info.ModTime(), info.Size())
	s.updateCacheEntry(normRel, info.ModTime(), info.Size(), meta)

	return &Note{
		NoteMeta: meta,
		Content:  string(content),
	}, nil
}

// Save writes a note to disk (creating parent directories as needed) and updates its cached metadata.
func (s *Store) Save(relPath string, content []byte, createOnly bool) (*NoteMeta, error) {
	normRel, absPath, err := s.CleanNotePath(relPath)
	if err != nil {
		return nil, err
	}

	if createOnly {
		if _, err := os.Stat(absPath); err == nil {
			return nil, ErrAlreadyExists
		}
	}

	parentDir := filepath.Dir(absPath)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return nil, fmt.Errorf("create parent folder: %w", err)
	}

	if err := os.WriteFile(absPath, content, 0o644); err != nil {
		return nil, fmt.Errorf("write note: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}

	meta := ParseNoteMetaAndTags(normRel, content, info.ModTime(), info.Size())
	s.updateCacheEntry(normRel, info.ModTime(), info.Size(), meta)
	return &meta, nil
}

// Rename moves or renames an existing note from oldPath to newPath.
func (s *Store) Rename(oldRelPath, newRelPath string) (*NoteMeta, error) {
	oldNorm, oldAbs, err := s.CleanNotePath(oldRelPath)
	if err != nil {
		return nil, err
	}
	newNorm, newAbs, err := s.CleanNotePath(newRelPath)
	if err != nil {
		return nil, err
	}
	if oldNorm == newNorm {
		n, err := s.Get(oldNorm)
		if err != nil {
			return nil, err
		}
		return &n.NoteMeta, nil
	}

	if _, err := os.Stat(oldAbs); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if _, err := os.Stat(newAbs); err == nil {
		return nil, ErrAlreadyExists
	}

	if err := os.MkdirAll(filepath.Dir(newAbs), 0o755); err != nil {
		return nil, err
	}
	if err := os.Rename(oldAbs, newAbs); err != nil {
		return nil, err
	}

	s.mu.Lock()
	delete(s.cache, oldNorm)
	s.mu.Unlock()

	note, err := s.Get(newNorm)
	if err != nil {
		return nil, err
	}
	return &note.NoteMeta, nil
}

// Delete removes a note from disk and evicts it from the cache.
func (s *Store) Delete(relPath string) error {
	normRel, absPath, err := s.CleanNotePath(relPath)
	if err != nil {
		return err
	}

	if err := os.Remove(absPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}

	s.mu.Lock()
	delete(s.cache, normRel)
	s.mu.Unlock()
	return nil
}

// SaveAsset writes an uploaded image/asset directly into the same directory as noteRelPath
// (or rootDir if noteRelPath is empty or at the root) and returns:
//   - relAssetPath: path relative to rootDir (e.g. "projects/1763159000-a1b2c3d4.png")
//   - localName: filename relative to the note itself (e.g. "1763159000-a1b2c3d4.png")
func (s *Store) SaveAsset(noteRelPath string, originalFilename string, r io.Reader) (string, string, error) {
	ext := strings.ToLower(filepath.Ext(originalFilename))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".avif", ".bmp", ".pdf":
	case "":
		ext = ".png"
	default:
		cleanedExt := "."
		for _, ch := range ext[1:] {
			if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
				cleanedExt += string(ch)
			}
		}
		if len(cleanedExt) == 1 || len(cleanedExt) > 10 {
			ext = ".bin"
		} else {
			ext = cleanedExt
		}
	}

	targetDir := s.rootDir
	relDir := ""
	if strings.TrimSpace(noteRelPath) != "" {
		normNote, _, err := s.CleanNotePath(noteRelPath)
		if err != nil {
			return "", "", err
		}
		dir := filepath.ToSlash(filepath.Dir(normNote))
		if dir != "." && dir != "" {
			relDir = dir
			targetDir = filepath.Join(s.rootDir, filepath.FromSlash(dir))
		}
	}

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create note asset dir: %w", err)
	}

	now := time.Now().UTC()
	randBytes := make([]byte, 4)
	_, _ = rand.Read(randBytes)
	filename := fmt.Sprintf("%d-%s%s", now.Unix(), hex.EncodeToString(randBytes), ext)
	absTarget := filepath.Join(targetDir, filename)

	f, err := os.OpenFile(absTarget, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		return "", "", fmt.Errorf("create asset file: %w", err)
	}
	defer f.Close()

	// Limit single asset upload to 32MB
	if _, err := io.Copy(f, io.LimitReader(r, 32<<20)); err != nil {
		return "", "", fmt.Errorf("write asset: %w", err)
	}

	relAssetPath := filename
	if relDir != "" {
		relAssetPath = relDir + "/" + filename
	}
	return relAssetPath, filename, nil
}

// CleanFolderPath validates and normalizes a relative folder path,
// preventing directory traversal outside rootDir or into hidden directories.
func (s *Store) CleanFolderPath(relFolder string) (string, string, error) {
	relFolder = strings.TrimSpace(relFolder)
	relFolder = strings.Trim(relFolder, "/")
	if relFolder == "" || strings.HasPrefix(relFolder, "\\") || filepath.IsAbs(relFolder) {
		return "", "", ErrInvalidPath
	}

	cleaned := filepath.Clean(filepath.FromSlash(relFolder))
	if cleaned == "." || strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) {
		return "", "", ErrInvalidPath
	}

	parts := strings.Split(filepath.ToSlash(cleaned), "/")
	for _, p := range parts {
		if p == "" || p == "." || p == ".." || strings.HasPrefix(p, ".") {
			return "", "", ErrInvalidPath
		}
	}

	absPath := filepath.Join(s.rootDir, cleaned)
	relCheck, err := filepath.Rel(s.rootDir, absPath)
	if err != nil || strings.HasPrefix(relCheck, "..") {
		return "", "", ErrInvalidPath
	}

	return filepath.ToSlash(cleaned), absPath, nil
}

// CreateFolder creates a directory (including any parent directories) inside rootDir.
func (s *Store) CreateFolder(relFolder string) (string, error) {
	normRel, absPath, err := s.CleanFolderPath(relFolder)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(absPath, 0o755); err != nil {
		return "", fmt.Errorf("create folder: %w", err)
	}
	return normRel, nil
}

// walk calls fn for every entry under the notes folder, with its slash-separated
// path relative to the folder ("." for the folder itself). Entries that can't be read
// are skipped.
//
// It walks os.DirFS(rootDir) rather than filepath.WalkDir(rootDir): WalkDir starts
// with Lstat, so a notes folder that is itself a symlink or an NTFS junction would
// look like a file and show no notes. DirFS starts with Stat, which follows both.
// Links inside the folder are still not followed.
func (s *Store) walk(fn func(rel string, d fs.DirEntry) error) error {
	return fs.WalkDir(os.DirFS(s.rootDir), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		return fn(rel, d)
	})
}

// ListFolders returns all directory paths containing notes or existing inside rootDir.
func (s *Store) ListFolders() ([]string, error) {
	folderSet := make(map[string]struct{})
	err := s.walk(func(rel string, d fs.DirEntry) error {
		if !d.IsDir() || rel == "." {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return fs.SkipDir
		}
		folderSet[rel] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, err
	}

	folders := make([]string, 0, len(folderSet))
	for f := range folderSet {
		folders = append(folders, f)
	}
	sort.Strings(folders)
	return folders, nil
}

func (s *Store) updateCacheEntry(normRel string, modTime time.Time, size int64, meta NoteMeta) {
	s.mu.Lock()
	s.cache[normRel] = cachedMeta{
		modTimeUnixNano: modTime.UnixNano(),
		size:            size,
		meta:            cloneMeta(meta),
	}
	s.mu.Unlock()
}

func cloneMeta(m NoteMeta) NoteMeta {
	out := m
	if len(m.Tags) > 0 {
		out.Tags = append([]string(nil), m.Tags...)
	}
	if len(m.Metadata) > 0 {
		out.Metadata = make(map[string]string, len(m.Metadata))
		for k, v := range m.Metadata {
			out.Metadata[k] = v
		}
	}
	return out
}

type diskEntry struct {
	relPath string
	absPath string
	modTime time.Time
	size    int64
}

// syncAndLoadAllMeta walks rootDir, reuses cached NoteMeta for unchanged files (matching mtime & size),
// concurrently parses only new or modified files, and prunes deleted files from the cache.
func (s *Store) syncAndLoadAllMeta() ([]NoteMeta, error) {
	var entries []diskEntry

	err := s.walk(func(rel string, d fs.DirEntry) error {
		name := d.Name()
		if d.IsDir() {
			if rel != "." && strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || !strings.HasSuffix(strings.ToLower(name), ".md") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		entries = append(entries, diskEntry{
			relPath: rel,
			absPath: filepath.Join(s.rootDir, filepath.FromSlash(rel)),
			modTime: info.ModTime(),
			size:    info.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	results := make([]NoteMeta, len(entries))
	var toParseIdx []int

	s.mu.RLock()
	for i, e := range entries {
		if c, ok := s.cache[e.relPath]; ok && c.modTimeUnixNano == e.modTime.UnixNano() && c.size == e.size {
			results[i] = cloneMeta(c.meta)
		} else {
			toParseIdx = append(toParseIdx, i)
		}
	}
	s.mu.RUnlock()

	if len(toParseIdx) > 0 {
		workers := runtime.NumCPU()
		if workers < 2 {
			workers = 2
		}
		var g errgroup.Group
		g.SetLimit(workers)

		for _, idx := range toParseIdx {
			idx := idx
			e := entries[idx]
			g.Go(func() error {
				content, err := os.ReadFile(e.absPath)
				if err != nil {
					// If file vanished mid-scan, leave empty
					return nil
				}
				results[idx] = ParseNoteMetaAndTags(e.relPath, content, e.modTime, e.size)
				return nil
			})
		}
		_ = g.Wait()
	}

	// Update cache with new entries and prune deleted files if needed
	s.mu.Lock()
	if len(toParseIdx) > 0 || len(s.cache) != len(entries) {
		seen := make(map[string]struct{}, len(entries))
		for i, e := range entries {
			seen[e.relPath] = struct{}{}
			if results[i].Path != "" {
				s.cache[e.relPath] = cachedMeta{
					modTimeUnixNano: e.modTime.UnixNano(),
					size:            e.size,
					meta:            cloneMeta(results[i]),
				}
			}
		}
		for k := range s.cache {
			if _, ok := seen[k]; !ok {
				delete(s.cache, k)
			}
		}
	}
	s.mu.Unlock()

	// Filter out any entries that vanished during read
	valid := results[:0]
	for _, r := range results {
		if r.Path != "" {
			valid = append(valid, r)
		}
	}
	return valid, nil
}
