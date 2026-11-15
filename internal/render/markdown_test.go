package render

import (
	"strings"
	"testing"
)

func TestRendererAndFrontmatterStripping(t *testing.T) {
	r := New()
	md := []byte(`---
title: Render Test
tags: [go, test]
---

# Heading One

Here is an image next to the note: ![pic](sample.png)

` + "```go\npackage main\nfunc hello() string { return \"world\" }\n```" + `
`)

	htmlOut, err := r.RenderForNote(md, "projects/2026/note.md")
	if err != nil {
		t.Fatalf("RenderForNote failed: %v", err)
	}
	if strings.Contains(htmlOut, "title: Render Test") {
		t.Errorf("expected YAML frontmatter to be stripped from rendered HTML, got: %s", htmlOut)
	}
	if !strings.Contains(htmlOut, `<h1 id="heading-one">Heading One</h1>`) {
		t.Errorf("expected auto-heading ID on h1, got: %s", htmlOut)
	}
	if !strings.Contains(htmlOut, `src="/assets/projects/2026/sample.png"`) {
		t.Errorf("expected relative image path to be resolved to /assets/projects/2026/sample.png, got: %s", htmlOut)
	}
	if !strings.Contains(htmlOut, `class="chroma"`) {
		t.Errorf("expected Chroma syntax highlighting class, got: %s", htmlOut)
	}

	tableMD := []byte("| Endpoint | Description |\n| :--- | :--- |\n| `GET /api/notes?q=...&mode=fulltext\\|meta&tag=...` | Search |\n")
	tableHTML, err := r.Render(tableMD)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tableHTML, "<code>GET /api/notes?q=...&amp;mode=fulltext|meta&amp;tag=...</code>") {
		t.Errorf("expected table code cell to render cleanly, got: %s", tableHTML)
	}

	css := ChromaCSS()
	for _, want := range []string{
		`:root[data-theme="light"] .chroma .k {`,
		`:root[data-theme="dark"] .chroma .k {`,
		"@media (prefers-color-scheme: light) {",
		"@media (prefers-color-scheme: dark) {",
		`:root:not([data-theme="dark"]) .chroma .k {`,
		`:root:not([data-theme="light"]) .chroma .k {`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("expected ChromaCSS to contain %q", want)
		}
	}
	// Every rule must be scoped to a theme so light and dark never mix.
	for _, line := range strings.Split(css, "\n") {
		if strings.Contains(line, "{") && !strings.HasPrefix(line, ":root") && !strings.HasPrefix(line, "@media") {
			t.Errorf("unscoped ChromaCSS rule: %q", line)
		}
	}
}
