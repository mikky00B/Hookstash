package server

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hookstash/hookstash/internal/store"
)

func TestCaptureStoresRequestBeforeReturning(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	handler := New(Config{Store: db})

	body := `{"event":"payment.success","amount":5000}`
	req := httptest.NewRequest(http.MethodPost, "/hooks/default?source=test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Paystack-Signature", "test-signature")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	var response struct {
		ID            string `json:"id"`
		Captured      bool   `json:"captured"`
		ForwardStatus string `json:"forward_status"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.Captured || response.ID == "" {
		t.Fatalf("unexpected response: %+v", response)
	}

	stored, err := db.GetRequest(req.Context(), response.ID)
	if err != nil {
		t.Fatalf("get stored request: %v", err)
	}
	if stored.Method != http.MethodPost {
		t.Fatalf("method = %q, want POST", stored.Method)
	}
	if stored.QueryString != "source=test" {
		t.Fatalf("query = %q, want source=test", stored.QueryString)
	}
	if stored.BodyText != body {
		t.Fatalf("body = %q, want %q", stored.BodyText, body)
	}
	if stored.ProviderHint != "paystack" {
		t.Fatalf("provider = %q, want paystack", stored.ProviderHint)
	}
}

func TestListAndGetRequests(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	handler := New(Config{Store: db})

	captureReq := httptest.NewRequest(http.MethodPost, "/hooks/default", strings.NewReader(`{"ok":true}`))
	captureRec := httptest.NewRecorder()
	handler.ServeHTTP(captureRec, captureReq)
	if captureRec.Code != http.StatusAccepted {
		t.Fatalf("capture status = %d", captureRec.Code)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/requests", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d", listRec.Code)
	}

	var listResponse struct {
		Requests []store.CapturedRequest `json:"requests"`
	}
	if err := json.NewDecoder(listRec.Body).Decode(&listResponse); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResponse.Requests) != 1 {
		t.Fatalf("request count = %d, want 1", len(listResponse.Requests))
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/requests/"+listResponse.Requests[0].ID, nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d", getRec.Code)
	}
}

func TestDashboardFallbackServesInstructions(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	handler := New(Config{Store: db})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "Build the dashboard") {
		t.Fatalf("fallback body = %q", rec.Body.String())
	}
}

func TestEventsStreamReceivesRequestCreated(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	testServer := httptest.NewServer(New(Config{Store: db}))
	defer testServer.Close()

	eventsResp, err := testServer.Client().Get(testServer.URL + "/api/events")
	if err != nil {
		t.Fatalf("connect events: %v", err)
	}
	defer eventsResp.Body.Close()

	if eventsResp.StatusCode != http.StatusOK {
		t.Fatalf("events status = %d", eventsResp.StatusCode)
	}
	if contentType := eventsResp.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("content type = %q, want text/event-stream", contentType)
	}

	captureResp, err := testServer.Client().Post(testServer.URL+"/hooks/default", "application/json", strings.NewReader(`{"ok":true}`))
	if err != nil {
		t.Fatalf("capture request: %v", err)
	}
	defer captureResp.Body.Close()

	var captureResponse struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(captureResp.Body).Decode(&captureResponse); err != nil {
		t.Fatalf("decode capture response: %v", err)
	}

	lines := make(chan string, 16)
	go func() {
		scanner := bufio.NewScanner(eventsResp.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()

	deadline := time.After(2 * time.Second)
	var sawEvent bool
	var sawID bool
	for !sawEvent || !sawID {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("event stream closed before request.created")
			}
			if line == "event: request.created" {
				sawEvent = true
			}
			if strings.HasPrefix(line, "data: ") && strings.Contains(line, captureResponse.ID) {
				sawID = true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for request.created event; sawEvent=%v sawID=%v", sawEvent, sawID)
		}
	}
}

func TestCaptureForwardsAfterSavingRequest(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	var gotBody string
	var gotContentType string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusCreated)
	}))
	defer target.Close()

	handler := New(Config{Store: db, ForwardURL: target.URL + "/webhooks"})
	captureReq := httptest.NewRequest(http.MethodPost, "/hooks/default", strings.NewReader(`{"event":"charge.success"}`))
	captureReq.Header.Set("Content-Type", "application/json")
	captureRec := httptest.NewRecorder()
	handler.ServeHTTP(captureRec, captureReq)

	if captureRec.Code != http.StatusAccepted {
		t.Fatalf("capture status = %d; body %s", captureRec.Code, captureRec.Body.String())
	}

	var response struct {
		ID            string `json:"id"`
		ForwardStatus string `json:"forward_status"`
	}
	if err := json.NewDecoder(captureRec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ForwardStatus != "forwarded" {
		t.Fatalf("forward status = %q, want forwarded", response.ForwardStatus)
	}
	if gotBody != `{"event":"charge.success"}` {
		t.Fatalf("forwarded body = %q", gotBody)
	}
	if gotContentType != "application/json" {
		t.Fatalf("forwarded content type = %q", gotContentType)
	}

	stored, err := db.GetRequest(captureReq.Context(), response.ID)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	if stored.ForwardStatus != "forwarded" {
		t.Fatalf("stored forward status = %q", stored.ForwardStatus)
	}
	if stored.ForwardStatusCode == nil || *stored.ForwardStatusCode != http.StatusCreated {
		t.Fatalf("stored status code = %v, want 201", stored.ForwardStatusCode)
	}
}

func TestCaptureQueuesFailedForward(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	handler := New(Config{Store: db, ForwardURL: "http://127.0.0.1:1/webhooks"})
	captureReq := httptest.NewRequest(http.MethodPost, "/hooks/default", strings.NewReader(`{"event":"charge.failed"}`))
	captureRec := httptest.NewRecorder()
	handler.ServeHTTP(captureRec, captureReq)

	if captureRec.Code != http.StatusAccepted {
		t.Fatalf("capture status = %d; body %s", captureRec.Code, captureRec.Body.String())
	}

	var response struct {
		ID            string  `json:"id"`
		ForwardStatus string  `json:"forward_status"`
		ForwardError  *string `json:"forward_error"`
	}
	if err := json.NewDecoder(captureRec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ForwardStatus != "queued" {
		t.Fatalf("forward status = %q, want queued", response.ForwardStatus)
	}
	if response.ForwardError == nil || *response.ForwardError == "" {
		t.Fatalf("forward error was not returned")
	}

	stored, err := db.GetRequest(captureReq.Context(), response.ID)
	if err != nil {
		t.Fatalf("get stored request: %v", err)
	}
	if stored.BodyText != `{"event":"charge.failed"}` {
		t.Fatalf("stored body = %q", stored.BodyText)
	}
	if stored.ForwardStatus != "queued" {
		t.Fatalf("stored forward status = %q, want queued", stored.ForwardStatus)
	}
}

func TestReplayCapturedRequestSuccess(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	var gotMethod string
	var gotBody string
	var gotContentType string
	var gotCustomHeader string
	var gotConnection string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotMethod = r.Method
		gotBody = string(body)
		gotContentType = r.Header.Get("Content-Type")
		gotCustomHeader = r.Header.Get("X-Hookstash-Test")
		gotConnection = r.Header.Get("Connection")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"replayed":true}`))
	}))
	defer target.Close()

	handler := New(Config{Store: db})
	captureReq := httptest.NewRequest(http.MethodPut, "/hooks/default", strings.NewReader(`{"event":"replay.success"}`))
	captureReq.Header.Set("Content-Type", "application/json")
	captureReq.Header.Set("X-Hookstash-Test", "present")
	captureReq.Header.Set("Connection", "keep-alive")
	captureRec := httptest.NewRecorder()
	handler.ServeHTTP(captureRec, captureReq)
	if captureRec.Code != http.StatusAccepted {
		t.Fatalf("capture status = %d; body %s", captureRec.Code, captureRec.Body.String())
	}

	var captureResponse struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(captureRec.Body).Decode(&captureResponse); err != nil {
		t.Fatalf("decode capture response: %v", err)
	}

	replayPayload := `{"target_url":"` + target.URL + `/webhooks"}`
	replayReq := httptest.NewRequest(http.MethodPost, "/api/requests/"+captureResponse.ID+"/replay", strings.NewReader(replayPayload))
	replayRec := httptest.NewRecorder()
	handler.ServeHTTP(replayRec, replayReq)
	if replayRec.Code != http.StatusOK {
		t.Fatalf("replay status = %d; body %s", replayRec.Code, replayRec.Body.String())
	}

	var attempt store.ReplayAttempt
	if err := json.NewDecoder(replayRec.Body).Decode(&attempt); err != nil {
		t.Fatalf("decode replay attempt: %v", err)
	}
	if attempt.RequestID != captureResponse.ID {
		t.Fatalf("request id = %q, want %q", attempt.RequestID, captureResponse.ID)
	}
	if attempt.StatusCode == nil || *attempt.StatusCode != http.StatusAccepted {
		t.Fatalf("status code = %v, want 202", attempt.StatusCode)
	}
	if attempt.ResponseBody == nil || *attempt.ResponseBody != `{"replayed":true}` {
		t.Fatalf("response body = %v", attempt.ResponseBody)
	}
	if attempt.Error != nil {
		t.Fatalf("unexpected replay error: %v", *attempt.Error)
	}

	if gotMethod != http.MethodPut {
		t.Fatalf("method = %q, want PUT", gotMethod)
	}
	if gotBody != `{"event":"replay.success"}` {
		t.Fatalf("body = %q", gotBody)
	}
	if gotContentType != "application/json" {
		t.Fatalf("content type = %q", gotContentType)
	}
	if gotCustomHeader != "present" {
		t.Fatalf("custom header = %q", gotCustomHeader)
	}
	if gotConnection != "" {
		t.Fatalf("connection header was replayed: %q", gotConnection)
	}

	attempts, err := db.ListReplayAttempts(replayReq.Context(), captureResponse.ID)
	if err != nil {
		t.Fatalf("list replay attempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("attempt count = %d, want 1", len(attempts))
	}
}

func TestReplayRequiresTargetURL(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	handler := New(Config{Store: db})
	captureReq := httptest.NewRequest(http.MethodPost, "/hooks/default", strings.NewReader(`{"ok":true}`))
	captureRec := httptest.NewRecorder()
	handler.ServeHTTP(captureRec, captureReq)
	if captureRec.Code != http.StatusAccepted {
		t.Fatalf("capture status = %d", captureRec.Code)
	}

	var captureResponse struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(captureRec.Body).Decode(&captureResponse); err != nil {
		t.Fatalf("decode capture response: %v", err)
	}

	replayReq := httptest.NewRequest(http.MethodPost, "/api/requests/"+captureResponse.ID+"/replay", strings.NewReader(`{}`))
	replayRec := httptest.NewRecorder()
	handler.ServeHTTP(replayRec, replayReq)

	if replayRec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", replayRec.Code, replayRec.Body.String())
	}
	if !strings.Contains(replayRec.Body.String(), "target_url is required") {
		t.Fatalf("body = %q", replayRec.Body.String())
	}
}

func TestReplayCapturedRequestNotFound(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	handler := New(Config{Store: db})
	replayReq := httptest.NewRequest(http.MethodPost, "/api/requests/req_missing/replay", strings.NewReader(`{"target_url":"http://127.0.0.1:8000/webhooks"}`))
	replayRec := httptest.NewRecorder()
	handler.ServeHTTP(replayRec, replayReq)

	if replayRec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body %s", replayRec.Code, replayRec.Body.String())
	}
}

func TestReplayFailureIsStored(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	handler := New(Config{Store: db})
	captureReq := httptest.NewRequest(http.MethodPost, "/hooks/default", strings.NewReader(`{"event":"replay.failure"}`))
	captureRec := httptest.NewRecorder()
	handler.ServeHTTP(captureRec, captureReq)
	if captureRec.Code != http.StatusAccepted {
		t.Fatalf("capture status = %d", captureRec.Code)
	}

	var captureResponse struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(captureRec.Body).Decode(&captureResponse); err != nil {
		t.Fatalf("decode capture response: %v", err)
	}

	replayReq := httptest.NewRequest(http.MethodPost, "/api/requests/"+captureResponse.ID+"/replay", strings.NewReader(`{"target_url":"http://127.0.0.1:1/webhooks"}`))
	replayRec := httptest.NewRecorder()
	handler.ServeHTTP(replayRec, replayReq)
	if replayRec.Code != http.StatusOK {
		t.Fatalf("replay status = %d; body %s", replayRec.Code, replayRec.Body.String())
	}

	var attempt store.ReplayAttempt
	if err := json.NewDecoder(replayRec.Body).Decode(&attempt); err != nil {
		t.Fatalf("decode replay attempt: %v", err)
	}
	if attempt.StatusCode != nil {
		t.Fatalf("status code = %v, want nil", attempt.StatusCode)
	}
	if attempt.Error == nil || *attempt.Error == "" {
		t.Fatalf("expected replay error, got %v", attempt.Error)
	}

	attempts, err := db.ListReplayAttempts(replayReq.Context(), captureResponse.ID)
	if err != nil {
		t.Fatalf("list replay attempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("attempt count = %d, want 1", len(attempts))
	}
	if attempts[0].Error == nil || *attempts[0].Error == "" {
		t.Fatalf("stored replay error was empty")
	}
}
