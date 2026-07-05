package store

import "time"

type CapturedRequest struct {
	ID                string    `json:"id"`
	Method            string    `json:"method"`
	Path              string    `json:"path"`
	QueryString       string    `json:"query_string"`
	HeadersJSON       string    `json:"headers_json"`
	Body              []byte    `json:"-"`
	BodyText          string    `json:"body_text"`
	ContentType       string    `json:"content_type"`
	RemoteAddr        string    `json:"remote_addr"`
	ReceivedAt        time.Time `json:"received_at"`
	ProviderHint      string    `json:"provider_hint"`
	ForwardStatus     string    `json:"forward_status"`
	ForwardStatusCode *int      `json:"forward_status_code"`
	ForwardError      *string   `json:"forward_error"`
	ForwardDurationMS *int64    `json:"forward_duration_ms"`
	TargetURL         *string   `json:"target_url"`
}
