package api

import (
	"bytes"
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	urlpath "path"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/updater"
	"github.com/lieyan/firescribe/internal/version"
)

type Server struct {
	app       *app.App
	webDir    string
	webFS     fs.FS
	updater   *updater.Updater
	updateCfg updater.Config
}

type UpdateRuntime struct {
	Updater *updater.Updater
	Config  updater.Config
}

func New(application *app.App, webDir string, updateRuntime ...UpdateRuntime) *Server {
	embedded, _ := embeddedStaticFS()
	server := &Server{app: application, webDir: webDir, webFS: embedded}
	if len(updateRuntime) > 0 {
		server.updater = updateRuntime[0].Updater
		server.updateCfg = updateRuntime[0].Config
	}
	return server
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(capturePeerAddr)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", s.health)
		r.Get("/version", s.version)
		r.Get("/update/status", s.updateStatus)
		r.Post("/update/check", s.requireUpdateToken(s.updateCheck))
		r.Post("/update/apply", s.requireUpdateToken(s.updateApply))
		r.Post("/update/dismiss", s.requireUpdateToken(s.updateDismiss))
		r.Get("/documents", s.listDocuments)
		r.Post("/documents/import", s.importDocument)
		r.Get("/documents/{documentID}", s.getDocument)
		r.Patch("/documents/{documentID}", s.patchDocument)
		r.Delete("/documents/{documentID}", s.deleteDocument)
		r.Get("/documents/{documentID}/assets", s.listDocumentAssets)
		r.Put("/documents/{documentID}/tags", s.setDocumentTags)
		r.Get("/documents/{documentID}/pages", s.listPages)
		r.Post("/documents/{documentID}/recognition-runs", s.startRecognition)
		r.Get("/documents/{documentID}/recognition-runs", s.listRecognitionRuns)
		r.Get("/documents/{documentID}/final-text", s.finalText)
		r.Post("/documents/{documentID}/exports", s.exportDocument)
		r.Get("/documents/{documentID}/annotations", s.listAnnotations)
		r.Post("/documents/{documentID}/annotations", s.createAnnotation)

		r.Get("/pages/{pageID}", s.getPage)
		r.Get("/pages/{pageID}/image", s.pageImage)
		r.Get("/pages/{pageID}/thumbnail", s.pageThumbnail)
		r.Get("/pages/{pageID}/recognition-results", s.listRecognitionResults)
		r.Get("/pages/{pageID}/text-versions", s.listTextVersions)
		r.Post("/pages/{pageID}/text-versions", s.createTextVersion)

		r.Get("/recognition-runs/{runID}", s.getRecognitionRun)
		r.Get("/search", s.search)
		r.Get("/tags", s.listTags)
		r.Get("/assets/{assetID}/download", s.downloadAsset)
		r.Get("/exports/{exportID}", s.getExport)
		r.Get("/exports/{exportID}/download", s.downloadExport)
		r.Get("/jobs", s.listJobs)
		r.Get("/jobs/{jobID}", s.getJob)
		r.Post("/jobs/{jobID}/cancel", s.cancelJob)
		r.Post("/jobs/{jobID}/retry", s.retryJob)
		r.Patch("/annotations/{annotationID}", s.patchAnnotation)
	})

	r.NotFound(s.staticFallback)
	r.Get("/*", s.staticFallback)
	return r
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) version(w http.ResponseWriter, r *http.Request) {
	info := version.Info()
	info["update_channel"] = normalizedUpdateChannel(s.updateCfg.Channel)
	info["update_repo"] = strings.TrimSpace(s.updateCfg.Repo)
	info["update_source"] = strings.TrimSpace(s.updateCfg.Source)
	writeJSON(w, http.StatusOK, info)
}

type peerAddrKeyType struct{}

var peerAddrKey peerAddrKeyType

// capturePeerAddr stores the TCP peer address before RealIP rewrites
// r.RemoteAddr from spoofable forwarding headers.
func capturePeerAddr(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), peerAddrKey, r.RemoteAddr)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isLoopbackRequest(r *http.Request) bool {
	addr, _ := r.Context().Value(peerAddrKey).(string)
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// requireUpdateToken guards mutating update endpoints. With update.admin_token
// configured, requests must present it (X-Admin-Token or Bearer). Without a
// token, only loopback connections may trigger updates.
func (s *Server) requireUpdateToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(s.updateCfg.AdminToken)
		if token == "" {
			if !isLoopbackRequest(r) {
				writeJSON(w, http.StatusForbidden, map[string]string{
					"error": "update actions are limited to localhost; set update.admin_token in config.json to allow remote access",
				})
				return
			}
			next(w, r)
			return
		}
		provided := strings.TrimSpace(r.Header.Get("X-Admin-Token"))
		if provided == "" {
			if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
				provided = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
			}
		}
		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or missing admin token"})
			return
		}
		next(w, r)
	}
}

func (s *Server) updateStatus(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "updater is not configured"})
		return
	}
	writeJSON(w, http.StatusOK, s.updater.Status())
}

func (s *Server) updateCheck(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "updater is not configured"})
		return
	}
	result, err := s.updater.CheckOnly(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"has_update":      false,
			"current_version": result.CurrentVersion,
			"channel":         result.Channel,
			"error":           err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) updateApply(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "updater is not configured"})
		return
	}
	status := s.updater.Status()
	if status.State == "ready" {
		if err := s.updater.ApplyPending(r.Context()); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "applying"})
		return
	}
	if status.State == "checking" || status.State == "downloading" || status.State == "applying" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "update already in progress"})
		return
	}
	s.updater.StartUpdate(r.Context())
	writeJSON(w, http.StatusOK, map[string]string{"status": "update_started"})
}

func (s *Server) updateDismiss(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "updater is not configured"})
		return
	}
	s.updater.DismissPending()
	writeJSON(w, http.StatusOK, map[string]string{"status": "dismissed"})
}

func (s *Server) listDocuments(w http.ResponseWriter, r *http.Request) {
	docs, err := s.app.Store.ListDocuments(r.Context(), app.DocumentFilter{
		Query:  r.URL.Query().Get("q"),
		Status: r.URL.Query().Get("status"),
		Tag:    r.URL.Query().Get("tag"),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, docs)
}

func (s *Server) importDocument(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(256 << 20); err != nil {
		writeError(w, fmt.Errorf("parse upload: %w", err))
		return
	}
	file, header, err := firstUploadedFile(r.MultipartForm)
	if err != nil {
		writeError(w, err)
		return
	}
	defer file.Close()

	doc, err := s.app.ImportDocument(r.Context(), header.Filename, file, app.ImportOptions{
		Title:       r.FormValue("title"),
		Description: r.FormValue("description"),
		Author:      r.FormValue("author"),
		Source:      r.FormValue("source"),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, doc)
}

func (s *Server) getDocument(w http.ResponseWriter, r *http.Request) {
	doc, err := s.app.Store.GetDocument(r.Context(), chi.URLParam(r, "documentID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) patchDocument(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
		Author      *string `json:"author"`
		Source      *string `json:"source"`
		Status      *string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, err)
		return
	}
	doc, err := s.app.Store.PatchDocument(r.Context(), chi.URLParam(r, "documentID"), req.Title, req.Description, req.Author, req.Source, req.Status)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) listDocumentAssets(w http.ResponseWriter, r *http.Request) {
	assets, err := s.app.Store.ListDocumentAssets(r.Context(), chi.URLParam(r, "documentID"))
	if err != nil {
		writeError(w, err)
		return
	}
	for i := range assets {
		assets[i].DownloadURL = "/api/assets/" + assets[i].ID + "/download"
	}
	writeJSON(w, http.StatusOK, assets)
}

func (s *Server) setDocumentTags(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Names []string `json:"names"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, err)
		return
	}
	tags, err := s.app.Store.SetDocumentTags(r.Context(), chi.URLParam(r, "documentID"), req.Names)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tags)
}

func (s *Server) deleteDocument(w http.ResponseWriter, r *http.Request) {
	if err := s.app.Store.DeleteDocument(r.Context(), chi.URLParam(r, "documentID")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listPages(w http.ResponseWriter, r *http.Request) {
	pages, err := s.app.Store.ListPageDetails(r.Context(), chi.URLParam(r, "documentID"))
	if err != nil {
		writeError(w, err)
		return
	}
	for i := range pages {
		addPageURLs(&pages[i])
	}
	writeJSON(w, http.StatusOK, pages)
}

func (s *Server) getPage(w http.ResponseWriter, r *http.Request) {
	page, err := s.app.Store.GetPageDetail(r.Context(), chi.URLParam(r, "pageID"))
	if err != nil {
		writeError(w, err)
		return
	}
	addPageURLs(&page)
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) pageImage(w http.ResponseWriter, r *http.Request) {
	page, err := s.app.Store.GetPage(r.Context(), chi.URLParam(r, "pageID"))
	if err != nil {
		writeError(w, err)
		return
	}
	s.serveAsset(w, r, page.ImageAssetID, false)
}

func (s *Server) pageThumbnail(w http.ResponseWriter, r *http.Request) {
	page, err := s.app.Store.GetPage(r.Context(), chi.URLParam(r, "pageID"))
	if err != nil {
		writeError(w, err)
		return
	}
	if page.ThumbAssetID == "" {
		s.serveAsset(w, r, page.ImageAssetID, false)
		return
	}
	s.serveAsset(w, r, page.ThumbAssetID, false)
}

func (s *Server) startRecognition(w http.ResponseWriter, r *http.Request) {
	start, err := s.app.StartRecognition(r.Context(), chi.URLParam(r, "documentID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, start)
}

func (s *Server) listRecognitionRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.app.Store.ListRecognitionRuns(r.Context(), chi.URLParam(r, "documentID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) getRecognitionRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.app.Store.GetRecognitionRun(r.Context(), chi.URLParam(r, "runID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) listRecognitionResults(w http.ResponseWriter, r *http.Request) {
	results, err := s.app.Store.ListRecognitionResults(r.Context(), chi.URLParam(r, "pageID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) listTextVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := s.app.Store.ListTextVersions(r.Context(), chi.URLParam(r, "pageID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

func (s *Server) createTextVersion(w http.ResponseWriter, r *http.Request) {
	pageID := chi.URLParam(r, "pageID")
	page, err := s.app.Store.GetPage(r.Context(), pageID)
	if err != nil {
		writeError(w, err)
		return
	}
	var req struct {
		Kind           string `json:"kind"`
		BaseVersionID  string `json:"base_version_id"`
		SourceResultID string `json:"source_result_id"`
		Text           string `json:"text"`
		Status         string `json:"status"`
		CreatedBy      string `json:"created_by"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, err)
		return
	}
	version, err := s.app.SaveTextVersion(r.Context(), app.TextVersion{
		DocumentID:     page.DocumentID,
		PageID:         pageID,
		Kind:           req.Kind,
		BaseVersionID:  req.BaseVersionID,
		SourceResultID: req.SourceResultID,
		Text:           req.Text,
		Status:         req.Status,
		CreatedBy:      req.CreatedBy,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, version)
}

func (s *Server) finalText(w http.ResponseWriter, r *http.Request) {
	pages, err := s.app.Store.ListPages(r.Context(), chi.URLParam(r, "documentID"))
	if err != nil {
		writeError(w, err)
		return
	}
	type pageText struct {
		PageID        string `json:"page_id"`
		PageNo        int    `json:"page_no"`
		TextVersionID string `json:"text_version_id"`
		Text          string `json:"text"`
	}
	response := struct {
		Pages []pageText `json:"pages"`
		Text  string     `json:"text"`
	}{Pages: []pageText{}}
	var joined []string
	for _, page := range pages {
		versionID, text, err := s.app.Store.LatestTextForPage(r.Context(), page.ID)
		if err != nil {
			writeError(w, err)
			return
		}
		response.Pages = append(response.Pages, pageText{PageID: page.ID, PageNo: page.PageNo, TextVersionID: versionID, Text: text})
		joined = append(joined, text)
	}
	response.Text = strings.Join(joined, "\n\n")
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	results, err := s.app.Store.Search(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) listTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.app.Store.ListTags(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tags)
}

func (s *Server) exportDocument(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Format             string `json:"format"`
		IncludePageNumbers bool   `json:"include_page_numbers"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	exportFile, err := s.app.ExportDocument(r.Context(), chi.URLParam(r, "documentID"), req.Format, req.IncludePageNumbers)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, exportFile)
}

func (s *Server) listAnnotations(w http.ResponseWriter, r *http.Request) {
	annotations, err := s.app.Store.ListAnnotations(r.Context(), chi.URLParam(r, "documentID"), r.URL.Query().Get("page_id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, annotations)
}

func (s *Server) createAnnotation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PageID        string `json:"page_id"`
		TextVersionID string `json:"text_version_id"`
		Kind          string `json:"kind"`
		Status        string `json:"status"`
		Body          string `json:"body"`
		AnchorJSON    string `json:"anchor_json"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, err)
		return
	}
	annotation, err := s.app.CreateAnnotation(r.Context(), app.Annotation{
		DocumentID:    chi.URLParam(r, "documentID"),
		PageID:        req.PageID,
		TextVersionID: req.TextVersionID,
		Kind:          req.Kind,
		Status:        req.Status,
		Body:          req.Body,
		AnchorJSON:    req.AnchorJSON,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, annotation)
}

func (s *Server) patchAnnotation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status *string `json:"status"`
		Body   *string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, err)
		return
	}
	annotation, err := s.app.Store.PatchAnnotation(r.Context(), chi.URLParam(r, "annotationID"), req.Status, req.Body)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, annotation)
}

func (s *Server) getExport(w http.ResponseWriter, r *http.Request) {
	asset, _, err := s.app.AssetPath(r.Context(), chi.URLParam(r, "exportID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, asset)
}

func (s *Server) downloadExport(w http.ResponseWriter, r *http.Request) {
	s.serveAsset(w, r, chi.URLParam(r, "exportID"), true)
}

func (s *Server) downloadAsset(w http.ResponseWriter, r *http.Request) {
	s.serveAsset(w, r, chi.URLParam(r, "assetID"), true)
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.app.Store.ListJobs(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.app.Store.GetJob(r.Context(), chi.URLParam(r, "jobID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	if err := s.app.Store.MarkJobCanceled(r.Context(), chi.URLParam(r, "jobID")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) retryJob(w http.ResponseWriter, r *http.Request) {
	start, err := s.app.RetryJob(r.Context(), chi.URLParam(r, "jobID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, start)
}

func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request, assetID string, attachment bool) {
	if assetID == "" {
		writeError(w, sql.ErrNoRows)
		return
	}
	asset, path, err := s.app.AssetPath(r.Context(), assetID)
	if err != nil {
		writeError(w, err)
		return
	}
	if attachment {
		filename := filepath.Base(asset.StoragePath)
		if strings.TrimSpace(asset.OriginalName) != "" {
			filename = filepath.Base(asset.OriginalName)
		}
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	}
	if asset.MimeType != "" {
		w.Header().Set("Content-Type", asset.MimeType)
	}
	http.ServeFile(w, r, path)
}

func (s *Server) staticFallback(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	requestPath := cleanStaticPath(r.URL.Path)

	if s.webDir != "" {
		fullPath := filepath.Join(s.webDir, requestPath)
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			http.ServeFile(w, r, fullPath)
			return
		}
		indexPath := filepath.Join(s.webDir, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			http.ServeFile(w, r, indexPath)
			return
		}
	}

	if s.webFS != nil {
		s.serveEmbeddedStatic(w, r, requestPath)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) serveEmbeddedStatic(w http.ResponseWriter, r *http.Request, requestPath string) {
	if requestPath == "" || requestPath == "." {
		requestPath = "index.html"
	}
	requestPath = filepath.ToSlash(requestPath)
	if info, err := fs.Stat(s.webFS, requestPath); err == nil && !info.IsDir() {
		s.serveEmbeddedFile(w, r, requestPath)
		return
	}
	s.serveEmbeddedFile(w, r, "index.html")
}

func (s *Server) serveEmbeddedFile(w http.ResponseWriter, r *http.Request, name string) {
	data, err := fs.ReadFile(s.webFS, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if contentType := contentTypeFor(name); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(w, r, filepath.Base(name), time.Time{}, bytes.NewReader(data))
}

func cleanStaticPath(path string) string {
	clean := urlpath.Clean("/" + strings.TrimPrefix(path, "/"))
	rel := strings.TrimPrefix(clean, "/")
	if rel == "" || rel == "." {
		return "index.html"
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." {
			return "index.html"
		}
	}
	return filepath.FromSlash(rel)
}

func contentTypeFor(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	default:
		return mime.TypeByExtension(filepath.Ext(name))
	}
}

func normalizedUpdateChannel(channel string) string {
	channel = strings.ToLower(strings.TrimSpace(channel))
	if channel == "" {
		return "stable"
	}
	if channel != "stable" {
		return "dev"
	}
	return channel
}

func firstUploadedFile(form *multipart.Form) (multipart.File, *multipart.FileHeader, error) {
	if form == nil {
		return nil, nil, errors.New("missing multipart form")
	}
	for _, key := range []string{"file", "files"} {
		files := form.File[key]
		if len(files) > 0 {
			f, err := files[0].Open()
			return f, files[0], err
		}
	}
	for _, files := range form.File {
		if len(files) > 0 {
			f, err := files[0].Open()
			return f, files[0], err
		}
	}
	return nil, nil, errors.New("upload field \"file\" is required")
}

func addPageURLs(page *app.PageDetail) {
	page.ImageURL = "/api/pages/" + page.PageID + "/image"
	page.ThumbnailURL = "/api/pages/" + page.PageID + "/thumbnail"
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, sql.ErrNoRows) {
		status = http.StatusNotFound
	} else {
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "unsupported") || strings.Contains(message, "required") || strings.Contains(message, "parse") {
			status = http.StatusBadRequest
		}
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
