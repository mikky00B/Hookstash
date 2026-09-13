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
	EndpointID        string    `json:"endpoint_id"`
}

type Endpoint struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	TokenHash string    `json:"-"`
	Provider  string    `json:"provider"`
	CreatedAt time.Time `json:"created_at"`
}

type ReplayAttempt struct {
	ID                string    `json:"id"`
	RequestID         string    `json:"request_id"`
	TargetURL         string    `json:"target_url"`
	EditedBody        []byte    `json:"-"`
	EditedHeadersJSON *string   `json:"edited_headers_json"`
	StatusCode        *int      `json:"status_code"`
	ResponseBody      *string   `json:"response_body"`
	Error             *string   `json:"error"`
	DurationMS        int64     `json:"duration_ms"`
	CreatedAt         time.Time `json:"created_at"`
}
