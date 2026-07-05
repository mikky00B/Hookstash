package capture

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"time"
)

const ForwardTimeout = 10 * time.Second

type ForwardResult struct {
	Status     string
	StatusCode *int
	Error      *string
	DurationMS int64
	TargetURL  string
}

type ForwardRequest struct {
	Method      string
	TargetURL   string
	QueryString string
	Headers     http.Header
	Body        []byte
}

type Forwarder struct {
	client *http.Client
}

func NewForwarder(client *http.Client) Forwarder {
	if client == nil {
		client = &http.Client{Timeout: ForwardTimeout}
	}
	return Forwarder{client: client}
}

func (f Forwarder) Forward(ctx context.Context, req ForwardRequest) ForwardResult {
	start := time.Now()
	targetURL := appendQueryString(req.TargetURL, req.QueryString)

	ctx, cancel := context.WithTimeout(ctx, ForwardTimeout)
	defer cancel()

	forwardReq, err := http.NewRequestWithContext(ctx, req.Method, targetURL, bytes.NewReader(req.Body))
	if err != nil {
		return failedForwardResult(targetURL, start, err)
	}

	copyForwardHeaders(forwardReq.Header, req.Headers)
	resp, err := f.client.Do(forwardReq)
	if err != nil {
		return queuedForwardResult(targetURL, start, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	statusCode := resp.StatusCode
	return ForwardResult{
		Status:     "forwarded",
		StatusCode: &statusCode,
		DurationMS: time.Since(start).Milliseconds(),
		TargetURL:  targetURL,
	}
}

func appendQueryString(target string, rawQuery string) string {
	if rawQuery == "" {
		return target
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return target
	}
	existing := parsed.RawQuery
	if existing == "" {
		parsed.RawQuery = rawQuery
	} else {
		parsed.RawQuery = existing + "&" + rawQuery
	}
	return parsed.String()
}

func copyForwardHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		if isHopByHopHeader(key) {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func isHopByHopHeader(key string) bool {
	switch http.CanonicalHeaderKey(key) {
	case "Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Te",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
		"Host",
		"Content-Length":
		return true
	default:
		return false
	}
}

func queuedForwardResult(targetURL string, start time.Time, err error) ForwardResult {
	message := err.Error()
	return ForwardResult{
		Status:     "queued",
		Error:      &message,
		DurationMS: time.Since(start).Milliseconds(),
		TargetURL:  targetURL,
	}
}

func failedForwardResult(targetURL string, start time.Time, err error) ForwardResult {
	message := err.Error()
	return ForwardResult{
		Status:     "failed",
		Error:      &message,
		DurationMS: time.Since(start).Milliseconds(),
		TargetURL:  targetURL,
	}
}
