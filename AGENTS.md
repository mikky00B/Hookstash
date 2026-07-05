# AGENTS.md

## Project: Hookstash

Hookstash is a zero-registration, local-first webhook inspector and replay tool for backend developers.

The goal is to provide a single Go binary that lets a developer capture incoming webhooks, inspect headers and payloads in a local dashboard, forward requests to a local backend, edit/replay captured requests, and queue failed forwards when the local backend is unavailable.

This file defines how AI coding agents should work on this repository.

---

## Product Positioning

Hookstash is not trying to be a hosted webhook platform in v1.

It is a local developer tool.

Core promise:

> Run one command, open a local dashboard, capture webhooks, inspect them, replay them, and never lose a captured webhook just because your local backend is down.

The tool should feel like:

- Postman for incoming webhooks
- A local-first alternative to cloud webhook inspectors
- A developer-friendly companion to tunnels like ngrok or Cloudflare Tunnel

Important distinction:

Hookstash does not provide a public tunnel in v1. For external providers like Stripe, GitHub, Paystack, Flutterwave, Shopify, or Telegram to reach a local machine, the user must use a tunnel such as ngrok or Cloudflare Tunnel that points to Hookstash.

Recommended flow:

```txt
Provider webhook
        ↓
Public tunnel URL
        ↓
Hookstash localhost:4040
        ↓
User app localhost:8000/webhooks
```

---

## Core v1 Goals

The v1 product should do these things well:

1. Start as a single Go binary.
2. Serve a local dashboard at `http://localhost:4040`.
3. Capture incoming HTTP webhook requests.
4. Store captured requests in SQLite.
5. Forward captured requests to a configured local target.
6. Display requests, headers, body, and forwarding status in the dashboard.
7. Replay captured requests.
8. Allow editing JSON/body before replay.
9. Queue failed forwards for manual or automatic retry.
10. Export captured requests as cURL commands.

Do not expand v1 into a SaaS product.

Avoid these in v1:

- User accounts
- Cloud sync
- Hosted webhook URLs
- Billing
- Teams/workspaces
- Complex RBAC
- Full signature verification for every provider
- Full API gateway behavior
- Heavy plugin system
- Kubernetes support

---

## Target User

The target user is a backend developer testing webhook integrations locally.

Examples:

- A developer testing Stripe payment webhooks
- A Nigerian/African backend developer testing Paystack or Flutterwave webhooks
- A developer testing GitHub repository webhooks
- A developer testing Shopify app webhooks
- A developer testing Telegram, Discord, Clerk, or Supabase webhooks

The product should make webhook debugging easier, faster, and more visual.

---

## Product Principles

### 1. Local-first

Everything should work locally by default.

Use local SQLite storage.
Do not require registration.
Do not require an API key.
Do not require a cloud dashboard.

### 2. Capture before forward

Always save the incoming request before attempting to forward it.

This is critical.

If the user’s local backend is down, Hookstash must still preserve the webhook.

Correct order:

```txt
Incoming request
  → read raw body
  → save request to SQLite
  → notify UI
  → attempt forward
  → save forward result
```

Never forward first and save later.

### 3. Preserve raw request data

Webhook signatures often depend on the exact raw payload.

Store:

- HTTP method
- Path
- Query string
- Headers
- Raw body bytes
- Content-Type
- Remote address
- Received timestamp

Do not only store parsed JSON.

### 4. Simple commands

The default developer experience should be simple:

```bash
hookstash
```

With forwarding:

```bash
hookstash --forward http://localhost:8000/webhooks
```

With custom port:

```bash
hookstash --port 4040 --forward http://localhost:8000/webhooks
```

### 5. Useful UI over terminal noise

The CLI should print useful startup information, but the main experience should be the dashboard.

The dashboard should make it easy to:

- See incoming requests
- Inspect headers
- Inspect body
- See forward status
- Replay request
- Edit payload
- Copy as cURL
- Retry failed forwards

### 6. Small, focused, shippable

Prefer a working small feature over a half-built large feature.

The project should be built milestone by milestone.

---

## Recommended Tech Stack

### Backend / CLI

Use Go.

Recommended packages:

- `net/http` for HTTP server behavior
- `github.com/go-chi/chi/v5` for routing
- `github.com/spf13/cobra` for CLI commands
- SQLite for local persistence
- `modernc.org/sqlite` if avoiding CGO
- `github.com/mattn/go-sqlite3` if CGO is acceptable

Prefer `modernc.org/sqlite` for easier cross-platform builds unless the repository has already chosen another driver.

### Frontend

Use React + Vite if a frontend already exists or is planned.

Recommended frontend behavior:

- Clean dashboard UI
- Request list panel
- Request detail panel
- Tabs for Headers, Body, Forward Response, Replay
- JSON pretty view
- Editable body textarea for replay in v1

Do not overcomplicate the frontend with a heavy editor in early milestones.

A simple textarea is acceptable for the first replay editor.

### Realtime Updates

Use Server-Sent Events first.

Endpoint:

```txt
GET /api/events
```

SSE is enough for one-way updates from server to dashboard.

Avoid WebSockets unless there is a clear need later.

---

## Suggested Repository Structure

Use a clean Go project layout:

```txt
hookstash/
  cmd/
    hookstash/
      main.go
  internal/
    app/
      app.go
    server/
      server.go
      routes.go
      handlers.go
    capture/
      handler.go
      forwarder.go
      replay.go
      curl.go
      provider_hint.go
    store/
      sqlite.go
      migrations.go
      models.go
      queries.go
    stream/
      sse.go
      broker.go
    config/
      config.go
    ui/
      embed.go
  web/
    src/
    public/
    dist/
    package.json
    vite.config.ts
  examples/
    fastapi-webhook/
    express-webhook/
  docs/
    tunnel-setup.md
    provider-examples.md
  go.mod
  go.sum
  README.md
  AGENTS.md
```

Do not create unnecessary packages too early.

If the codebase is still small, prefer fewer packages with clear boundaries.

---

## Core Domain Model

### Captured Request

A captured request represents an incoming webhook received by Hookstash.

Suggested fields:

```go
type CapturedRequest struct {
    ID                string
    Method            string
    Path              string
    QueryString       string
    HeadersJSON       string
    Body              []byte
    BodyText          string
    ContentType       string
    RemoteAddr        string
    ReceivedAt        time.Time
    ProviderHint      string
    ForwardStatus     string
    ForwardStatusCode *int
    ForwardError      *string
    ForwardDurationMS *int64
    TargetURL         *string
}
```

### Replay Attempt

A replay attempt represents resending a captured request to a target URL.

Suggested fields:

```go
type ReplayAttempt struct {
    ID                string
    RequestID         string
    TargetURL         string
    EditedBody        []byte
    EditedHeadersJSON string
    StatusCode        *int
    ResponseBody      *string
    Error             *string
    DurationMS        int64
    CreatedAt         time.Time
}
```

---

## SQLite Schema Guidance

Use migrations even for a local SQLite tool.

Initial schema:

```sql
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
```

Indexes to consider:

```sql
CREATE INDEX IF NOT EXISTS idx_requests_received_at ON requests(received_at DESC);
CREATE INDEX IF NOT EXISTS idx_requests_forward_status ON requests(forward_status);
CREATE INDEX IF NOT EXISTS idx_replay_attempts_request_id ON replay_attempts(request_id);
```

---

## API Design

Use JSON APIs for dashboard communication.

Recommended endpoints:

```txt
GET    /api/health
GET    /api/requests
GET    /api/requests/{id}
DELETE /api/requests
POST   /api/requests/{id}/replay
POST   /api/requests/{id}/retry
POST   /api/retry-failed
GET    /api/events
GET    /api/settings
PATCH  /api/settings
```

Webhook capture endpoints:

```txt
ANY /hooks/default
ANY /hooks/{name}
```

For v1, `/hooks/default` is enough.

Return responses clearly.

Example response from capture endpoint:

```json
{
  "id": "req_123",
  "captured": true,
  "forward_status": "failed",
  "forward_error": "connection refused"
}
```

Do not expose internal Go errors directly to the frontend without sanitizing them.

---

## Request Capture Rules

When handling an incoming webhook:

1. Generate request ID.
2. Read raw body into bytes.
3. Copy headers into a serializable structure.
4. Detect provider hint from headers if possible.
5. Save request to SQLite immediately.
6. Notify SSE subscribers.
7. If `--forward` is configured, forward request to the target.
8. Save forward result.
9. Notify SSE subscribers again if status changed.
10. Return a response to the original webhook sender.

Important:

In Go, `r.Body` can only be read once. Store the body bytes and reuse them for persistence and forwarding.

Example pattern:

```go
body, err := io.ReadAll(r.Body)
if err != nil {
    // handle error
}
_ = r.Body.Close()
```

For forwarding:

```go
forwardReq, err := http.NewRequestWithContext(ctx, r.Method, targetURL, bytes.NewReader(body))
```

---

## Forwarding Rules

Forwarding should preserve the original request as much as possible.

Preserve:

- Method
- Body
- Content-Type
- Relevant custom headers
- Signature headers
- Event headers

Do not blindly forward hop-by-hop headers.

Avoid forwarding:

- `Connection`
- `Keep-Alive`
- `Proxy-Authenticate`
- `Proxy-Authorization`
- `TE`
- `Trailer`
- `Transfer-Encoding`
- `Upgrade`
- `Host`
- `Content-Length`

Set a reasonable timeout.

Recommended default timeout:

```txt
10 seconds
```

Forward status values:

```txt
not_configured
forwarded
failed
queued
```

If the target server is down, mark as `queued` or `failed` depending on the current queue design.

For user-facing UI, prefer `queued` when the request is eligible for retry.

---

## Replay Rules

Replay must support two modes:

1. Replay original request unchanged.
2. Replay with edited body and/or headers.

Endpoint:

```txt
POST /api/requests/{id}/replay
```

Suggested payload:

```json
{
  "target_url": "http://localhost:8000/webhooks",
  "body": "{\"event\":\"payment.success\"}",
  "headers": {
    "Content-Type": "application/json"
  }
}
```

Replay should:

- Load captured request.
- Use target URL from payload or current settings.
- Use edited body if provided.
- Use edited headers if provided.
- Send HTTP request.
- Store replay attempt.
- Return response status/body preview.

Do not mutate the original captured request when replaying.

---

## Queue and Retry Rules

Offline-first capture means:

> Once Hookstash receives the request, it must save it before forwarding so the request is not lost if the user’s backend is down.

For failed forwards:

- Save failure reason.
- Mark request as queued or failed.
- Allow manual retry.
- Add retry-all failed forwards.
- Optional: add auto-retry loop later.

Do not retry forever without limits.

Suggested retry behavior for v1:

- Manual retry one request
- Manual retry all failed requests
- Optional auto-retry disabled by default

If auto-retry is added:

- Use a configurable interval.
- Avoid high-frequency retry loops.
- Add cancellation with context.

---

## Provider Hint Detection

Provider hints are optional but useful for UI badges.

Detect based on headers:

```txt
Stripe: stripe-signature
GitHub: x-github-event, x-hub-signature-256
Paystack: x-paystack-signature
Flutterwave: verif-hash
Shopify: x-shopify-hmac-sha256
Telegram: x-telegram-bot-api-secret-token or path hints
Discord: x-signature-ed25519, x-signature-timestamp
```

Provider hints should not block capture.

If unknown, use:

```txt
unknown
```

Do not claim signature verification is done unless it is actually implemented.

---

## cURL Export Rules

The “Copy as cURL” feature should generate a command that recreates the request.

Include:

- Method
- Target URL
- Headers
- Raw body

Avoid including sensitive headers by default if the UI later supports redaction.

For v1, preserve accuracy and warn users if secrets may appear in exported commands.

Example:

```bash
curl -X POST 'http://localhost:8000/webhooks' \
  -H 'Content-Type: application/json' \
  -H 'X-Paystack-Signature: ...' \
  --data-raw '{"event":"charge.success"}'
```

---

## UI Requirements

The dashboard should be simple and useful.

Recommended layout:

```txt
Top bar:
- Hookstash name
- Current webhook endpoint
- Forward target
- Status indicator

Left panel:
- Request list
- Method
- Provider badge
- Forward status
- Timestamp

Right panel:
- Request detail
- Headers tab
- Body tab
- Forward response tab
- Replay tab
```

Important UI states:

- Empty state when no request has arrived
- Captured request state
- Forwarded successfully state
- Forward failed/queued state
- Replay success state
- Replay error state

Use plain, beginner-friendly text.

Examples:

```txt
No webhooks yet. Send a POST request to /hooks/default to see it here.
```

```txt
Forward failed because your local backend did not respond. The request has been saved and can be retried.
```

---

## CLI Requirements

Default command:

```bash
hookstash
```

Should print:

```txt
Hookstash is running.

Dashboard:       http://localhost:4040
Webhook URL:     http://localhost:4040/hooks/default
Forward target:  not configured
Database:        ~/.hookstash/hookstash.db
```

With forwarding:

```bash
hookstash --forward http://localhost:8000/webhooks
```

Should print:

```txt
Hookstash is running.

Dashboard:       http://localhost:4040
Webhook URL:     http://localhost:4040/hooks/default
Forward target:  http://localhost:8000/webhooks
Database:        ~/.hookstash/hookstash.db
```

Supported flags for v1:

```txt
--port
--host
--forward
--db
--open
--log-level
```

Supported commands for v1 or near-v1:

```txt
hookstash
hookstash version
hookstash clear
```

Do not add too many CLI commands before the core dashboard works.

---

## Configuration Rules

Configuration sources, in priority order:

1. CLI flags
2. Environment variables
3. Config file
4. Defaults

Suggested environment variables:

```txt
HOOKSTASH_PORT
HOOKSTASH_HOST
HOOKSTASH_FORWARD_URL
HOOKSTASH_DB_PATH
HOOKSTASH_LOG_LEVEL
```

Default database path:

```txt
~/.hookstash/hookstash.db
```

Do not store secrets unless necessary.

---

## Security Notes

Hookstash is a local developer tool, but security still matters.

Rules:

- Bind to `127.0.0.1` by default, not `0.0.0.0`.
- If users bind to `0.0.0.0`, show a warning.
- Do not expose dashboard publicly by default.
- Do not log full secrets in terminal logs.
- Be careful with request headers that may contain tokens.
- Add optional header redaction later.

Default host:

```txt
127.0.0.1
```

Warning example:

```txt
Warning: Hookstash is listening on 0.0.0.0. Your dashboard may be reachable from other devices on your network.
```

---

## Testing Requirements

Every meaningful backend change should include tests where practical.

Minimum test areas:

- Capturing request stores method/path/headers/body
- Forwarding preserves body and content type
- Failed forward is saved as failed/queued
- Replay sends original body
- Edited replay sends edited body
- Provider hint detection
- cURL export generation
- SQLite migration setup

Use Go tests:

```bash
go test ./...
```

Use `httptest` for HTTP tests.

Prefer tests that do not require external network access.

---

## Quality Commands

Before considering work complete, run:

```bash
gofmt -w .
go test ./...
go vet ./...
```

If frontend exists, also run:

```bash
npm install
npm run build
npm run lint
```

Only run commands that are valid for the current repo.
If the repo does not have a frontend yet, do not invent frontend checks.

---

## Git and Commit Rules

Make focused commits.

Good commit examples:

```txt
feat: capture incoming webhook requests
feat: add sqlite persistence for captured requests
feat: forward captured requests to target url
feat: add replay endpoint
fix: preserve raw body during forwarding
```

Avoid vague commits:

```txt
update files
changes
fix stuff
```

Do not commit generated binaries unless the repository explicitly requires it.

---

## Milestone Plan for Agents

Work in this order.

### Milestone 1: Basic Capture

Goal:

- Start local server.
- Capture requests at `/hooks/default`.
- Store method, path, headers, body, timestamp.
- List requests through an API.

Deliverables:

```txt
GET /api/health
ANY /hooks/default
GET /api/requests
GET /api/requests/{id}
SQLite storage
Basic tests
```

Manual test:

```bash
curl -X POST http://localhost:4040/hooks/default \
  -H "Content-Type: application/json" \
  -d '{"event":"payment.success","amount":5000}'
```

### Milestone 2: Forwarding

Goal:

- Add `--forward` flag.
- Forward captured requests to local backend.
- Save forward result.

Deliverables:

```txt
Forwarder package/function
Forward status fields in DB
Forward status in API response
Tests with httptest target server
```

### Milestone 3: Dashboard v1

Goal:

- Add local dashboard.
- Show request list and details.

Deliverables:

```txt
GET / serves dashboard
Request list
Request details
Headers display
Body display
Forward status display
```

### Milestone 4: Realtime Updates

Goal:

- New requests appear without manual refresh.

Deliverables:

```txt
GET /api/events
SSE broker
Frontend EventSource integration
```

### Milestone 5: Replay

Goal:

- Replay captured requests.

Deliverables:

```txt
POST /api/requests/{id}/replay
Replay attempt storage
Replay response display
Tests
```

### Milestone 6: Edit Before Replay

Goal:

- Modify body and headers before replay.

Deliverables:

```txt
Editable body UI
Editable headers UI or simple JSON headers input
JSON validation for JSON content
Replay attempt history
```

### Milestone 7: Queue and Retry

Goal:

- Failed forwards can be retried.

Deliverables:

```txt
Queued/failed status
POST /api/requests/{id}/retry
POST /api/retry-failed
Retry button
Retry all button
```

### Milestone 8: Export and Polish

Goal:

- Make the tool feel complete.

Deliverables:

```txt
Copy as cURL
Provider badges
Search/filter
Clear history
README
Demo instructions
Dockerfile
GitHub Actions
```

---

## Definition of Done

A task is done when:

- The feature works manually.
- Relevant tests pass.
- Errors are handled clearly.
- The UI/API communicates useful status to the user.
- The implementation does not break existing behavior.
- The code is formatted.
- The README or docs are updated if the user-facing behavior changed.

---

## Documentation Style

Write docs in direct, practical language.

Prefer:

```txt
Run Hookstash with a forward target:
```

Over:

```txt
Users may optionally configure a target forwarding resource depending on their environment.
```

Good documentation sections:

- What Hookstash does
- What Hookstash does not do
- Quick start
- Testing with curl
- Testing with ngrok
- Testing with Paystack
- Testing with GitHub
- Replay requests
- Retry failed forwards
- Security notes

---

## README Opening

Use this as the starting README direction:

```md
# Hookstash

Hookstash is a zero-registration webhook inspector for backend developers.

Run a single Go binary, capture incoming webhooks, inspect headers and payloads in a local dashboard, edit JSON, replay requests, and queue failed forwards when your local server is down.

No account. No auth token. No cloud dashboard. Just a local-first webhook workbench for testing Stripe, GitHub, Paystack, Flutterwave, Shopify, Telegram, and other webhook integrations.
```

---

## Example Quick Start

```bash
hookstash --forward http://localhost:8000/webhooks
```

Then open:

```txt
http://localhost:4040
```

Send a test webhook:

```bash
curl -X POST http://localhost:4040/hooks/default \
  -H "Content-Type: application/json" \
  -d '{"event":"charge.success","amount":5000}'
```

The dashboard should show:

- Method: POST
- Provider: unknown
- Body: JSON payload
- Forward target: `http://localhost:8000/webhooks`
- Forward status: success or failed

---

## Common Agent Mistakes to Avoid

Do not:

- Build a cloud service in v1.
- Add authentication before the local dashboard works.
- Parse JSON and discard the raw body.
- Forward before saving.
- Require Docker for local use.
- Require an account or token.
- Add a tunnel implementation too early.
- Use WebSockets when SSE is enough.
- Create a huge plugin architecture before core capture/replay works.
- Silently ignore forwarding errors.
- Store only the last request.
- Break cross-platform builds without reason.
- Bind publicly by default.

---

## Preferred Implementation Order

Always follow this order unless the user explicitly changes priorities:

```txt
Capture
→ Store
→ Forward
→ Dashboard
→ Realtime
→ Replay
→ Edit
→ Queue
→ Export
→ Package
```

This order keeps the project useful early and prevents scope creep.

---

## Future Ideas

These are not v1 requirements.

Potential future features:

- Signature verification helpers
- Provider-specific templates
- Multiple webhook endpoints
- Request diff viewer
- Session import/export
- Header redaction
- Team mode
- Optional hosted relay service
- Homebrew install
- VS Code extension
- Browser notifications
- Request body schema validation
- Mock response rules
- Webhook contract testing

Do not implement these until the MVP is stable.

---

## Agent Behavior Guidelines

When working on this codebase:

1. Read this file first.
2. Identify the current milestone.
3. Keep changes focused.
4. Prefer simple working code over abstract architecture.
5. Add or update tests for backend behavior.
6. Preserve local-first and zero-registration principles.
7. Do not introduce cloud dependencies without explicit instruction.
8. Do not rename the product without explicit instruction.
9. Explain tradeoffs when making architectural choices.
10. Update README/docs when user-facing behavior changes.

---

## Current Product Name

Use the name:

```txt
Hookstash
```

Reason:

The original name “HookRelay” is too close to existing tools and domains such as Webhook Relay and other HookRelay projects. Hookstash better communicates capture, storage, inspection, and replay.

---

## Final Reminder

Hookstash should feel simple, useful, and local.

The best version of this project is not the one with the most features.

The best version is the one where a backend developer can run one command, receive a webhook, inspect what happened, fix their local endpoint, and replay the exact same request without stress.
