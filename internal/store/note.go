package store

import (
	"bufio"
	"bytes"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

// SearchMatch represents a single matching line from a full-text grep search.
type SearchMatch struct {
	LineNumber int    `json:"line"`
	Snippet    string `json:"snippet"`
}

// NoteMeta holds metadata about a Markdown note without carrying its full body.
type NoteMeta struct {
	Path      string            `json:"path"`
	Title     string            `json:"title"`
	Folder    string            `json:"folder"`
	Tags      []string          `json:"tags"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	UpdatedAt time.Time         `json:"updatedAt"`
	Size      int64             `json:"size"`
	Matches   []SearchMatch     `json:"matches,omitempty"`
}

// Note represents a full note including raw Markdown content and rendered HTML.
type Note struct {
	NoteMeta
	Content string `json:"content"`
	HTML    string `json:"html"`
}

// TagCount represents a discovered tag and the number of notes containing it.
type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// ParseNoteMetaAndTags parses YAML frontmatter, the note title, and both frontmatter
// and inline #tags from a Markdown byte slice.
func ParseNoteMetaAndTags(relPath string, content []byte, modTime time.Time, size int64) NoteMeta {
	folder := filepath.ToSlash(filepath.Dir(relPath))
	if folder == "." {
		folder = ""
	}

	metaMap := make(map[string]string)
	tagSet := make(map[string]struct{})
	var title string

	scanner := bufio.NewScanner(bytes.NewReader(content))
	// Allow lines up to 256KB
	buf := make([]byte, 0, 16*1024)
	scanner.Buffer(buf, 256*1024)

	lineIdx := 0
	inFrontmatter := false
	frontmatterDone := false
	inFrontmatterTagsList := false
	inCodeFence := false
	fenceMarker := ""

	for scanner.Scan() {
		line := scanner.Text()
		lineIdx++
		trimmed := strings.TrimSpace(line)

		// Check frontmatter opening on first line
		if lineIdx == 1 && trimmed == "---" {
			inFrontmatter = true
			continue
		}

		if inFrontmatter && !frontmatterDone {
			if trimmed == "---" || trimmed == "..." {
				inFrontmatter = false
				frontmatterDone = true
				inFrontmatterTagsList = false
				continue
			}

			// Handle YAML list item under `tags:`
			if inFrontmatterTagsList {
				if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
					val := cleanYAMLValue(trimmed[2:])
					addNormalizedTag(tagSet, val)
					continue
				} else if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
					// Indented continuation
					continue
				} else {
					inFrontmatterTagsList = false
				}
			}

			if colonIdx := strings.IndexByte(line, ':'); colonIdx > 0 {
				key := strings.ToLower(strings.TrimSpace(line[:colonIdx]))
				rawVal := strings.TrimSpace(line[colonIdx+1:])
				val := cleanYAMLValue(rawVal)

				if key == "title" && val != "" {
					title = val
				} else if key == "tags" || key == "tag" {
					if rawVal == "" {
						inFrontmatterTagsList = true
					} else {
						parseFrontmatterTags(rawVal, tagSet)
					}
				}
				if val != "" {
					metaMap[key] = val
				}
			}
			continue
		}

		// Outside frontmatter: handle fenced code blocks (``` or ~~~)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			marker := trimmed[:3]
			if !inCodeFence {
				inCodeFence = true
				fenceMarker = marker
			} else if marker == fenceMarker {
				inCodeFence = false
				fenceMarker = ""
			}
			continue
		}
		if inCodeFence {
			continue
		}

		// Extract first ATX heading (# Title) if title not yet found
		if title == "" && strings.HasPrefix(trimmed, "#") {
			hashes := 0
			for hashes < len(trimmed) && trimmed[hashes] == '#' {
				hashes++
			}
			if hashes >= 1 && hashes <= 6 && hashes < len(trimmed) && trimmed[hashes] == ' ' {
				headingText := strings.TrimSpace(trimmed[hashes+1:])
				headingText = strings.TrimRight(headingText, "# ")
				if headingText != "" {
					title = headingText
				}
			}
		}

		// Scan line for inline #tags (ignoring headings and inline codespans)
		extractInlineTagsFromLine(line, tagSet)
	}

	if title == "" {
		base := filepath.Base(relPath)
		title = strings.TrimSuffix(base, filepath.Ext(base))
	}

	tags := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tags = append(tags, t)
	}
	sort.Strings(tags)

	if len(metaMap) == 0 {
		metaMap = nil
	}

	return NoteMeta{
		Path:      filepath.ToSlash(relPath),
		Title:     title,
		Folder:    folder,
		Tags:      tags,
		Metadata:  metaMap,
		UpdatedAt: modTime,
		Size:      size,
	}
}

func cleanYAMLValue(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func parseFrontmatterTags(raw string, tagSet map[string]struct{}) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "[")
	raw = strings.TrimSuffix(raw, "]")
	// Split by comma or whitespace
	var fields []string
	if strings.Contains(raw, ",") {
		fields = strings.Split(raw, ",")
	} else {
		fields = strings.Fields(raw)
	}
	for _, f := range fields {
		addNormalizedTag(tagSet, cleanYAMLValue(f))
	}
}

func addNormalizedTag(tagSet map[string]struct{}, raw string) {
	t := strings.TrimSpace(raw)
	t = strings.TrimPrefix(t, "#")
	t = strings.ToLower(t)
	if isValidTagName(t) {
		tagSet[t] = struct{}{}
	}
}

func isValidTagName(t string) bool {
	if len(t) == 0 || len(t) > 64 {
		return false
	}
	// Must start with a letter
	first := rune(t[0])
	if !unicode.IsLetter(first) {
		return false
	}
	for _, r := range t {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' && r != '/' {
			return false
		}
	}
	return true
}

func extractInlineTagsFromLine(line string, tagSet map[string]struct{}) {
	inBackticks := false
	n := len(line)
	for i := 0; i < n; i++ {
		ch := line[i]
		if ch == '`' {
			inBackticks = !inBackticks
			continue
		}
		if inBackticks {
			continue
		}
		if ch == '#' {
			// Must be at start of line or preceded by whitespace or opening punctuation
			if i > 0 {
				prev := line[i-1]
				if prev != ' ' && prev != '\t' && prev != '(' && prev != '[' && prev != ',' && prev != ';' {
					continue
				}
			}
			// Must not be followed by another '#' or space (e.g. Markdown headings)
			if i+1 >= n {
				continue
			}
			next := line[i+1]
			if !unicode.IsLetter(rune(next)) {
				continue
			}
			// Read tag characters
			j := i + 1
			for j < n {
				r := rune(line[j])
				if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '/' {
					j++
				} else {
					break
				}
			}
			candidate := strings.TrimRight(line[i+1:j], "-_/")
			addNormalizedTag(tagSet, candidate)
			i = j - 1
		}
	}
}
