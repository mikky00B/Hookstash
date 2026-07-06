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
	"github.com/hookstash/hookstash/internal/stream"
)

type Store interface {
	CreateRequest(ctx context.Context, req store.CapturedRequest) error
	ListRequests(ctx context.Context) ([]store.CapturedRequest, error)
	GetRequest(ctx context.Context, id string) (store.CapturedRequest, error)
	UpdateForwardResult(ctx context.Context, id string, status string, statusCode *int, forwardError *string, durationMS int64, targetURL string) error
	CreateReplayAttempt(ctx context.Context, attempt store.ReplayAttempt) error
}

type Config struct {
	Store      Store
	ForwardURL string
}

type Server struct {
	store      Store
	forwardURL string
	forwarder  capture.Forwarder
	replayer   capture.Replayer
	broker     *stream.Broker
	mux        *http.ServeMux
}

func New(cfg Config) http.Handler {
	s := &Server{
		store:      cfg.Store,
		forwardURL: cfg.ForwardURL,
		forwarder:  capture.NewForwarder(nil),
		replayer:   capture.NewReplayer(nil),
		broker:     stream.NewBroker(),
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
	s.mux.HandleFunc("GET /api/events", s.handleEvents)
	s.mux.HandleFunc("GET /api/requests", s.handleListRequests)
	s.mux.HandleFunc("GET /api/requests/{id}", s.handleGetRequest)
	s.mux.HandleFunc("POST /api/requests/{id}/replay", s.handleReplayRequest)
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

	s.publishRequestCreated(req)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"id":             req.ID,
		"captured":       true,
		"forward_status": req.ForwardStatus,
		"forward_error":  req.ForwardError,
	})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}

	events, unsubscribe := s.broker.Subscribe()
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	_, _ = io.WriteString(w, ": connected\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = io.WriteString(w, ": heartbeat\n\n")
			flusher.Flush()
		case event, ok := <-events:
			if !ok {
				return
			}
			writeSSE(w, event)
			flusher.Flush()
		}
	}
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

func (s *Server) handleReplayRequest(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		TargetURL string           `json:"target_url"`
		Body      *string          `json:"body"`
		Headers   *json.RawMessage `json:"headers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid replay payload")
		return
	}
	payload.TargetURL = strings.TrimSpace(payload.TargetURL)
	if payload.TargetURL == "" {
		writeError(w, http.StatusBadRequest, "target_url is required")
		return
	}

	req, err := s.store.GetRequest(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load request")
		return
	}

	headers, err := headersFromJSON(req.HeadersJSON)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load request headers")
		return
	}
	var editedHeadersJSON *string
	if payload.Headers != nil {
		overrides, normalized, err := replayHeaderOverrides(*payload.Headers)
		if err != nil {
			writeError(w, http.StatusBadRequest, "headers must be a JSON object with string or string array values")
			return
		}
		for key, values := range overrides {
			headers.Del(key)
			for _, value := range values {
				headers.Add(key, value)
			}
		}
		editedHeadersJSON = &normalized
	}

	body := req.Body
	if body == nil {
		body = []byte(req.BodyText)
	}
	var editedBody []byte
	if payload.Body != nil {
		editedBody = []byte(*payload.Body)
		body = editedBody
	}

	result := s.replayer.Replay(r.Context(), capture.ReplayRequest{
		Method:    req.Method,
		TargetURL: payload.TargetURL,
		Headers:   headers,
		Body:      body,
	})

	attempt := store.ReplayAttempt{
		ID:                newReplayAttemptID(),
		RequestID:         req.ID,
		TargetURL:         payload.TargetURL,
		EditedBody:        editedBody,
		EditedHeadersJSON: editedHeadersJSON,
		StatusCode:        result.StatusCode,
		ResponseBody:      result.ResponseBody,
		Error:             result.Error,
		DurationMS:        result.DurationMS,
		CreatedAt:         time.Now().UTC(),
	}
	if err := s.store.CreateReplayAttempt(r.Context(), attempt); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save replay attempt")
		return
	}

	writeJSON(w, http.StatusOK, attempt)
}

func (s *Server) publishRequestCreated(req store.CapturedRequest) {
	payload := map[string]any{
		"id":                  req.ID,
		"method":              req.Method,
		"path":                req.Path,
		"query_string":        req.QueryString,
		"provider_hint":       req.ProviderHint,
		"forward_status":      req.ForwardStatus,
		"forward_status_code": req.ForwardStatusCode,
		"received_at":         req.ReceivedAt,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	s.broker.Publish(stream.Event{
		Type: "request.created",
		Data: data,
	})
}

func writeSSE(w io.Writer, event stream.Event) {
	_, _ = io.WriteString(w, "event: "+event.Type+"\n")
	for _, line := range strings.Split(string(event.Data), "\n") {
		_, _ = io.WriteString(w, "data: "+line+"\n")
	}
	_, _ = io.WriteString(w, "\n")
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

func headersFromJSON(headersJSON string) (http.Header, error) {
	var values map[string][]string
	if err := json.Unmarshal([]byte(headersJSON), &values); err != nil {
		return nil, err
	}
	headers := make(http.Header, len(values))
	for key, headerValues := range values {
		for _, value := range headerValues {
			headers.Add(key, value)
		}
	}
	return headers, nil
}

func replayHeaderOverrides(raw json.RawMessage) (http.Header, string, error) {
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, "", err
	}
	headers := make(http.Header, len(values))
	normalized := make(map[string][]string, len(values))
	for key, value := range values {
		switch typed := value.(type) {
		case string:
			headers.Set(key, typed)
			normalized[key] = []string{typed}
		case []any:
			headerValues := make([]string, 0, len(typed))
			for _, item := range typed {
				text, ok := item.(string)
				if !ok {
					return nil, "", errors.New("header array values must be strings")
				}
				headerValues = append(headerValues, text)
				headers.Add(key, text)
			}
			normalized[key] = headerValues
		default:
			return nil, "", errors.New("header values must be strings or string arrays")
		}
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, "", err
	}
	return headers, string(encoded), nil
}

func newRequestID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "req_" + strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339Nano), ":", "")
	}
	return "req_" + hex.EncodeToString(b[:])
}

func newReplayAttemptID() string {
	return strings.Replace(newRequestID(), "req_", "rep_", 1)
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
