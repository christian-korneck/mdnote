package store

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// SearchMode controls whether search scans full note bodies or only title/metadata.
type SearchMode string

const (
	ModeFullText SearchMode = "fulltext"
	ModeMeta     SearchMode = "meta"
)

// QueryOptions defines filtering, search mode, sorting, and pagination parameters.
type QueryOptions struct {
	Query  string     `json:"query"`
	Mode   SearchMode `json:"mode"`
	Tag    string     `json:"tag"`
	Folder string     `json:"folder"`
	Sort   string     `json:"sort"`
	Limit  int        `json:"limit"`
	Offset int        `json:"offset"`
}

// QueryResult wraps paginated search results along with total match count and execution timing.
type QueryResult struct {
	Items      []NoteMeta `json:"items"`
	Total      int        `json:"total"`
	Limit      int        `json:"limit"`
	Offset     int        `json:"offset"`
	Mode       SearchMode `json:"mode"`
	DurationMs float64    `json:"durationMs"`
}

// Search queries and filters notes using either concurrent full-text grep or title/metadata matching.
func (s *Store) Search(opts QueryOptions) (*QueryResult, error) {
	start := time.Now()

	if opts.Mode != ModeMeta {
		opts.Mode = ModeFullText
	}
	if opts.Limit <= 0 {
		opts.Limit = 100
	} else if opts.Limit > 1000 {
		opts.Limit = 1000
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}

	allMeta, err := s.syncAndLoadAllMeta()
	if err != nil {
		return nil, err
	}

	folderFilter := strings.Trim(filepath.ToSlash(strings.TrimSpace(opts.Folder)), "/")
	tagFilter := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(opts.Tag), "#"))
	rawQuery := strings.TrimSpace(opts.Query)

	// Step 1: Filter by folder and tag
	candidates := make([]NoteMeta, 0, len(allMeta))
	for _, m := range allMeta {
		if folderFilter != "" {
			if m.Folder != folderFilter && !strings.HasPrefix(m.Folder, folderFilter+"/") {
				continue
			}
		}
		if tagFilter != "" {
			if !hasTag(m.Tags, tagFilter) {
				continue
			}
		}
		candidates = append(candidates, m)
	}

	// Step 2: Execute search if Query is non-empty
	type scoredNote struct {
		meta  NoteMeta
		score int
	}
	var matched []scoredNote

	if rawQuery == "" {
		matched = make([]scoredNote, len(candidates))
		for i, c := range candidates {
			matched[i] = scoredNote{meta: c, score: 0}
		}
	} else if opts.Mode == ModeMeta {
		matcher := compileMatcher(rawQuery)
		for _, c := range candidates {
			if score := matchMeta(c, matcher); score > 0 {
				matched = append(matched, scoredNote{meta: c, score: score})
			}
		}
	} else {
		// Concurrent grep-style full-text scan across candidate files
		matcher := compileMatcher(rawQuery)
		workers := runtime.NumCPU()
		if workers < 2 {
			workers = 2
		}

		var mu sync.Mutex
		var g errgroup.Group
		g.SetLimit(workers)

		for _, cand := range candidates {
			cand := cand
			g.Go(func() error {
				metaScore := matchMeta(cand, matcher)
				absPath := filepath.Join(s.rootDir, filepath.FromSlash(cand.Path))
				matches, lineHits := grepFile(absPath, matcher, 3)

				if metaScore > 0 || lineHits > 0 {
					cand.Matches = matches
					totalScore := metaScore*10 + lineHits
					mu.Lock()
					matched = append(matched, scoredNote{meta: cand, score: totalScore})
					mu.Unlock()
				}
				return nil
			})
		}
		_ = g.Wait()
	}

	// Step 3: Sort results
	sortMode := strings.ToLower(strings.TrimSpace(opts.Sort))
	sort.Slice(matched, func(i, j int) bool {
		a, b := matched[i], matched[j]
		if rawQuery != "" && sortMode == "" {
			if a.score != b.score {
				return a.score > b.score
			}
		}
		switch sortMode {
		case "title_asc":
			if !strings.EqualFold(a.meta.Title, b.meta.Title) {
				return strings.ToLower(a.meta.Title) < strings.ToLower(b.meta.Title)
			}
			return a.meta.Path < b.meta.Path
		case "path_asc":
			return a.meta.Path < b.meta.Path
		case "updated_asc":
			if !a.meta.UpdatedAt.Equal(b.meta.UpdatedAt) {
				return a.meta.UpdatedAt.Before(b.meta.UpdatedAt)
			}
			return a.meta.Path < b.meta.Path
		default: // "updated_desc"
			if !a.meta.UpdatedAt.Equal(b.meta.UpdatedAt) {
				return a.meta.UpdatedAt.After(b.meta.UpdatedAt)
			}
			return a.meta.Path < b.meta.Path
		}
	})

	total := len(matched)
	startIdx := opts.Offset
	if startIdx > total {
		startIdx = total
	}
	endIdx := startIdx + opts.Limit
	if endIdx > total {
		endIdx = total
	}

	items := make([]NoteMeta, 0, endIdx-startIdx)
	for _, sn := range matched[startIdx:endIdx] {
		items = append(items, sn.meta)
	}

	durationMs := float64(time.Since(start).Microseconds()) / 1000.0
	return &QueryResult{
		Items:      items,
		Total:      total,
		Limit:      opts.Limit,
		Offset:     opts.Offset,
		Mode:       opts.Mode,
		DurationMs: durationMs,
	}, nil
}

// ListTags returns all tags discovered across all Markdown files, sorted by frequency then name.
func (s *Store) ListTags() ([]TagCount, error) {
	allMeta, err := s.syncAndLoadAllMeta()
	if err != nil {
		return nil, err
	}

	counts := make(map[string]int)
	for _, m := range allMeta {
		for _, t := range m.Tags {
			counts[t]++
		}
	}

	out := make([]TagCount, 0, len(counts))
	for tag, c := range counts {
		out = append(out, TagCount{Tag: tag, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Tag < out[j].Tag
	})
	return out, nil
}

func hasTag(tags []string, target string) bool {
	for _, t := range tags {
		if t == target {
			return true
		}
	}
	return false
}

type queryMatcher struct {
	re         *regexp.Regexp
	lowerQuery string
	terms      []string
}

// compileMatcher supports regex queries (wrapped in /.../ or prefixed with re:)
// as well as fast case-insensitive substring and multi-term matching.
func compileMatcher(q string) queryMatcher {
	if len(q) > 2 && strings.HasPrefix(q, "/") && strings.HasSuffix(q, "/") {
		pattern := q[1 : len(q)-1]
		if re, err := regexp.Compile("(?i)" + pattern); err == nil {
			return queryMatcher{re: re, lowerQuery: strings.ToLower(pattern)}
		}
	} else if strings.HasPrefix(q, "re:") && len(q) > 3 {
		pattern := strings.TrimSpace(q[3:])
		if re, err := regexp.Compile("(?i)" + pattern); err == nil {
			return queryMatcher{re: re, lowerQuery: strings.ToLower(pattern)}
		}
	}
	lq := strings.ToLower(q)
	return queryMatcher{
		lowerQuery: lq,
		terms:      strings.Fields(lq),
	}
}

func (m queryMatcher) matchesLine(line string) bool {
	if m.re != nil {
		return m.re.MatchString(line)
	}
	lowerLine := strings.ToLower(line)
	if strings.Contains(lowerLine, m.lowerQuery) {
		return true
	}
	if len(m.terms) > 1 {
		for _, term := range m.terms {
			if !strings.Contains(lowerLine, term) {
				return false
			}
		}
		return true
	}
	return false
}

func matchMeta(meta NoteMeta, m queryMatcher) int {
	score := 0
	if m.re != nil {
		if m.re.MatchString(meta.Title) {
			score += 5
		}
		if m.re.MatchString(meta.Path) {
			score += 3
		}
		for _, t := range meta.Tags {
			if m.re.MatchString(t) {
				score += 4
			}
		}
		for k, v := range meta.Metadata {
			if m.re.MatchString(k) || m.re.MatchString(v) {
				score += 2
			}
		}
		return score
	}

	// Build combined metadata searchable text for multi-term queries
	lowerTitle := strings.ToLower(meta.Title)
	lowerPath := strings.ToLower(meta.Path)

	if strings.Contains(lowerTitle, m.lowerQuery) {
		score += 5
	}
	if strings.Contains(lowerPath, m.lowerQuery) {
		score += 3
	}
	for _, t := range meta.Tags {
		if strings.Contains(t, m.lowerQuery) {
			score += 4
		}
	}
	for k, v := range meta.Metadata {
		if strings.Contains(strings.ToLower(k), m.lowerQuery) || strings.Contains(strings.ToLower(v), m.lowerQuery) {
			score += 2
		}
	}
	if score > 0 {
		return score
	}

	if len(m.terms) > 1 {
		var sb strings.Builder
		sb.WriteString(lowerTitle)
		sb.WriteByte(' ')
		sb.WriteString(lowerPath)
		for _, t := range meta.Tags {
			sb.WriteByte(' ')
			sb.WriteString(t)
		}
		for k, v := range meta.Metadata {
			sb.WriteByte(' ')
			sb.WriteString(strings.ToLower(k))
			sb.WriteByte(' ')
			sb.WriteString(strings.ToLower(v))
		}
		combined := sb.String()
		for _, term := range m.terms {
			if !strings.Contains(combined, term) {
				return 0
			}
		}
		return 2
	}

	return 0
}

func grepFile(absPath string, m queryMatcher, maxSnippets int) ([]SearchMatch, int) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 16*1024)
	scanner.Buffer(buf, 256*1024)

	var matches []SearchMatch
	totalHits := 0
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if m.matchesLine(line) {
			totalHits++
			if len(matches) < maxSnippets {
				snippet := strings.TrimSpace(line)
				if len(snippet) > 160 {
					snippet = snippet[:157] + "..."
				}
				matches = append(matches, SearchMatch{
					LineNumber: lineNum,
					Snippet:    snippet,
				})
			}
		}
	}

	return matches, totalHits
}
