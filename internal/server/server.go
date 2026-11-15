package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/christian-korneck/mdnote/internal/render"
	"github.com/christian-korneck/mdnote/internal/settings"
	"github.com/christian-korneck/mdnote/internal/store"
	"github.com/christian-korneck/mdnote/internal/web"
)

// Server wires the filesystem note store, Markdown renderer, and HTTP routes.
type Server struct {
	store     *store.Store
	renderer  *render.Renderer
	settings  *settings.File
	chromaCSS string
	mux       *http.ServeMux
}

// New creates a configured HTTP Server for mdnote. set is the shared UI settings file.
func New(st *store.Store, rnd *render.Renderer, set *settings.File) *Server {
	s := &Server{
		store:     st,
		renderer:  rnd,
		settings:  set,
		chromaCSS: render.ChromaCSS(),
		mux:       http.NewServeMux(),
	}
	s.registerRoutes()
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) registerRoutes() {
	// Web UI & Static Assets
	s.mux.HandleFunc("GET /{$}", s.handleIndex)
	s.mux.HandleFunc("GET /static/chroma.css", s.handleChromaCSS)

	staticSub, _ := fs.Sub(web.StaticFS, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// Uploaded Note Assets (Images stored alongside notes in RootDir)
	s.mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir(s.store.RootDir()))))

	// JSON REST API
	s.mux.HandleFunc("POST /api/refresh", s.handleRefresh)
	s.mux.HandleFunc("GET /api/notes", s.handleListNotes)
	s.mux.HandleFunc("POST /api/notes", s.handleCreateNote)
	s.mux.HandleFunc("POST /api/notes/rename", s.handleRenameNote)
	s.mux.HandleFunc("GET /api/notes/{path...}", s.handleGetNote)
	s.mux.HandleFunc("PUT /api/notes/{path...}", s.handlePutNote)
	s.mux.HandleFunc("DELETE /api/notes/{path...}", s.handleDeleteNote)
	s.mux.HandleFunc("POST /api/render", s.handleRender)
	s.mux.HandleFunc("GET /api/tags", s.handleListTags)
	s.mux.HandleFunc("GET /api/folders", s.handleListFolders)
	s.mux.HandleFunc("POST /api/folders", s.handleCreateFolder)
	s.mux.HandleFunc("POST /api/assets", s.handleUploadAsset)
	s.mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	s.mux.HandleFunc("PATCH /api/settings", s.handlePatchSettings)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := web.StaticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "UI index not found", http.StatusInternalServerError)
		return
	}
	// Put the settings on <html> so the theme and split divider are right on first paint.
	data = bytes.Replace(data, []byte(`<html lang="en">`), []byte(htmlTag(s.loadSettings(), s.store.RootDir())), 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

// htmlTag returns the <html> start tag carrying the settings and the notes
// directory (for showing absolute file paths). "system" is the absence of
// data-theme, so the CSS follows prefers-color-scheme.
func htmlTag(set settings.Settings, rootDir string) string {
	var b strings.Builder
	b.WriteString(`<html lang="en"`)
	if set.Theme != "system" {
		fmt.Fprintf(&b, ` data-theme="%s"`, set.Theme) // validated: light or dark
	}
	fmt.Fprintf(&b, ` data-autosave="%t" style="--split-ratio: %s%%"`,
		set.Autosave, strconv.FormatFloat(set.SplitRatio, 'f', -1, 64))
	fmt.Fprintf(&b, ` data-notes-dir="%s" data-path-sep="%s">`,
		html.EscapeString(rootDir), html.EscapeString(string(filepath.Separator)))
	return b.String()
}

// loadSettings returns the current settings, or the defaults if the config file is unreadable.
func (s *Server) loadSettings() settings.Settings {
	set, err := s.settings.Load()
	if err != nil {
		log.Printf("settings: %v (using defaults)", err)
	}
	return set
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.loadSettings())
}

func (s *Server) handlePatchSettings(w http.ResponseWriter, r *http.Request) {
	var p settings.Patch
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	set, err := s.settings.Update(p)
	switch {
	case errors.Is(err, settings.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
	default:
		writeJSON(w, http.StatusOK, set)
	}
}

func (s *Server) handleChromaCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte(s.chromaCSS))
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	total, err := s.store.Refresh()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"total":  total,
	})
}

func (s *Server) handleListNotes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	mode := store.ModeFullText
	if strings.EqualFold(q.Get("mode"), string(store.ModeMeta)) {
		mode = store.ModeMeta
	}

	res, err := s.store.Search(store.QueryOptions{
		Query:  q.Get("q"),
		Mode:   mode,
		Tag:    q.Get("tag"),
		Folder: q.Get("folder"),
		Sort:   q.Get("sort"),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type createNoteRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (s *Server) handleCreateNote(w http.ResponseWriter, r *http.Request) {
	var req createNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	meta, err := s.store.Save(req.Path, []byte(req.Content), true)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, meta)
}

type renameNoteRequest struct {
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
}

func (s *Server) handleRenameNote(w http.ResponseWriter, r *http.Request) {
	var req renameNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	meta, err := s.store.Rename(req.OldPath, req.NewPath)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (s *Server) handleGetNote(w http.ResponseWriter, r *http.Request) {
	relPath := r.PathValue("path")
	note, err := s.store.Get(relPath)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	rendered, err := s.renderer.RenderForNote([]byte(note.Content), note.Path)
	if err == nil {
		note.HTML = rendered
	}
	writeJSON(w, http.StatusOK, note)
}

type putNoteRequest struct {
	Content string `json:"content"`
}

func (s *Server) handlePutNote(w http.ResponseWriter, r *http.Request) {
	relPath := r.PathValue("path")
	var req putNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	meta, err := s.store.Save(relPath, []byte(req.Content), false)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	relPath := r.PathValue("path")
	if err := s.store.Delete(relPath); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type renderRequest struct {
	Content string `json:"content"`
	Path    string `json:"path"`
}

type renderResponse struct {
	HTML string `json:"html"`
}

func (s *Server) handleRender(w http.ResponseWriter, r *http.Request) {
	var req renderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	htmlOut, err := s.renderer.RenderForNote([]byte(req.Content), req.Path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, renderResponse{HTML: htmlOut})
}

func (s *Server) handleListTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.store.ListTags()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tags)
}

func (s *Server) handleListFolders(w http.ResponseWriter, r *http.Request) {
	folders, err := s.store.ListFolders()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, folders)
}

type createFolderRequest struct {
	Path string `json:"path"`
}

func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	var req createFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	folder, err := s.store.CreateFolder(req.Path)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"folder": folder})
}

type assetUploadResponse struct {
	Path     string `json:"path"`
	URL      string `json:"url"`
	Markdown string `json:"markdown"`
}

func (s *Server) handleUploadAsset(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")
	notePath := r.URL.Query().Get("notePath")
	var assetRelPath, localName string

	if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "failed to parse multipart form")
			return
		}
		if formNotePath := r.FormValue("notePath"); formNotePath != "" {
			notePath = formNotePath
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "missing 'file' field in multipart upload")
			return
		}
		defer file.Close()

		assetRelPath, localName, err = s.store.SaveAsset(notePath, header.Filename, file)
		if err != nil {
			writeStoreError(w, err)
			return
		}
	} else {
		filename := r.URL.Query().Get("filename")
		if filename == "" {
			filename = "image.png"
		}
		var err error
		assetRelPath, localName, err = s.store.SaveAsset(notePath, filename, r.Body)
		if err != nil {
			writeStoreError(w, err)
			return
		}
	}

	urlPath := "/assets/" + filepath.ToSlash(assetRelPath)
	writeJSON(w, http.StatusCreated, assetUploadResponse{
		Path:     assetRelPath,
		URL:      urlPath,
		Markdown: fmt.Sprintf("![image](%s)", localName),
	})
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrInvalidPath):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrAlreadyExists):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
