package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hookstash/hookstash/internal/capture"
	"github.com/hookstash/hookstash/internal/store"
)

type Store interface {
	CreateRequest(ctx context.Context, req store.CapturedRequest) error
	ListRequests(ctx context.Context) ([]store.CapturedRequest, error)
	GetRequest(ctx context.Context, id string) (store.CapturedRequest, error)
	UpdateForwardResult(ctx context.Context, id string, status string, statusCode *int, forwardError *string, durationMS int64, targetURL string) error
}

type Config struct {
	Store      Store
	ForwardURL string
}

type Server struct {
	store      Store
	forwardURL string
	forwarder  capture.Forwarder
	mux        *http.ServeMux
}

func New(cfg Config) http.Handler {
	s := &Server{
		store:      cfg.Store,
		forwardURL: cfg.ForwardURL,
		forwarder:  capture.NewForwarder(nil),
		mux:        http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/requests", s.handleListRequests)
	s.mux.HandleFunc("GET /api/requests/{id}", s.handleGetRequest)
	s.mux.HandleFunc("/hooks/default", s.handleCapture)
	s.mux.HandleFunc("/", s.handleDashboard)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
	})
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	distDir := filepath.Join("web", "dist")
	indexPath := filepath.Join(distDir, "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		path := filepath.Clean(filepath.Join(distDir, r.URL.Path))
		if strings.HasPrefix(path, filepath.Clean(distDir)) {
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				http.ServeFile(w, r, path)
				return
			}
		}
		http.ServeFile(w, r, indexPath)
		return
	}

	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, "<!doctype html><title>Hookstash</title><h1>Hookstash</h1><p>Build the dashboard with npm run build in web, then restart Hookstash.</p>")
}

func (s *Server) handleCapture(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/hooks/default" {
		http.NotFound(w, r)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	_ = r.Body.Close()

	headersJSON, err := json.Marshal(headerMap(r.Header))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not serialize request headers")
		return
	}

	targetURL := stringPtrOrNil(s.forwardURL)
	forwardStatus := "not_configured"
	if s.forwardURL != "" {
		forwardStatus = "queued"
	}

	req := store.CapturedRequest{
		ID:            newRequestID(),
		Method:        r.Method,
		Path:          r.URL.Path,
		QueryString:   r.URL.RawQuery,
		HeadersJSON:   string(headersJSON),
		Body:          body,
		BodyText:      string(body),
		ContentType:   r.Header.Get("Content-Type"),
		RemoteAddr:    r.RemoteAddr,
		ReceivedAt:    time.Now().UTC(),
		ProviderHint:  capture.ProviderHint(r.Header, r.URL.Path),
		ForwardStatus: forwardStatus,
		TargetURL:     targetURL,
	}

	if err := s.store.CreateRequest(r.Context(), req); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save captured request")
		return
	}

	if s.forwardURL != "" {
		result := s.forwarder.Forward(r.Context(), capture.ForwardRequest{
			Method:      r.Method,
			TargetURL:   s.forwardURL,
			QueryString: r.URL.RawQuery,
			Headers:     r.Header,
			Body:        body,
		})
		req.ForwardStatus = result.Status
		req.ForwardStatusCode = result.StatusCode
		req.ForwardError = result.Error
		req.ForwardDurationMS = &result.DurationMS
		req.TargetURL = &result.TargetURL

		if err := s.store.UpdateForwardResult(r.Context(), req.ID, result.Status, result.StatusCode, result.Error, result.DurationMS, result.TargetURL); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save forward result")
			return
		}
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"id":             req.ID,
		"captured":       true,
		"forward_status": req.ForwardStatus,
		"forward_error":  req.ForwardError,
	})
}

func (s *Server) handleListRequests(w http.ResponseWriter, r *http.Request) {
	requests, err := s.store.ListRequests(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list requests")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"requests": requests,
	})
}

func (s *Server) handleGetRequest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	req, err := s.store.GetRequest(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load request")
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func headerMap(headers http.Header) map[string][]string {
	out := make(map[string][]string, len(headers))
	for key, values := range headers {
		copied := make([]string, len(values))
		copy(copied, values)
		out[key] = copied
	}
	return out
}

func newRequestID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "req_" + strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339Nano), ":", "")
	}
	return "req_" + hex.EncodeToString(b[:])
}

func stringPtrOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": message,
	})
}
