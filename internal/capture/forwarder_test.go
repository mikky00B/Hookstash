package capture

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestForwarderPreservesMethodBodyHeadersAndQuery(t *testing.T) {
	var gotMethod string
	var gotBody string
	var gotContentType string
	var gotSignature string
	var gotQuery string
	var gotConnection string

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotSignature = r.Header.Get("X-Paystack-Signature")
		gotConnection = r.Header.Get("Connection")
		gotQuery = r.URL.RawQuery
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set("X-Paystack-Signature", "sig_test")
	headers.Set("Connection", "close")

	result := NewForwarder(target.Client()).Forward(context.Background(), ForwardRequest{
		Method:      http.MethodPost,
		TargetURL:   target.URL + "/webhooks?existing=1",
		QueryString: "source=test",
		Headers:     headers,
		Body:        []byte(`{"event":"charge.success"}`),
	})

	if result.Status != "forwarded" {
		t.Fatalf("status = %q, want forwarded; error=%v", result.Status, result.Error)
	}
	if result.StatusCode == nil || *result.StatusCode != http.StatusNoContent {
		t.Fatalf("status code = %v, want 204", result.StatusCode)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if gotBody != `{"event":"charge.success"}` {
		t.Fatalf("body = %q", gotBody)
	}
	if gotContentType != "application/json" {
		t.Fatalf("content type = %q", gotContentType)
	}
	if gotSignature != "sig_test" {
		t.Fatalf("signature = %q", gotSignature)
	}
	if gotConnection != "" {
		t.Fatalf("connection header = %q, want empty", gotConnection)
	}
	if gotQuery != "existing=1&source=test" {
		t.Fatalf("query = %q", gotQuery)
	}
}
