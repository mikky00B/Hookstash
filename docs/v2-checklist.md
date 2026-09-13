# Hookstash v2 Checklist

Live progress tracker for the v2 branch. Update as work lands; each milestone
maps to a section in `docs/v2-plan.md`.

## M1 — Multi-endpoint capture

- [x] Versioned schema migrations (base + v2 migration for endpoints)
- [x] `endpoints` table + Endpoint model + store methods (create/list/get/delete)
- [x] Token auth on capture (Bearer header or `?token=`), token stored hashed
- [x] Capture routing `/hooks/{slug}` with 404 for unknown slugs
- [x] `/hooks/default` backward compat (built-in `default` endpoint)
- [x] Requests scoped by endpoint; `?endpoint=` filter on list API
- [x] Endpoints CRUD API (`POST/GET/DELETE /api/endpoints`)
- [x] CLI: `hookstash endpoint add|list|rm`
- [x] Dashboard: endpoint bar with chips, create-endpoint form, one-time token display
- [x] Dashboard rebuilt (`npm run build` in web/)
- [x] `go test ./...` green
- [x] End-to-end smoke test (create tokened endpoint → 401 without token → capture with Bearer → filtered list)

## M2 — Public URL (wrapped cloudflared)

- [x] `--public` flag / `Public URL` button spawns `cloudflared tunnel --url ...`
- [x] Parse trycloudflare.com URL from tunnel output, show + copy in dashboard
- [x] Tunnel lifecycle managed (kill on shutdown, restart button)
- [x] Tunnel status over SSE (`tunnel.started`, `tunnel.error`)
- [x] Fallback card with install instructions when cloudflared is missing
- [x] `--tunnel-url` / `HOOKSTASH_TUNNEL_URL` for user-managed tunnels
- [x] Smoke-tested: fresh status (`disabled`), missing-binary fallback (error + install hint), external mode (`--tunnel-url`)

## M3 — Signature Lab: verify

- [ ] Secret/provider config per endpoint (dashboard + API)
- [ ] Stripe verification (timestamp scheme) + test vectors
- [ ] GitHub, Paystack, Shopify HMAC verification + test vectors
- [ ] Flutterwave / Telegram static-token verification
- [ ] Signature badge on requests (valid / invalid / unknown)
- [ ] Computed-vs-received signature detail in dashboard

## M4 — Signature Lab: re-sign

- [ ] "Re-sign as <provider>" in replay editor
- [ ] Stripe tolerance controls (timestamp freshness) for testing expired events

## M5 — Capture-to-test

- [ ] cURL export (`GET /api/requests/{id}/export?format=curl`)
- [ ] Go httptest fixture export
- [ ] JSON / raw export

## M6 — Retry queued forwards

- [ ] `POST /api/requests/{id}/retry`
- [ ] Sweep retry per endpoint
- [ ] Optional `--retry-interval` background sweeper

## Docs

- [x] `docs/v2-plan.md`
- [x] `docs/setup-and-keys.md` (what the user must install/provide)
- [x] README rewrite for v2 (endpoints, tunnel, signature lab)
