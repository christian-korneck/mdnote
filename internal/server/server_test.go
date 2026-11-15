package server

import (
	"bytes"
	"encoding/json"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/christian-korneck/mdnote/internal/render"
	"github.com/christian-korneck/mdnote/internal/settings"
	"github.com/christian-korneck/mdnote/internal/store"
)

func TestServerAPIEndpoints(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rnd := render.New()
	srv := New(st, rnd, settings.NewFile(filepath.Join(t.TempDir(), "config.json")))
	ts := httptest.NewServer(srv)
	defer ts.Close()

	// 0. GET / and GET /static/chroma.css
	uiResp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer uiResp.Body.Close()
	if uiResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on /, got %d", uiResp.StatusCode)
	}

	cssResp, err := http.Get(ts.URL + "/static/chroma.css")
	if err != nil {
		t.Fatal(err)
	}
	defer cssResp.Body.Close()
	if cssResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on /static/chroma.css, got %d", cssResp.StatusCode)
	}

	// 1. POST /api/notes to create a note with Go code block and tags
	payloadBytes, _ := json.Marshal(createNoteRequest{
		Path:    "dev/golang-guide.md",
		Content: "---\ntitle: Go Guide\ntags: [golang]\n---\n\n# Go Guide\n\nCheck out #concurrency:\n\n```go\npackage main\nfunc main() {}\n```\n",
	})
	resp, err := http.Post(ts.URL+"/api/notes", "application/json", bytes.NewReader(payloadBytes))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", resp.StatusCode)
	}

	// 2. GET /api/notes/dev/golang-guide.md and verify syntax-highlighted HTML
	getResp, err := http.Get(ts.URL + "/api/notes/dev/golang-guide.md")
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", getResp.StatusCode)
	}
	var note store.Note
	if err := json.NewDecoder(getResp.Body).Decode(&note); err != nil {
		t.Fatal(err)
	}
	if note.Title != "Go Guide" {
		t.Errorf("expected title 'Go Guide', got %q", note.Title)
	}
	if !strings.Contains(note.HTML, "chroma") {
		t.Errorf("expected Chroma syntax highlighting class in rendered HTML, got: %s", note.HTML)
	}

	// 3. GET /api/notes?q=concurrency&mode=fulltext
	searchResp, err := http.Get(ts.URL + "/api/notes?q=concurrency&mode=fulltext")
	if err != nil {
		t.Fatal(err)
	}
	defer searchResp.Body.Close()
	var qr store.QueryResult
	if err := json.NewDecoder(searchResp.Body).Decode(&qr); err != nil {
		t.Fatal(err)
	}
	if qr.Total != 1 || qr.Items[0].Path != "dev/golang-guide.md" {
		t.Errorf("unexpected search result: %+v", qr)
	}

	// 4. PUT /api/notes/dev/golang-guide.md to update content
	putReq, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/notes/dev/golang-guide.md", strings.NewReader(`{"content":"# Updated Guide\n\nWith #updated tag."}`))
	putReq.Header.Set("Content-Type", "application/json")
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on PUT, got %d", putResp.StatusCode)
	}

	// 5. GET /api/tags, GET /api/folders, and POST /api/refresh
	refResp, err := http.Post(ts.URL+"/api/refresh", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer refResp.Body.Close()
	if refResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on /api/refresh, got %d", refResp.StatusCode)
	}

	tagsResp, err := http.Get(ts.URL + "/api/tags")
	if err != nil {
		t.Fatal(err)
	}
	defer tagsResp.Body.Close()
	var tags []store.TagCount
	_ = json.NewDecoder(tagsResp.Body).Decode(&tags)
	if len(tags) != 1 || tags[0].Tag != "updated" {
		t.Errorf("expected tag 'updated', got %+v", tags)
	}

	foldersResp, err := http.Get(ts.URL + "/api/folders")
	if err != nil {
		t.Fatal(err)
	}
	defer foldersResp.Body.Close()
	var folders []string
	_ = json.NewDecoder(foldersResp.Body).Decode(&folders)
	if len(folders) != 1 || folders[0] != "dev" {
		t.Errorf("expected folder 'dev', got %+v", folders)
	}

	// 5b. POST /api/folders to create a new nested folder
	createFolderResp, err := http.Post(ts.URL+"/api/folders", "application/json", strings.NewReader(`{"path":"projects/2026"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer createFolderResp.Body.Close()
	if createFolderResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created on POST /api/folders, got %d", createFolderResp.StatusCode)
	}

	// 6. POST /api/render
	renderResp, err := http.Post(ts.URL+"/api/render", "application/json", strings.NewReader(`{"content":"**bold**"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer renderResp.Body.Close()
	var rOut renderResponse
	_ = json.NewDecoder(renderResp.Body).Decode(&rOut)
	if !strings.Contains(rOut.HTML, "<strong>bold</strong>") {
		t.Errorf("expected rendered bold HTML, got %q", rOut.HTML)
	}

	// 7. POST /api/notes/rename
	renameResp, err := http.Post(ts.URL+"/api/notes/rename", "application/json", strings.NewReader(`{"oldPath":"dev/golang-guide.md","newPath":"dev/go-renamed.md"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer renameResp.Body.Close()
	if renameResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on rename, got %d", renameResp.StatusCode)
	}

	// 8. DELETE /api/notes/dev/go-renamed.md
	delReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/notes/dev/go-renamed.md", nil)
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatal(err)
	}
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 No Content on DELETE, got %d", delResp.StatusCode)
	}

	// 9. POST /api/assets (simulate clipboard image paste upload next to a note)
	var mpBuf bytes.Buffer
	mw := multipart.NewWriter(&mpBuf)
	_ = mw.WriteField("notePath", "projects/2026/roadmap.md")
	fw, err := mw.CreateFormFile("file", "pasted.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte("PNG_IMAGE_PAYLOAD"))
	_ = mw.Close()

	assetResp, err := http.Post(ts.URL+"/api/assets", mw.FormDataContentType(), &mpBuf)
	if err != nil {
		t.Fatal(err)
	}
	defer assetResp.Body.Close()
	if assetResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created for asset upload, got %d", assetResp.StatusCode)
	}
	var uploaded assetUploadResponse
	if err := json.NewDecoder(assetResp.Body).Decode(&uploaded); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(uploaded.Path, "projects/2026/") || !strings.HasPrefix(uploaded.URL, "/assets/projects/2026/") {
		t.Errorf("expected asset saved next to note in projects/2026/, got: %+v", uploaded)
	}
	if strings.Contains(uploaded.Markdown, "/") {
		t.Errorf("expected relative filename in Markdown link, got: %s", uploaded.Markdown)
	}

	// Verify GET /assets/... serves the uploaded image
	fetchImg, err := http.Get(ts.URL + uploaded.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer fetchImg.Body.Close()
	imgBytes, _ := io.ReadAll(fetchImg.Body)
	if string(imgBytes) != "PNG_IMAGE_PAYLOAD" {
		t.Errorf("expected uploaded image payload, got %q", string(imgBytes))
	}

	// 10. POST /api/assets with a raw body (what the web UI sends; see uploadImageAndInsert in app.js)
	rawResp, err := http.Post(ts.URL+"/api/assets?notePath=projects/2026/roadmap.md&filename=drop.jpg",
		"application/octet-stream", strings.NewReader("JPEG_RAW_PAYLOAD"))
	if err != nil {
		t.Fatal(err)
	}
	defer rawResp.Body.Close()
	if rawResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created for raw asset upload, got %d", rawResp.StatusCode)
	}
	var rawUploaded assetUploadResponse
	if err := json.NewDecoder(rawResp.Body).Decode(&rawUploaded); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rawUploaded.Path, "projects/2026/") || !strings.HasSuffix(rawUploaded.Path, ".jpg") {
		t.Errorf("expected raw upload saved as .jpg next to note in projects/2026/, got: %+v", rawUploaded)
	}
	rawImg, err := http.Get(ts.URL + rawUploaded.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer rawImg.Body.Close()
	rawBytes, _ := io.ReadAll(rawImg.Body)
	if string(rawBytes) != "JPEG_RAW_PAYLOAD" {
		t.Errorf("expected raw uploaded payload, got %q", string(rawBytes))
	}
}

func TestSettingsEndpoints(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "mdnote", "config.json")
	ts := httptest.NewServer(New(st, render.New(), settings.NewFile(cfgPath)))
	defer ts.Close()

	get := func(path string) string {
		t.Helper()
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return string(body)
	}
	patch := func(body string) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/settings", strings.NewReader(body))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	var got settings.Settings
	if err := json.Unmarshal([]byte(get("/api/settings")), &got); err != nil || got != settings.Defaults() {
		t.Fatalf("GET /api/settings = %+v, %v; want defaults", got, err)
	}
	dirAttrs := ` data-notes-dir="` + html.EscapeString(st.RootDir()) + `" data-path-sep="` + string(filepath.Separator) + `">`
	if !strings.Contains(get("/"), `<html lang="en" data-autosave="true" style="--split-ratio: 50%"`+dirAttrs) {
		t.Fatal("GET / doesn't carry the default settings on <html>")
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatalf("config file created without a change (stat err %v)", err)
	}

	if code := patch(`{"theme":"dark","splitRatio":40,"autosave":false}`); code != http.StatusOK {
		t.Fatalf("PATCH /api/settings = %d, want 200", code)
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("config file not written: %v", err)
	}
	if !strings.Contains(get("/"), `<html lang="en" data-theme="dark" data-autosave="false" style="--split-ratio: 40%"`+dirAttrs) {
		t.Fatal("GET / doesn't carry the patched settings on <html>")
	}

	if code := patch(`{"theme":"blue"}`); code != http.StatusBadRequest {
		t.Fatalf("PATCH with an invalid theme = %d, want 400", code)
	}
	if code := patch(`not json`); code != http.StatusBadRequest {
		t.Fatalf("PATCH with invalid JSON = %d, want 400", code)
	}
}

func TestSettingsNeverExposeAPIToken(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"apiToken":"s3cret-token","theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(New(st, render.New(), settings.NewFile(cfgPath)))
	defer ts.Close()
	for _, path := range []string{"/api/settings", "/"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if strings.Contains(string(body), "s3cret-token") || strings.Contains(string(body), "apiToken") {
			t.Errorf("GET %s exposes the API token", path)
		}
	}
}
