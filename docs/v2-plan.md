# Hookstash v2 Plan

Status: draft
Date: 2026-09-13
Baseline: master @ 8050cbc (replay editing shipped)

## Positioning

Hookstash v1 is a local-first webhook inspector. v2 makes it a **webhook.site
replacement** with a differentiator no hosted tool offers: **signature-aware
replay**.

Why developers leave webhook.site (verified pain points):

1. Free URLs expire in ~7 days and the captured data is deleted.
2. Free URLs cap out at 50–100 captured requests.
3. Payloads are readable by anyone who guesses the URL ID, and the domain is
   abused by malware campaigns, so security teams block it outright.
4. Localhost forwarding and API access are paid features.
5. Users report occasional delivery lag or dropped events.

v2 solves 1–4 by architecture: data lives in the user's local SQLite file, URLs
live as long as the binary runs, nothing leaves the machine, and the CLI/API is
the product rather than a paid add-on.

The standout feature is **Signature Lab** (below). Inspection alone is a
commodity; testing your signature-verification path without re-triggering the
provider is the friction every webhook developer hits and no major tool solves.

## Scope

### 1. Multi-endpoint capture (fixes the single-endpoint limitation)

Replace `/hooks/default` as the only capture path with named endpoints.

- `POST /api/endpoints` creates an endpoint: `{ "name": "payments" }`
  → returns `{ "id": "ep_...", "slug": "payments", "token": "hs_..." }`.
- Capture path becomes `ANY /hooks/{slug}`. Requests to unknown slugs return
  404 and are logged (debug level) so typos are discoverable.
- Optional per-endpoint token: if set, capture requires
  `Authorization: Bearer <token>` or `?token=<token>`. This stops randos from
  polluting an endpoint while a tunnel is open.
- Keep `/hooks/default` working, mapped to a built-in `default` endpoint, so
  v1 muscle memory and the README quick start still work.
- Requests are scoped by endpoint; list/detail APIs gain an
  `?endpoint=<slug>` filter. The dashboard groups the sidebar by endpoint.
- CLI: `hookstash endpoint add payments`, `hookstash endpoint list`.

Storage: new `endpoints` table (`id`, `slug`, `token_hash`, `created_at`);
`requests` gains `endpoint_id` (migrated rows map to `default`).

### 2. Public URL via wrapped cloudflared (no accounts, no infra)

Hookstash stays a single binary; it does not run a relay service.

- `hookstash --public` (or the `Public URL` button in the dashboard) spawns
  `cloudflared tunnel --url http://127.0.0.1:<port> --no-autoupdate` as a child
  process, parses the assigned `https://<something>.trycloudflare.com` URL from
  its output, and shows it in the dashboard with a copy button.
- Quick tunnels are ephemeral and need no Cloudflare account. Named tunnels
  (stable URL, requires `cloudflared tunnel login` on the user's machine) are a
  stretch goal: if a named tunnel already exists, respect
  `HOOKSTASH_TUNNEL_URL` / `--tunnel-url` to just display a user-managed URL.
- If `cloudflared` is not on PATH: dashboard shows a dismissible card with
  install instructions per OS; capture still works locally.
- The tunnel process is killed on shutdown (signal handling already needed for
  clean SQLite close).
- Status is published over the existing SSE stream (`tunnel.started`,
  `tunnel.error`) so the dashboard updates live.
- Non-goal: hosting a relay. That drags in abuse handling (see webhook.site's
  malware problem), uptime, and cost — the exact v1 philosophy rejects it.

### 3. Signature Lab (the headline feature)

Two capabilities: **verify** on capture and **re-sign** on replay.

Verify on capture:

- Per-endpoint (or per-request, via the dashboard) secret configuration.
- On capture, Hookstash computes the provider's signature over the raw body
  using the configured secret and marks the request
  `signature: valid | invalid | unknown` (unknown = no signature header present
  or no secret configured).
- Provider schemes, in order of demand:

  | Provider | Header | Scheme |
  |---|---|---|
  | Stripe | `Stripe-Signature` | `t=<ts>,v1=hex(hmac_sha256(secret, ts + "." + body)))` |
  | GitHub | `X-Hub-Signature-256` | `sha256=hex(hmac_sha256(secret, body))` |
  | Paystack | `X-Paystack-Signature` | `hex(hmac_sha256(secret, body))` |
  | Shopify | `X-Shopify-Hmac-Sha256` | `base64(hmac_sha256(secret, body))` |
  | Flutterwave | `verif-hash` | static secret comparison (no HMAC) |
  | Telegram | `X-Telegram-Bot-Api-Secret-Token` | static token comparison |
  | Discord | `X-Signature-Ed25519` + `X-Signature-Timestamp` | Ed25519, verify-only (needs the app's public key; we cannot re-sign) |

  Provider detection already exists (`internal/capture/provider_hint.go`); this
  builds on it. Stripe is first: it has the timestamp-tolerance wrinkle and the
  most users.
- Dashboard: signature badge next to each request (green check / red x / gray
  dash) plus the computed vs. received signature in the detail view, so a
  mismatch tells you *which* part is wrong (wrong secret, body mutated by a
  proxy, tolerance expired).

Re-sign on replay (the killer move):

- In the replay editor (already shipped), a "Re-sign as <provider>" action
  recomputes the signature header from the *edited* body using the secret the
  user supplies in the UI (kept in memory / optionally in local config, never
  sent anywhere).
- This lets a developer edit a Stripe payload — change `amount`, mark the event
  as already processed, simulate a replay attack with an old timestamp — and
  still have their local handler's verification accept it. Testing the
  verification path without re-triggering a real provider event is the thing
  webhook.site simply does not do.
- Static-token providers (Flutterwave, Telegram) re-sign trivially. Discord is
  verify-only.

Security stance: secrets are opt-in to persist (plaintext in local SQLite,
documented as such; the binary is already local-only by default). The README's
existing "captured headers may contain secrets" note extends to this.

Storage: `requests` gains `signature_status` and `signature_detail` (JSON:
received, computed, algorithm); `endpoints` gains `provider` and optional
`secret_hash`/`secret` columns.

### 4. Capture-to-test (fast follow, same v2 window)

Turn a captured request into a runnable artifact, from the dashboard and
`GET /api/requests/{id}/export?format=...`:

- `curl` — the already-planned export; filters hop-by-hop headers like replay
  does.
- `go-httptest` — emits a self-contained Go test that posts the exact method,
  headers, and raw body to a configurable handler target. This is the fixture
  format for regression-testing webhook handlers.
- `json` — full stored record for custom tooling.
- `raw` — original body bytes (for signature tools, scripts).

### 5. Retry queued forwards (existing roadmap item)

- `POST /api/requests/{id}/retry` re-attempts the forward with the stored
  target URL.
- `POST /api/endpoints/{slug}/retry-failed` sweeps a whole endpoint.
- Optional `--retry-interval` background sweeper (off by default).

## Milestones

1. **M1 — Endpoints**: schema migration, capture routing, token auth, API +
   dashboard grouping, `/hooks/default` compat. No behavior change otherwise.
2. **M2 — Public URL**: cloudflared wrapper, tunnel lifecycle, SSE status,
   dashboard UI, graceful shutdown.
3. **M3 — Signature Lab, verify**: per-provider verification for Stripe,
   GitHub, Paystack, Shopify (+ static-token providers), signature badges,
   secrets UI.
4. **M4 — Signature Lab, re-sign**: re-sign action in the replay editor,
   tolerance controls for Stripe.
5. **M5 — Capture-to-test**: curl / go-httptest / json / raw exports.
6. **M6 — Retry**: manual + sweep retries, forward-status plumbing.

M3–M4 can be developed behind the M1 endpoint model (secrets live on
endpoints), which is why M1 comes first. M5 and M6 are independent and can
reorder freely.

## Non-goals (unchanged from v1)

No accounts, no hosted relay, no cloud sync, no teams, no billing. v2 is still
one Go binary plus one SQLite file. The public URL comes from the user's own
cloudflared, not our infrastructure.

## Docs work

- README: rewrite Status/Roadmap for v2, add Signature Lab quick start
  (a Stripe-style signed webhook you can replay against any local handler),
  extend security notes (secrets in SQLite, tunnel exposure).
- New `docs/signature-schemes.md` documenting each provider's exact scheme
  with a test vector, so verification behavior is verifiable in CI.
