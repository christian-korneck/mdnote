package app

import (
	"fmt"

	"github.com/christian-korneck/mdnote/internal/store"
)

const welcomeMarkdown = `---
title: Welcome to mdnote
tags: [welcome, golang, markdown]
author: mdnote
---

# Welcome to mdnote

**mdnote** is a fast, side-by-side Markdown notes app backed directly by a folder of ` + "`.md`" + ` files on disk.

## Key Features

- **Filesystem Native**: Drop or edit ` + "`.md`" + ` files directly in your notes folder at any time.
- **Full-Text & Metadata Search**: Use the toggle next to the search bar to switch between concurrent grep-style **Full-text** search and **Title/Meta** search.
- **Grep-Style Tags**: Add tags in YAML frontmatter or inline like #productivity #notes #architecture.
- **Copy & Paste Images**: Paste an image from your clipboard (` + "`Ctrl+V` / `Cmd+V`" + `) or drag-and-drop an image file directly into the editor.
- **Server-Side Syntax Highlighting**: Powered by Goldmark + Chroma:

` + "```go" + `
package main

import "fmt"

func main() {
	fmt.Println("Hello from mdnote!")
}
` + "```" + `

## REST API Quick Reference

| Endpoint | Description |
| :--- | :--- |
| ` + "`GET /api/notes?q=...&mode=fulltext\\|meta&tag=...`" + ` | Search & list notes |
| ` + "`GET /api/notes/{path}`" + ` | Read note content, metadata & HTML |
| ` + "`PUT /api/notes/{path}`" + ` | Create or update a note |
| ` + "`POST /api/assets`" + ` | Upload an image asset |
| ` + "`GET /api/tags`" + ` | List all tags with counts |
`

func seedFolders(st *store.Store, count int) ([]string, error) {
	domains := []string{"engineering", "projects", "journal", "architecture", "ops", "research", "design", "security"}
	subareas := []string{"backend", "frontend", "infra", "storage", "search", "api", "roadmap", "runbooks", "rfcs", "notes"}
	created := make([]string, 0, count)

	for i := 1; i <= count; i++ {
		d := domains[i%len(domains)]
		sub := subareas[(i/len(domains))%len(subareas)]
		var folderPath string
		if i%3 == 0 {
			folderPath = fmt.Sprintf("%s/%s/group-%04d", d, sub, i)
		} else {
			folderPath = fmt.Sprintf("%s/folder-%04d", d, i)
		}
		norm, err := st.CreateFolder(folderPath)
		if err != nil {
			return nil, err
		}
		created = append(created, norm)
	}
	return created, nil
}

func seedNotes(st *store.Store, count int, folders []string) error {
	if len(folders) == 0 {
		folders = []string{"engineering", "projects", "journal", "architecture", "ops/runbooks"}
	}
	tags := []string{"go", "backend", "search", "markdown", "design", "performance", "linux", "security"}
	for i := 1; i <= count; i++ {
		folder := folders[i%len(folders)]
		t1 := tags[i%len(tags)]
		t2 := tags[(i*3)%len(tags)]
		relPath := fmt.Sprintf("%s/note-%05d.md", folder, i)
		body := fmt.Sprintf(`---
title: Technical Note %05d - %s
tags: [%s, %s]
owner: team-%d
---

# Technical Note %05d

This note belongs to **%s** and tracks #milestone-%d.

## Code Sample

`+"```go\nfunc computeMetric%d() int {\n\treturn %d * 42\n}\n```\n"+`
Unique search token: token_%05d_xyz.
`, i, folder, t1, t2, i%5, i, folder, i%10, i, i, i)

		if _, err := st.Save(relPath, []byte(body), false); err != nil {
			return err
		}
	}
	return nil
}
