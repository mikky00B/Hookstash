package store

import (
	"context"
	"testing"
	"time"
)

func TestSQLiteStoreMigratesAndStoresRequests(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	req := CapturedRequest{
		ID:            "req_test",
		Method:        "POST",
		Path:          "/hooks/default",
		QueryString:   "source=test",
		HeadersJSON:   `{"Content-Type":["application/json"]}`,
		Body:          []byte(`{"event":"charge.success"}`),
		BodyText:      `{"event":"charge.success"}`,
		ContentType:   "application/json",
		RemoteAddr:    "127.0.0.1:5000",
		ReceivedAt:    time.Now().UTC(),
		ProviderHint:  "unknown",
		ForwardStatus: "not_configured",
	}

	if err := db.CreateRequest(context.Background(), req); err != nil {
		t.Fatalf("create request: %v", err)
	}

	got, err := db.GetRequest(context.Background(), req.ID)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	if got.Method != "POST" {
		t.Fatalf("method = %q, want POST", got.Method)
	}
	if string(got.Body) != string(req.Body) {
		t.Fatalf("body = %q, want %q", string(got.Body), string(req.Body))
	}
}
