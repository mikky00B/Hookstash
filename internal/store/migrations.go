package store

// baseSQL creates the v1 schema. It is idempotent and runs on every start so
// databases created before versioned migrations (v1.0) keep working.
const baseSQL = `
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

// migrations lists the versioned migrations applied in order after baseSQL.
// migrations[0] is version 2; version 1 is the base schema.
var migrations = []string{
	// v2: named capture endpoints. Existing requests belong to the default endpoint.
	`
CREATE TABLE IF NOT EXISTS endpoints (
  id TEXT PRIMARY KEY,
  slug TEXT NOT NULL UNIQUE,
  token_hash TEXT NOT NULL DEFAULT '',
  provider TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL
);

ALTER TABLE requests ADD COLUMN endpoint_id TEXT NOT NULL DEFAULT 'default';

INSERT INTO endpoints (id, slug, token_hash, provider, created_at)
VALUES ('ep_default', 'default', '', '', CURRENT_TIMESTAMP);

CREATE INDEX IF NOT EXISTS idx_requests_endpoint_id ON requests(endpoint_id);
CREATE INDEX IF NOT EXISTS idx_endpoints_slug ON endpoints(slug);
`,
}

const schemaMigrationsSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at DATETIME NOT NULL
);
`
