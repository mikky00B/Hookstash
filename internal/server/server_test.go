package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
