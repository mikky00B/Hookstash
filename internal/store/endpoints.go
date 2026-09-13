package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

func (s *SQLiteStore) CreateEndpoint(ctx context.Context, endpoint Endpoint) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO endpoints (id, slug, token_hash, provider, created_at)
VALUES (?, ?, ?, ?, ?)`,
		endpoint.ID,
		endpoint.Slug,
		endpoint.TokenHash,
		endpoint.Provider,
		endpoint.CreatedAt.UTC(),
	)
	return err
}

func (s *SQLiteStore) ListEndpoints(ctx context.Context) ([]Endpoint, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, slug, token_hash, provider, created_at
FROM endpoints
ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var endpoints []Endpoint
	for rows.Next() {
		var endpoint Endpoint
		var createdAt time.Time
		if err := rows.Scan(&endpoint.ID, &endpoint.Slug, &endpoint.TokenHash, &endpoint.Provider, &createdAt); err != nil {
			return nil, err
		}
		endpoint.CreatedAt = createdAt.UTC()
		endpoints = append(endpoints, endpoint)
	}
	return endpoints, rows.Err()
}

func (s *SQLiteStore) GetEndpointBySlug(ctx context.Context, slug string) (Endpoint, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, slug, token_hash, provider, created_at
FROM endpoints
WHERE slug = ?`, strings.ToLower(slug))

	var endpoint Endpoint
	var createdAt time.Time
	err := row.Scan(&endpoint.ID, &endpoint.Slug, &endpoint.TokenHash, &endpoint.Provider, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Endpoint{}, ErrNotFound
		}
		return Endpoint{}, err
	}
	endpoint.CreatedAt = createdAt.UTC()
	return endpoint, nil
}

func (s *SQLiteStore) GetEndpoint(ctx context.Context, id string) (Endpoint, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, slug, token_hash, provider, created_at
FROM endpoints
WHERE id = ?`, id)

	var endpoint Endpoint
	var createdAt time.Time
	err := row.Scan(&endpoint.ID, &endpoint.Slug, &endpoint.TokenHash, &endpoint.Provider, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Endpoint{}, ErrNotFound
		}
		return Endpoint{}, err
	}
	endpoint.CreatedAt = createdAt.UTC()
	return endpoint, nil
}

func (s *SQLiteStore) DeleteEndpoint(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM endpoints WHERE id = ?`, id)
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

// GenerateToken returns a fresh capture token and its hash. Only the hash is
// stored; the plaintext is shown once at creation.
func GenerateToken() (string, string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", "", err
	}
	token := "hs_" + hex.EncodeToString(b[:])
	return token, HashToken(token), nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NormalizeSlug validates and normalizes an endpoint name.
func NormalizeSlug(slug string) (string, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return "", errors.New("slug is required")
	}
	if len(slug) > 40 {
		return "", errors.New("slug must be 40 characters or fewer")
	}
	for _, char := range slug {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			continue
		}
		return "", errors.New("slug may only contain lowercase letters, digits, dashes, and underscores")
	}
	return slug, nil
}

// NewEndpointID generates a random endpoint id.
func NewEndpointID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "ep_" + strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339Nano), ":", "")
	}
	return "ep_" + hex.EncodeToString(b[:])
}
