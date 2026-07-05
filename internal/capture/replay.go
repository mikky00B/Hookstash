package capture

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"
)

const responsePreviewLimit = 64 * 1024

type ReplayRequest struct {
	Method    string
	TargetURL string
	Headers   http.Header
	Body      []byte
}

type ReplayResult struct {
	StatusCode   *int
	ResponseBody *string
	Error        *string
	DurationMS   int64
}

type Replayer struct {
	client *http.Client
}

func NewReplayer(client *http.Client) Replayer {
	if client == nil {
		client = &http.Client{Timeout: ForwardTimeout}
	}
	return Replayer{client: client}
}

func (r Replayer) Replay(ctx context.Context, req ReplayRequest) ReplayResult {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, ForwardTimeout)
	defer cancel()

	replayReq, err := http.NewRequestWithContext(ctx, req.Method, req.TargetURL, bytes.NewReader(req.Body))
	if err != nil {
		return failedReplayResult(start, err)
	}

	copyForwardHeaders(replayReq.Header, req.Headers)
	resp, err := r.client.Do(replayReq)
	if err != nil {
		return failedReplayResult(start, err)
	}
	defer resp.Body.Close()

	previewBytes, _ := io.ReadAll(io.LimitReader(resp.Body, responsePreviewLimit))
	preview := string(previewBytes)
	statusCode := resp.StatusCode
	return ReplayResult{
		StatusCode:   &statusCode,
		ResponseBody: &preview,
		DurationMS:   time.Since(start).Milliseconds(),
	}
}

func failedReplayResult(start time.Time, err error) ReplayResult {
	message := err.Error()
	return ReplayResult{
		Error:      &message,
		DurationMS: time.Since(start).Milliseconds(),
	}
}
