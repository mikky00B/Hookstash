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

func TestCaptureUnknownEndpointReturnsNotFound(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	handler := New(Config{Store: db})
	req := httptest.NewRequest(http.MethodPost, "/hooks/doesnotexist", strings.NewReader(`{"ok":true}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unknown endpoint") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestCaptureToCustomEndpointAndFilterList(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	handler := New(Config{Store: db})

	createReq := httptest.NewRequest(http.MethodPost, "/api/endpoints", strings.NewReader(`{"slug":"Payments"}`))
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create endpoint status = %d; body %s", createRec.Code, createRec.Body.String())
	}
	var createResponse struct {
		Endpoint store.Endpoint `json:"endpoint"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&createResponse); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if createResponse.Endpoint.Slug != "payments" {
		t.Fatalf("slug = %q, want normalized payments", createResponse.Endpoint.Slug)
	}

	captureReq := httptest.NewRequest(http.MethodPost, "/hooks/payments", strings.NewReader(`{"event":"charge.success"}`))
	captureRec := httptest.NewRecorder()
	handler.ServeHTTP(captureRec, captureReq)
	if captureRec.Code != http.StatusAccepted {
		t.Fatalf("capture status = %d; body %s", captureRec.Code, captureRec.Body.String())
	}
	var captureResponse struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(captureRec.Body).Decode(&captureResponse); err != nil {
		t.Fatalf("decode capture response: %v", err)
	}
	if captureResponse.Endpoint != "payments" {
		t.Fatalf("capture endpoint = %q, want payments", captureResponse.Endpoint)
	}

	// Capture on the default endpoint too, so filtering is observable.
	otherReq := httptest.NewRequest(http.MethodPost, "/hooks/default", strings.NewReader(`{"event":"other"}`))
	otherRec := httptest.NewRecorder()
	handler.ServeHTTP(otherRec, otherReq)
	if otherRec.Code != http.StatusAccepted {
		t.Fatalf("default capture status = %d", otherRec.Code)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/requests?endpoint=payments", nil)
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
	if listResponse.Requests[0].EndpointID != createResponse.Endpoint.ID {
		t.Fatalf("endpoint id = %q, want %q", listResponse.Requests[0].EndpointID, createResponse.Endpoint.ID)
	}

	filteredReq := httptest.NewRequest(http.MethodGet, "/api/requests?endpoint=missing", nil)
	filteredRec := httptest.NewRecorder()
	handler.ServeHTTP(filteredRec, filteredReq)
	if filteredRec.Code != http.StatusBadRequest {
		t.Fatalf("unknown endpoint filter status = %d, want 400", filteredRec.Code)
	}
}

func TestCaptureTokenAuth(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	handler := New(Config{Store: db})

	createReq := httptest.NewRequest(http.MethodPost, "/api/endpoints", strings.NewReader(`{"slug":"secure","with_token":true}`))
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create endpoint status = %d", createRec.Code)
	}
	var createResponse struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&createResponse); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if createResponse.Token == "" {
		t.Fatalf("expected a one-time token in the create response")
	}

	unauthorized := httptest.NewRequest(http.MethodPost, "/hooks/secure", strings.NewReader(`{"ok":true}`))
	unauthorizedRec := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedRec, unauthorized)
	if unauthorizedRec.Code != http.StatusUnauthorized {
		t.Fatalf("status without token = %d, want 401", unauthorizedRec.Code)
	}

	wrongToken := httptest.NewRequest(http.MethodPost, "/hooks/secure?token=hs_wrong", strings.NewReader(`{"ok":true}`))
	wrongRec := httptest.NewRecorder()
	handler.ServeHTTP(wrongRec, wrongToken)
	if wrongRec.Code != http.StatusUnauthorized {
		t.Fatalf("status with wrong token = %d, want 401", wrongRec.Code)
	}

	bearerReq := httptest.NewRequest(http.MethodPost, "/hooks/secure", strings.NewReader(`{"ok":true}`))
	bearerReq.Header.Set("Authorization", "Bearer "+createResponse.Token)
	bearerRec := httptest.NewRecorder()
	handler.ServeHTTP(bearerRec, bearerReq)
	if bearerRec.Code != http.StatusAccepted {
		t.Fatalf("status with bearer token = %d; body %s", bearerRec.Code, bearerRec.Body.String())
	}

	queryReq := httptest.NewRequest(http.MethodPost, "/hooks/secure?token="+createResponse.Token, strings.NewReader(`{"ok":true}`))
	queryRec := httptest.NewRecorder()
	handler.ServeHTTP(queryRec, queryReq)
	if queryRec.Code != http.StatusAccepted {
		t.Fatalf("status with query token = %d", queryRec.Code)
	}
}

func TestCreateEndpointValidation(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	handler := New(Config{Store: db})

	cases := []struct {
		name    string
		payload string
		status  int
	}{
		{"empty slug", `{"slug":""}`, http.StatusBadRequest},
		{"bad characters", `{"slug":"Pay ments!"}`, http.StatusBadRequest},
		{"duplicate", `{"slug":"default"}`, http.StatusConflict},
	}
	for _, testCase := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/endpoints", strings.NewReader(testCase.payload))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != testCase.status {
			t.Fatalf("%s: status = %d, want %d; body %s", testCase.name, rec.Code, testCase.status, rec.Body.String())
		}
	}
}

func TestDeleteEndpointRules(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	handler := New(Config{Store: db})

	defaultReq := httptest.NewRequest(http.MethodDelete, "/api/endpoints/ep_default", nil)
	defaultRec := httptest.NewRecorder()
	handler.ServeHTTP(defaultRec, defaultReq)
	if defaultRec.Code != http.StatusBadRequest {
		t.Fatalf("delete default status = %d, want 400", defaultRec.Code)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/endpoints", strings.NewReader(`{"slug":"temp"}`))
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	var createResponse struct {
		Endpoint store.Endpoint `json:"endpoint"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&createResponse); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/endpoints/"+createResponse.Endpoint.ID, nil)
	deleteRec := httptest.NewRecorder()
	handler.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", deleteRec.Code)
	}

	missingReq := httptest.NewRequest(http.MethodDelete, "/api/endpoints/"+createResponse.Endpoint.ID, nil)
	missingRec := httptest.NewRecorder()
	handler.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d, want 404", missingRec.Code)
	}
}

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

func TestReplayUsesEditedBody(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	var gotBody string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	handler := New(Config{Store: db})
	captureReq := httptest.NewRequest(http.MethodPost, "/hooks/default", strings.NewReader(`{"event":"original"}`))
	captureReq.Header.Set("Content-Type", "application/json")
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

	replayPayload := `{"target_url":"` + target.URL + `","body":"{\"event\":\"edited\"}"}`
	replayReq := httptest.NewRequest(http.MethodPost, "/api/requests/"+captureResponse.ID+"/replay", strings.NewReader(replayPayload))
	replayRec := httptest.NewRecorder()
	handler.ServeHTTP(replayRec, replayReq)
	if replayRec.Code != http.StatusOK {
		t.Fatalf("replay status = %d; body %s", replayRec.Code, replayRec.Body.String())
	}

	if gotBody != `{"event":"edited"}` {
		t.Fatalf("replayed body = %q, want edited body", gotBody)
	}
	attempts, err := db.ListReplayAttempts(replayReq.Context(), captureResponse.ID)
	if err != nil {
		t.Fatalf("list replay attempts: %v", err)
	}
	if len(attempts) != 1 || string(attempts[0].EditedBody) != `{"event":"edited"}` {
		t.Fatalf("stored edited body = %+v", attempts)
	}
}

func TestReplayAppliesEditedHeaders(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	var gotContentType string
	var gotOriginalHeader string
	var gotReplayHeader string
	var gotHost string
	var gotConnection string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		gotOriginalHeader = r.Header.Get("X-Original")
		gotReplayHeader = r.Header.Get("X-Replay")
		gotHost = r.Host
		gotConnection = r.Header.Get("Connection")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	handler := New(Config{Store: db})
	captureReq := httptest.NewRequest(http.MethodPost, "/hooks/default", strings.NewReader(`{"ok":true}`))
	captureReq.Header.Set("Content-Type", "application/json")
	captureReq.Header.Set("X-Original", "kept")
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

	replayPayload := `{
		"target_url":"` + target.URL + `",
		"headers":{
			"Content-Type":"application/vnd.hookstash+json",
			"X-Replay":["yes"],
			"Connection":"close",
			"Host":"evil.example"
		}
	}`
	replayReq := httptest.NewRequest(http.MethodPost, "/api/requests/"+captureResponse.ID+"/replay", strings.NewReader(replayPayload))
	replayRec := httptest.NewRecorder()
	handler.ServeHTTP(replayRec, replayReq)
	if replayRec.Code != http.StatusOK {
		t.Fatalf("replay status = %d; body %s", replayRec.Code, replayRec.Body.String())
	}

	if gotContentType != "application/vnd.hookstash+json" {
		t.Fatalf("content type = %q", gotContentType)
	}
	if gotOriginalHeader != "kept" {
		t.Fatalf("original header = %q", gotOriginalHeader)
	}
	if gotReplayHeader != "yes" {
		t.Fatalf("replay header = %q", gotReplayHeader)
	}
	if gotConnection != "" {
		t.Fatalf("connection header was replayed: %q", gotConnection)
	}
	if gotHost == "evil.example" {
		t.Fatalf("host override was replayed")
	}

	attempts, err := db.ListReplayAttempts(replayReq.Context(), captureResponse.ID)
	if err != nil {
		t.Fatalf("list replay attempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("attempt count = %d, want 1", len(attempts))
	}
	if attempts[0].EditedHeadersJSON == nil || !strings.Contains(*attempts[0].EditedHeadersJSON, "X-Replay") {
		t.Fatalf("edited headers were not stored: %+v", attempts[0].EditedHeadersJSON)
	}
}

func TestReplayRejectsInvalidEditedHeaders(t *testing.T) {
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

	replayReq := httptest.NewRequest(http.MethodPost, "/api/requests/"+captureResponse.ID+"/replay", strings.NewReader(`{
		"target_url":"http://127.0.0.1:8000/webhooks",
		"headers":{"X-Bad":123}
	}`))
	replayRec := httptest.NewRecorder()
	handler.ServeHTTP(replayRec, replayReq)

	if replayRec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", replayRec.Code, replayRec.Body.String())
	}
	if !strings.Contains(replayRec.Body.String(), "headers must be a JSON object") {
		t.Fatalf("body = %q", replayRec.Body.String())
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
