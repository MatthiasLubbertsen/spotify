# AGENTS.md — spotis

Living project doc. Keep it current: update the relevant section in the same change as the code.
Sections are numbered, subsections lettered (refer to them like "7c").

## 1. Purpose

**1a. What it is.** `spotis` is a self-hosted Go service that registers as the ONE real Spotify
app (Spotify allows a developer one app in the Developer Portal, with an allowlist of users) and
exposes any number of *virtual apps*. Each virtual app has its own client ID, secret, redirect URIs
and limits. spotis mimics Spotify's OAuth (`/authorize`, `/api/token`) and Web API (`/v1/*`) with the
same paths, so a normal Spotify client library only needs its base URLs swapped.

**1b. Hard limit.** Spotify's allowlist still applies upstream: every end user must be added under
*Users management* of the real app (dev mode: max 5 users, owner needs Premium). Cannot be bypassed.
The dashboard explains this (Upstream page) and users who are not allowlisted get an explanatory page
at login (`oauth.Callback`, 403 from `/v1/me`).

**1c. Naming.** Short name: `spotis`. Always write "dashboard" (never the other word). Hack Club, if mentioned, is "Hack Club".

## 2. Decisions (and why)

**2a. Storage:** SQLite (pure Go `modernc.org/sqlite`, WAL) in `SPOTIS_DATA_DIR`. Chosen with the owner.
**2b. Tokens are ours, not Spotify's.** Clients get opaque spotis tokens (stored as SHA-256). Each (virtual app, Spotify user) is a *grant* holding the upstream refresh token, AES-GCM encrypted. The proxy swaps in a fresh upstream access token (cached per grant, refreshed with a per-grant lock). Revoking a grant cascades to all its tokens.
**2c. One grant per (app, user).** Re-login updates the same grant. Different apps never share upstream tokens, so the upstream scope = what that app requested.
**2d. Upstream auth:** authorization-code with the client secret (Spotify apps always have one). No upstream PKCE.
**2e. Refresh tokens we issue:** never rotated and never expire (revocation via dashboard / upstream `invalid_grant` → grant status `reauth`). Refresh responses repeat the same `refresh_token` (Spotify makes it optional; clients cope either way).
**2f. Client secrets:** 32 hex chars, shown once, stored hashed. Rotation is immediate (old secret stops working; issued tokens stay valid).
**2g. No separate "API keys".** The client secret is the credential; client-credentials tokens cover app-only calls. Added per-app: enabled flag, allowed scopes, per-minute rate limit, client-credentials toggle.
**2h. Secrets at rest:** upstream client secret + refresh tokens encrypted with a key derived from `SPOTIS_SECRET_KEY`, else generated once in `$DATA_DIR/secret.key` (0600). Losing the key = every user must log in again.
**2i. Single host.** Accounts and API paths share one host (`/authorize`, `/api/token`, `/v1/*`) since they don't collide. The dashboard lives under `/dashboard` (UI and `/dashboard/api`).
**2j. Image/registry:** GHCR (`ghcr.io/<owner>/<repo>`), multi-arch amd64+arm64. Repo is named `spotify`, app is `spotis`.
**2k. Spotify's July 2026 changelog says developers can now create 25 apps** (the Feb 2026 limit was 1) and dev-mode quota is counted per developer account. spotis still works as specified; just be aware.

## 3. Routing and auth

| Path | Who | Protection |
|---|---|---|
| `/` , `/assets/*` | anyone | public static landing page |
| `/healthz` | anyone | public |
| `GET /authorize` | end users' browsers | validates `client_id` + exact `redirect_uri` (loopback ports free), else error page |
| `GET /oauth/callback` | **Spotify** (the one fixed upstream redirect URI) | single-use `state` bound to a pending request |
| `POST /api/token` | client libraries | virtual app client auth (Basic or body) / PKCE verifier |
| `/v1/*` | client libraries | `Bearer` spotis token |
| `/dashboard`, `/dashboard/*`, `/dashboard/api/*` | the owner | **Tinyauth via Traefik forward-auth** |

**3a. Traefik:** two routers (see `docker-compose.yml`): public one for the host, plus a higher-priority router for `PathPrefix(/dashboard)` carrying the `tinyauth@docker` middleware. No OAuth inside the app.
**3b. Defence in depth:** the app also refuses `/dashboard*` without the forwarded `Remote-User`/`Remote-Email` header, optionally restricts to `SPOTIS_DASHBOARD_USERS`, and rejects cross-origin writes (Origin / Sec-Fetch-Site). Never expose the app port directly: those headers are only trustworthy behind Traefik.
**3c. Upstream redirect URI to register at Spotify:** `${SPOTIS_PUBLIC_URL}/oauth/callback` (https).

## 4. OAuth behaviour (mimicking Spotify)

Researched from developer.spotify.com (Oct 2026):
**4a. Flows supported:** Authorization Code (secret), Authorization Code + PKCE (S256 only), Client Credentials. Implicit grant is deprecated upstream and not implemented.
**4b. `/authorize`:** params `client_id, response_type=code, redirect_uri, state, scope, show_dialog, code_challenge(_method)`. Errors go to the redirect URI as `error` (+ `state`); bad client/redirect shows an error page, never a redirect. `show_dialog=true` is forwarded to Spotify.
**4c. `/api/token`:** `authorization_code` (needs secret OR PKCE verifier; redirect_uri must match), `refresh_token` (secret or `client_id` for PKCE), `client_credentials` (secret; no refresh token). Response: `access_token, token_type=Bearer, expires_in=3600, scope, refresh_token`. Errors: `{error, error_description}` with 400/401 like Spotify.
**4d. Redirect URIs** follow Spotify: https, or http on `127.0.0.1`/`[::1]` (never `localhost`); a registered loopback URI without port matches any port; custom schemes allowed for native apps.
**4e. Scopes:** passed through to Spotify untouched (Spotify validates names); per-app allowlist optional (`invalid_scope` redirect).

## 5. Web API proxy

**5a.** `/v1/*` forwarded to `https://api.spotify.com` with method, query, body and headers (minus hop-by-hop, cookies, forwarded-for, Accept-Encoding).
**5b. JSON URL rewriting:** `https://api.spotify.com` in JSON bodies and `Location`-style headers is rewritten to `SPOTIS_PUBLIC_URL`, so `next`/`previous`/`href` paging links keep working through spotis.
**5c. Errors:** our own 401s use Spotify's `{"error":{"status","message"}}` format ("No token provided", "Invalid access token", "The access token expired").
**5d. Rate limits:** Spotify limits per rolling 30 s window and dev-mode quota buckets (`429` + `Retry-After`, body may include `reason: QUOTA_EXCEEDED`). All virtual apps share ONE upstream quota, so an upstream 429 starts a global cooldown (Retry-After, capped at 300 s) during which spotis answers 429 itself. Plus optional per-app per-minute limit (fixed window).
**5e. Stale upstream token:** an upstream 401 triggers one transparent retry with a fresh token (only when the body is replayable).
**5f. Not mimicked:** Spotify removed many endpoints for dev-mode apps in Feb 2026 (batch gets, `/me/tracks` etc. → `/me/library`, `/playlists/{id}/tracks` → `/items`, search limit max 10). spotis does not translate; Spotify's own errors pass through.

## 6. Data model (SQLite)

`settings` (upstream client id + encrypted secret) · `apps` · `users` (Spotify accounts) · `grants` (app×user, encrypted upstream refresh token, status `ok|reauth`) · `tokens` (hashed access/refresh; `grant_id` NULL for client-credentials) · `auth_requests` (10 min) · `auth_codes` (5 min, single use) · `requests` (usage log, pruned after `SPOTIS_RETENTION_DAYS`, default 30). OAuth log rows store the path only (queries carry codes).

## 7. Code map

**7a.** `cmd/spotis` entrypoint.
**7b.** `internal/server` mux + route map; `flow_test.go` full-flow test against a fake Spotify.
**7c.** `internal/oauth` authorize/callback/token. `internal/proxy` API proxy. `internal/upstream` the real-Spotify client, token cache, cooldown.
**7d.** `internal/dashboard` JSON API (`/dashboard/api`). `internal/store` SQLite. `internal/usage` async request logger. `internal/crypto`, `internal/util`, `internal/config`.
**7e.** `internal/webui` embeds `internal/webui/dist` (built from `frontend/`, not committed).
**7f.** `frontend/` Vite + React + TS + Tailwind + shadcn/ui, **dark only**. Landing at `/`, dashboard at `/dashboard/*`.

## 8. Configuration (env)

`SPOTIS_PUBLIC_URL` (default `https://spotify.matthiaz.dev`) · `SPOTIS_ADDR` (`:8080`) · `SPOTIS_DATA_DIR` (`./data`, `/data` in Docker) · `SPOTIS_SECRET_KEY` · `SPOTIS_DASHBOARD_USERS` (comma list, optional) · `SPOTIS_RETENTION_DAYS` · `SPOTIS_UPSTREAM_ACCOUNTS_URL` / `SPOTIS_UPSTREAM_API_URL` (tests/mocks only). Upstream Spotify credentials are NOT env: set them in the dashboard (Upstream page).

## 9. Build, test, release

**9a.** `make test` (vet + race tests) · `make web` (frontend) · `make build`.
**9b.** `go test ./internal/server` is the quick full-flow check: dashboard auth → upstream config → app create → authorize (PKCE) → callback → token → proxied call with URL rewriting → refresh → client credentials → secret rotation → revoke → rate limits → cooldown → allowlist page → usage log → app delete.
**9c.** CI (`.github/workflows/ci.yml`): typecheck/build frontend, `go vet`, `go test -race`, then multi-arch Docker image to GHCR (push on `main` and `v*` tags; PRs build only).
**9d.** Go toolchain follows `go.mod` (1.26).

## 10. First-time setup

1. Create the one app at developer.spotify.com/dashboard; add redirect URI `https://spotify.matthiaz.dev/oauth/callback`; add every end user under *Users management*.
2. Deploy with `docker-compose.yml`, open `/`, log in (Tinyauth).
3. Dashboard → Upstream: paste the real client ID/secret, press Test.
4. Dashboard → Apps → New app; use the Connect tab for the client snippet.

## 11. Open items / ideas

- Per-app webhook/audit export; optional grace period for rotated secrets.
- Cap refresh tokens per grant (they accumulate one per login).
