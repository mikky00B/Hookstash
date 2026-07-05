package store

const migrationSQL = `
CREATE TABLE IF NOT EXISTS requests (
  id TEXT PRIMARY KEY,
  method TEXT NOT NULL,
  path TEXT NOT NULL,
  query_string TEXT,
  headers_json TEXT NOT NULL,
  body BLOB,
  body_text TEXT,
  content_type TEXT,
  remote_addr TEXT,
  received_at DATETIME NOT NULL,
  provider_hint TEXT,
  forward_status TEXT,
  forward_status_code INTEGER,
  forward_error TEXT,
  forward_duration_ms INTEGER,
  target_url TEXT
);

CREATE TABLE IF NOT EXISTS replay_attempts (
  id TEXT PRIMARY KEY,
  request_id TEXT NOT NULL,
  target_url TEXT NOT NULL,
  edited_body BLOB,
  edited_headers_json TEXT,
  status_code INTEGER,
  response_body TEXT,
  error TEXT,
  duration_ms INTEGER,
  created_at DATETIME NOT NULL,
  FOREIGN KEY (request_id) REFERENCES requests(id)
);

CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_requests_received_at ON requests(received_at DESC);
CREATE INDEX IF NOT EXISTS idx_requests_forward_status ON requests(forward_status);
CREATE INDEX IF NOT EXISTS idx_replay_attempts_request_id ON replay_attempts(request_id);
`
