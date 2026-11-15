package render

import (
	"bytes"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

// Renderer converts Markdown content into syntax-highlighted HTML.
type Renderer struct {
	md goldmark.Markdown
}

// New creates a new Markdown Renderer with GitHub Flavored Markdown and Chroma syntax highlighting.
func New() *Renderer {
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			highlighting.NewHighlighting(
				highlighting.WithStyle("github"),
				highlighting.WithFormatOptions(
					chromahtml.WithClasses(true),
					chromahtml.WithLineNumbers(false),
				),
			),
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
			html.WithXHTML(),
		),
	)
	return &Renderer{md: md}
}

var frontmatterRegex = regexp.MustCompile(`(?s)\A---\r?\n.*?\r?\n---(?:\r?\n|\z)`)
var imgSrcRegex = regexp.MustCompile(`(<img\b[^>]*?\bsrc=")([^"]+)(")`)

// Render converts raw Markdown (stripping optional YAML frontmatter) into HTML.
func (r *Renderer) Render(markdown []byte) (string, error) {
	return r.RenderForNote(markdown, "")
}

// RenderForNote converts raw Markdown into HTML and resolves relative image URLs
// relative to the directory of noteRelPath so images stored next to the .md file render in the UI.
func (r *Renderer) RenderForNote(markdown []byte, noteRelPath string) (string, error) {
	body := StripFrontmatter(markdown)
	var buf bytes.Buffer
	if err := r.md.Convert(body, &buf); err != nil {
		return "", err
	}
	out := buf.String()

	noteDir := ""
	if strings.TrimSpace(noteRelPath) != "" {
		d := filepath.ToSlash(filepath.Dir(strings.TrimPrefix(filepath.ToSlash(noteRelPath), "/")))
		if d != "." && d != "/" {
			noteDir = d
		}
	}

	out = imgSrcRegex.ReplaceAllStringFunc(out, func(match string) string {
		sub := imgSrcRegex.FindStringSubmatch(match)
		if len(sub) != 4 {
			return match
		}
		src := strings.TrimSpace(sub[2])
		if src == "" ||
			strings.HasPrefix(src, "http://") ||
			strings.HasPrefix(src, "https://") ||
			strings.HasPrefix(src, "data:") ||
			strings.HasPrefix(src, "/") {
			return match
		}
		resolved := src
		if noteDir != "" {
			resolved = path.Clean(noteDir + "/" + src)
		} else {
			resolved = path.Clean(src)
		}
		if strings.HasPrefix(resolved, "..") {
			return match
		}
		return sub[1] + "/assets/" + strings.TrimPrefix(resolved, "/") + sub[3]
	})

	return out, nil
}

// StripFrontmatter removes leading YAML frontmatter delimited by --- lines.
func StripFrontmatter(content []byte) []byte {
	loc := frontmatterRegex.FindIndex(content)
	if loc != nil && loc[0] == 0 {
		return content[loc[1]:]
	}
	return content
}

// Chroma styles used for the light and dark UI themes. Rendered HTML only carries
// token classes, so switching themes is purely a matter of which CSS rules apply.
const (
	lightChromaStyle = "github"
	darkChromaStyle  = "github-dark"
)

// ChromaCSS returns the CSS rules for Chroma syntax highlighting classes in a light
// and a dark variant. A variant applies when <html data-theme> names it explicitly,
// or when data-theme is absent and the OS color scheme matches. The two rule sets
// never apply at the same time, so properties from one theme can't leak into the other.
func ChromaCSS() string {
	var b strings.Builder
	writeThemedChromaCSS(&b, lightChromaStyle, "light", "dark")
	writeThemedChromaCSS(&b, darkChromaStyle, "dark", "light")
	return strings.TrimSpace(b.String())
}

func writeThemedChromaCSS(b *strings.Builder, styleName, mode, otherMode string) {
	rules := chromaRules(styleName)
	explicit := fmt.Sprintf(`:root[data-theme="%s"] `, mode)
	system := fmt.Sprintf(`:root:not([data-theme="%s"]) `, otherMode)

	for _, rule := range rules {
		b.WriteString(explicit + rule + "\n")
	}
	fmt.Fprintf(b, "@media (prefers-color-scheme: %s) {\n", mode)
	for _, rule := range rules {
		b.WriteString(system + rule + "\n")
	}
	b.WriteString("}\n")
}

// chromaRules returns Chroma's "selector { declarations }" rules for one style.
// The .bg and .chroma wrapper rules are dropped so the code block background and
// base text color come from the app stylesheet's theme variables.
func chromaRules(styleName string) []string {
	style := styles.Get(styleName)
	if style == nil {
		style = styles.Fallback
	}
	var buf bytes.Buffer
	formatter := chromahtml.New(chromahtml.WithClasses(true), chromahtml.WithCSSComments(false))
	_ = formatter.WriteCSS(&buf, style)

	var rules []string
	for _, line := range strings.Split(buf.String(), "\n") {
		line = strings.TrimSpace(line)
		selector, _, ok := strings.Cut(line, " {")
		if !ok || selector == ".bg" || selector == ".chroma" {
			continue
		}
		rules = append(rules, line)
	}
	return rules
}
