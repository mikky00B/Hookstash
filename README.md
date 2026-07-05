# Hookstash

Hookstash is a zero-registration webhook inspector for backend developers.

Run a single Go binary, capture incoming webhooks, inspect headers and payloads, and forward requests to your local server without losing the original webhook when your backend is down.

No account. No auth token. No cloud dashboard. Just a local-first webhook workbench for testing Stripe, GitHub, Paystack, Flutterwave, Shopify, Telegram, and other webhook integrations.

## Status

Hookstash is early. The current build includes:

- Local HTTP server
- SQLite request storage
- Webhook capture at `/hooks/default`
- Request listing and detail APIs
- Optional forwarding to a local target
- Queued status when forwarding fails
- Provider hints from common webhook headers
- Local dashboard for browsing captured requests
- Realtime dashboard refresh with Server-Sent Events
- Replay captured requests to a target URL

Replay editing, retry controls, and cURL export are planned next.

## Quick Start

Run Hookstash:

```bash
go run ./cmd/hookstash
```

Open:

```txt
http://127.0.0.1:4040
```

Send a test webhook:

```bash
curl -X POST http://127.0.0.1:4040/hooks/default \
  -H "Content-Type: application/json" \
  -d '{"event":"charge.success","amount":5000}'
```

List captured requests:

```bash
curl http://127.0.0.1:4040/api/requests
```

The dashboard fetches captured requests from `GET /api/requests` and shows request details, headers, body, provider hints, and forwarding results. It also listens to `GET /api/events` so new captures appear without refreshing the page.

## Forward To Your App

Start Hookstash with a forward target:

```bash
go run ./cmd/hookstash --forward http://127.0.0.1:8000/webhooks
```

Then send webhooks to Hookstash:

```txt
http://127.0.0.1:4040/hooks/default
```

Hookstash saves the request first, then forwards it to your app. If your app is unavailable, the captured request remains stored and the forward status is marked as `queued`.

Recommended external provider flow:

```txt
Provider webhook
        ↓
Public tunnel URL
        ↓
Hookstash localhost:4040
        ↓
Your app localhost:8000/webhooks
```

Hookstash does not provide a public tunnel. Use a tool like ngrok or Cloudflare Tunnel and point it to Hookstash.

## CLI Flags

```txt
--host        Host to bind. Default: 127.0.0.1
--port        Port to listen on. Default: 4040
--forward     Optional local webhook target URL
--db          SQLite database path. Default: ~/.hookstash/hookstash.db
--open        Reserved for opening the dashboard
--log-level   Log level. Default: info
```

Environment variables:

```txt
HOOKSTASH_HOST
HOOKSTASH_PORT
HOOKSTASH_FORWARD_URL
HOOKSTASH_DB_PATH
HOOKSTASH_LOG_LEVEL
```

CLI flags take priority over environment variables.

## API

Health check:

```txt
GET /api/health
```

Capture a webhook:

```txt
ANY /hooks/default
```

List captured requests:

```txt
GET /api/requests
```

Get one captured request:

```txt
GET /api/requests/{id}
```

Subscribe to dashboard events:

```txt
GET /api/events
```

The event stream currently publishes `request.created` when a captured request is available.

Replay a captured request:

```txt
POST /api/requests/{id}/replay
```

Payload:

```json
{
  "target_url": "http://127.0.0.1:8000/webhooks"
}
```

Replay sends the original method, headers, and body. It filters hop-by-hop headers such as `Connection`, `Transfer-Encoding`, `Content-Length`, and `Host`.

Example capture response:

```json
{
  "captured": true,
  "forward_error": null,
  "forward_status": "not_configured",
  "id": "req_..."
}
```

Forward status values currently used:

```txt
not_configured
forwarded
queued
failed
```

## Stored Request Data

Hookstash preserves the raw request body because webhook signatures often depend on the exact payload.

Stored fields include:

- HTTP method
- Path
- Query string
- Headers as JSON
- Raw body bytes
- Body text
- Content type
- Remote address
- Received timestamp
- Provider hint
- Forward status and error details
- Target URL

## Provider Hints

Provider hints are detected from common headers:

```txt
Stripe       stripe-signature
GitHub       x-github-event, x-hub-signature-256
Paystack     x-paystack-signature
Flutterwave  verif-hash
Shopify      x-shopify-hmac-sha256
Telegram     x-telegram-bot-api-secret-token
Discord      x-signature-ed25519, x-signature-timestamp
```

Provider hints are labels only. Hookstash does not verify signatures yet.

## Development

Run tests:

```bash
go test ./...
```

Run vet:

```bash
go vet ./...
```

Format source:

```bash
gofmt -w cmd internal
```

Build the dashboard:

```bash
cd web
npm install
npm run build
```

## Security Notes

Hookstash binds to `127.0.0.1` by default.

If you bind to `0.0.0.0`, your dashboard and capture endpoint may be reachable from other devices on your network. Only do that when you understand the exposure.

Captured headers may contain secrets. Be careful when sharing API responses, database files, or future exported cURL commands.

## Roadmap

Near-term milestones:

1. Edit body and headers before replay
2. Retry queued forwards
3. Export captured requests as cURL

Hookstash is intentionally not a hosted webhook platform in v1. No accounts, cloud sync, hosted webhook URLs, billing, teams, or API gateway behavior.
