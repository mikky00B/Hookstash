package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func Open(path string) (*SQLiteStore, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	store := &SQLiteStore{db: db}
	if err := store.Migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) Migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, migrationSQL)
	return err
}

func (s *SQLiteStore) CreateRequest(ctx context.Context, req CapturedRequest) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO requests (
	id, method, path, query_string, headers_json, body, body_text, content_type,
	remote_addr, received_at, provider_hint, forward_status, forward_status_code,
	forward_error, forward_duration_ms, target_url
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.ID,
		req.Method,
		req.Path,
		req.QueryString,
		req.HeadersJSON,
		req.Body,
		req.BodyText,
		req.ContentType,
		req.RemoteAddr,
		req.ReceivedAt.UTC(),
		req.ProviderHint,
		req.ForwardStatus,
		req.ForwardStatusCode,
		req.ForwardError,
		req.ForwardDurationMS,
		req.TargetURL,
	)
	return err
}

func (s *SQLiteStore) ListRequests(ctx context.Context) ([]CapturedRequest, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, method, path, query_string, headers_json, body, body_text, content_type,
	remote_addr, received_at, provider_hint, forward_status, forward_status_code,
	forward_error, forward_duration_ms, target_url
FROM requests
ORDER BY received_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var requests []CapturedRequest
	for rows.Next() {
		req, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		requests = append(requests, req)
	}
	return requests, rows.Err()
}

func (s *SQLiteStore) GetRequest(ctx context.Context, id string) (CapturedRequest, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, method, path, query_string, headers_json, body, body_text, content_type,
	remote_addr, received_at, provider_hint, forward_status, forward_status_code,
	forward_error, forward_duration_ms, target_url
FROM requests
WHERE id = ?`, id)

	req, err := scanRequest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return CapturedRequest{}, ErrNotFound
	}
	return req, err
}

func (s *SQLiteStore) UpdateForwardResult(ctx context.Context, id string, status string, statusCode *int, forwardError *string, durationMS int64, targetURL string) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE requests
SET forward_status = ?,
	forward_status_code = ?,
	forward_error = ?,
	forward_duration_ms = ?,
	target_url = ?
WHERE id = ?`,
		status,
		statusCode,
		forwardError,
		durationMS,
		targetURL,
		id,
	)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

type requestScanner interface {
	Scan(dest ...any) error
}

func scanRequest(scanner requestScanner) (CapturedRequest, error) {
	var req CapturedRequest
	var receivedAt time.Time
	err := scanner.Scan(
		&req.ID,
		&req.Method,
		&req.Path,
		&req.QueryString,
		&req.HeadersJSON,
		&req.Body,
		&req.BodyText,
		&req.ContentType,
		&req.RemoteAddr,
		&receivedAt,
		&req.ProviderHint,
		&req.ForwardStatus,
		&req.ForwardStatusCode,
		&req.ForwardError,
		&req.ForwardDurationMS,
		&req.TargetURL,
	)
	req.ReceivedAt = receivedAt.UTC()
	return req, err
}

var ErrNotFound = errors.New("not found")
