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
	"github.com/hookstash/hookstash/internal/tunnel"
)

type Store interface {
	CreateRequest(ctx context.Context, req store.CapturedRequest) error
	ListRequests(ctx context.Context, endpointID string) ([]store.CapturedRequest, error)
	GetRequest(ctx context.Context, id string) (store.CapturedRequest, error)
	UpdateForwardResult(ctx context.Context, id string, status string, statusCode *int, forwardError *string, durationMS int64, targetURL string) error
	CreateReplayAttempt(ctx context.Context, attempt store.ReplayAttempt) error
	CreateEndpoint(ctx context.Context, endpoint store.Endpoint) error
	ListEndpoints(ctx context.Context) ([]store.Endpoint, error)
	GetEndpointBySlug(ctx context.Context, slug string) (store.Endpoint, error)
	DeleteEndpoint(ctx context.Context, id string) error
}

type Config struct {
	Store      Store
	ForwardURL string
	Tunnel     *tunnel.Manager
	Broker     *stream.Broker
}

type Server struct {
	store      Store
	forwardURL string
	forwarder  capture.Forwarder
	replayer   capture.Replayer
	broker     *stream.Broker
	tunnel     *tunnel.Manager
	mux        *http.ServeMux
}

func New(cfg Config) http.Handler {
	broker := cfg.Broker
	if broker == nil {
		broker = stream.NewBroker()
	}
	s := &Server{
		store:      cfg.Store,
		forwardURL: cfg.ForwardURL,
		forwarder:  capture.NewForwarder(nil),
		replayer:   capture.NewReplayer(nil),
		broker:     broker,
		tunnel:     cfg.Tunnel,
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
	s.mux.HandleFunc("POST /api/endpoints", s.handleCreateEndpoint)
	s.mux.HandleFunc("GET /api/endpoints", s.handleListEndpoints)
	s.mux.HandleFunc("DELETE /api/endpoints/{id}", s.handleDeleteEndpoint)
	s.mux.HandleFunc("GET /api/tunnel", s.handleTunnelStatus)
	s.mux.HandleFunc("POST /api/tunnel/start", s.handleTunnelStart)
	s.mux.HandleFunc("POST /api/tunnel/stop", s.handleTunnelStop)
	s.mux.HandleFunc("/hooks/{slug}", s.handleCapture)
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
	endpoint, ok := s.authorizeEndpoint(w, r)
	if !ok {
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
		EndpointID:    endpoint.ID,
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
		"endpoint":       endpoint.Slug,
		"forward_status": req.ForwardStatus,
		"forward_error":  req.ForwardError,
	})
}

// authorizeEndpoint resolves the {slug} path value to an endpoint and enforces
// its capture token, if one is configured.
func (s *Server) authorizeEndpoint(w http.ResponseWriter, r *http.Request) (store.Endpoint, bool) {
	slug := strings.ToLower(r.PathValue("slug"))
	endpoint, err := s.store.GetEndpointBySlug(r.Context(), slug)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "unknown endpoint: "+slug)
		return store.Endpoint{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load endpoint")
		return store.Endpoint{}, false
	}

	if endpoint.TokenHash != "" {
		token := r.URL.Query().Get("token")
		if auth := r.Header.Get("Authorization"); auth != "" {
			if bearer, found := strings.CutPrefix(auth, "Bearer "); found {
				token = strings.TrimSpace(bearer)
			}
		}
		if token == "" || store.HashToken(token) != endpoint.TokenHash {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hookstash"`)
			writeError(w, http.StatusUnauthorized, "invalid or missing capture token")
			return store.Endpoint{}, false
		}
	}

	return endpoint, true
}

func (s *Server) handleTunnelStatus(w http.ResponseWriter, r *http.Request) {
	if s.tunnel == nil {
		writeJSON(w, http.StatusOK, tunnel.Status{State: tunnel.StateDisabled})
		return
	}
	writeJSON(w, http.StatusOK, s.tunnel.Status())
}

func (s *Server) handleTunnelStart(w http.ResponseWriter, r *http.Request) {
	if s.tunnel == nil {
		writeError(w, http.StatusBadRequest, "tunnel support is not configured")
		return
	}
	status := s.tunnel.Start()
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleTunnelStop(w http.ResponseWriter, r *http.Request) {
	if s.tunnel == nil {
		writeError(w, http.StatusBadRequest, "tunnel support is not configured")
		return
	}
	writeJSON(w, http.StatusOK, s.tunnel.Stop())
}

func (s *Server) handleCreateEndpoint(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Slug      string `json:"slug"`
		Provider  string `json:"provider"`
		WithToken bool   `json:"with_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid endpoint payload")
		return
	}

	slug, err := store.NormalizeSlug(payload.Slug)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.store.GetEndpointBySlug(r.Context(), slug); err == nil {
		writeError(w, http.StatusConflict, "endpoint already exists: "+slug)
		return
	}

	endpoint := store.Endpoint{
		ID:        store.NewEndpointID(),
		Slug:      slug,
		Provider:  strings.TrimSpace(payload.Provider),
		CreatedAt: time.Now().UTC(),
	}

	var response map[string]any
	if payload.WithToken {
		token, tokenHash, err := store.GenerateToken()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not generate capture token")
			return
		}
		endpoint.TokenHash = tokenHash
		response = map[string]any{
			"endpoint": endpoint,
			"token":    token,
		}
	} else {
		response = map[string]any{
			"endpoint": endpoint,
		}
	}

	if err := s.store.CreateEndpoint(r.Context(), endpoint); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create endpoint")
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) handleListEndpoints(w http.ResponseWriter, r *http.Request) {
	endpoints, err := s.store.ListEndpoints(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list endpoints")
		return
	}
	if endpoints == nil {
		endpoints = []store.Endpoint{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"endpoints": endpoints,
	})
}

func (s *Server) handleDeleteEndpoint(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "ep_default" {
		writeError(w, http.StatusBadRequest, "the default endpoint cannot be deleted")
		return
	}
	if err := s.store.DeleteEndpoint(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "endpoint not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete endpoint")
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	endpointID := ""
	if slug := strings.TrimSpace(r.URL.Query().Get("endpoint")); slug != "" {
		endpoint, err := s.store.GetEndpointBySlug(r.Context(), strings.ToLower(slug))
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "unknown endpoint: "+slug)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not load endpoint")
			return
		}
		endpointID = endpoint.ID
	}

	requests, err := s.store.ListRequests(r.Context(), endpointID)
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
		"endpoint_id":         req.EndpointID,
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
