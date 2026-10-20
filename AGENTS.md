# AGENTS.md

Single source of truth for how this app works. **Keep it up to date with every meaningful change** (new route, table,
env var, flow, decision). Sections are numbered, subsections lettered, so they can be cited as e.g. "see 5b".

Conventions: call the UI the **dashboard** everywhere (never "admin"). Write "Hack Club" with that exact casing if it
ever appears.

---

## 1. Overview

**1a. Purpose.** Spotify allows a developer only one app, usable only by allowlisted users, with impractical multiple
redirect URIs. This server registers as that one real Spotify app (single fixed redirect URI
`<PUBLIC_URL>/callback`) and lets the owner create unlimited *virtual apps*, each with its own client ID, secret,
redirect URIs, scope limit and rate limit. It mimics Spotify's OAuth and Web API (same paths) so client libraries only
swap base URLs.

**1b. Terms.**
- *Upstream*: the real Spotify (accounts.spotify.com, api.spotify.com) and the one real app registered there.
- *Virtual app* ("app"): our own client registration (table `apps`).
- *Connection*: one upstream Spotify grant for a (virtual app, Spotify user) pair; holds the encrypted upstream tokens.
- *Our tokens*: opaque bearer tokens we issue to virtual-app clients (`spx_…` access, `spr_…` refresh).

**1c. Non-goals.** No accounts/roles inside the app (dashboard login is Tinyauth), no bypass of Spotify's allowlist, no
mimicking of Spotify's consent UI (the user sees Spotify's real consent page for the real app), no web player SDK,
no mocking of Spotify responses (everything under `/v1` is proxied).

**1d. Limitation to always tell the owner.** Every end user must be on the real app's allowlist in Spotify's Developer
Dashboard (User Management; dev mode: max 5 users, owner needs Premium). The dashboard "Spotify app" page and the
README state this; the `/callback` shows a "Not allowed by Spotify" page when upstream answers 403 for `/v1/me`.

## 2. Architecture

**2a. Shape.** One Go binary (stdlib `net/http`, `modernc.org/sqlite`, pure Go, CGO off) serving: the Spotify-compatible
public surface, the dashboard API, and the embedded React frontend (`web/dist` via `go:embed`). One SQLite file.

**2b. File map.**
| File | Responsibility |
|---|---|
| `main.go` | `Server`, routes, static serving, settings KV, request log writer, janitor, `healthcheck` subcommand |
| `config.go` | env parsing (see 6a) |
| `crypto.go` | AES-256-GCM `seal/open`, token hashing, random helpers |
| `db.go` | schema (idempotent `CREATE TABLE IF NOT EXISTS`), SQLite open |
| `oauth.go` | `/authorize`, `/callback`, `/api/token`, upstream token refresh, upstream back-off |
| `proxy.go` | `/v1/*` proxy, per-app rate limit, response rewriting |
| `dashboard.go` | dashboard guard middleware + JSON API under `/dashboard/api` |
| `e2e_test.go` | full-flow test against a fake Spotify (see 10a) |
| `web/` | Vite + React + TypeScript + Tailwind v4 + shadcn/ui (see 7) |

**2c. Data path.**
```
client lib ──/authorize──> us ──302──> accounts.spotify.com/authorize (our client_id, our PKCE, fixed redirect)
user logs in at Spotify ──> us:/callback ──exchange code──> Spotify ──> we store upstream tokens (encrypted)
us ──302 (our code)──> virtual app redirect URI
client ──POST /api/token──> our tokens  ──GET /v1/*──> us ──swap bearer──> api.spotify.com
```

**2d. Concurrency.** SQLite pool is `MaxOpenConns(1)`; never run a query while iterating another's rows (collect, close,
then query). Upstream refresh is serialised per connection by a mutex (`connLocks`). Request logging is async through a
channel.

## 3. Flows

**3a. `/authorize` (GET, public).** Validates `client_id` (known + enabled) and `redirect_uri` (exact match against the
app's list). Errors on these two render an HTML page and never redirect. Other errors redirect to the client with
`error` + `state`: `unsupported_response_type` (must be `code`), `invalid_request` (`code_challenge_method` must be
`S256`, challenge must be 43 chars), `invalid_scope` (app has an allowed-scope list). PKCE is optional here: with a
challenge the client is a public PKCE client; without, it must authenticate with its secret at the token endpoint.
We store a `pending` row (10 min) with our own random PKCE verifier (sealed), set the `sp_flow` cookie (HttpOnly,
Lax, Secure on https) and redirect to upstream `/authorize` with: our upstream `client_id`, `redirect_uri=<PUBLIC_URL><callback>`,
`state=<pending id>`, our `code_challenge` (S256), the requested `scope` and `show_dialog` pass-through.

**3b. `/callback` (GET, public, the one URI registered at Spotify).** Atomically consumes the `pending` row
(`DELETE … RETURNING`), checks the `sp_flow` cookie equals the state (login-CSRF protection), forwards upstream
`error` to the client, exchanges the code (+ our verifier, no secret) for upstream tokens, calls upstream `GET /v1/me`
to learn the Spotify user id (upstream 403 → friendly allowlist page), upserts the `connections` row
(`UNIQUE(app_id,user_id)`: re-login replaces tokens and scope), creates our one-time code (10 min, hashed) and
redirects to the client's `redirect_uri` with `code` + original `state`.

**3c. `/api/token` (POST, public, form-encoded, CORS enabled).** Client auth: HTTP Basic or `client_id` (+ optional
`client_secret`) in the body. Wrong secret/unknown/disabled client → 401 `invalid_client`. Grants:
- `authorization_code`: code is single use (consumed even on failure), must belong to the app, `redirect_uri` must equal
  the one used at `/authorize`; if the code was issued with a PKCE challenge, `code_verifier` is required and checked
  (S256); otherwise a valid client secret is required. Returns `access_token`, `token_type=Bearer`, `scope`,
  `expires_in=3600`, `refresh_token`.
- `refresh_token`: public-client style (only `client_id`) or with secret, both accepted like Spotify. Returns a new
  access token and echoes the same refresh token (Spotify may omit it; echoing is safe for all libraries). Refresh
  tokens do not expire on our side; they die when the connection is revoked or upstream refresh fails with
  `invalid_grant` (connection auto-deleted).
- `client_credentials`: requires the client secret **and** the upstream client secret to be set (dashboard). Token has no
  connection; proxy uses the real app's cached client-credentials token (public data only).
- Error format is RFC 6749 JSON `{"error","error_description"}`.

**3d. API proxy `/v1/*` (public, bearer token).** Look up our access token by SHA-256 hash → app + optional connection →
enforce app enabled, per-app rate limit (3f), global upstream back-off (3g) → get a valid upstream access token
(refresh when <60 s left) → forward method, path, query, body (≤16 MB) and a small header allowlist with the upstream
bearer → relay status, headers and body. On upstream 401 it force-refreshes once and retries. JSON bodies and
`Location` headers have `https://api.spotify.com/` rewritten to `<PUBLIC_URL>/` so pagination `next`/`href` URLs keep
going through the proxy (important: spotipy's `sp.next()` follows them as absolute URLs). Non-JSON is streamed.
CORS is `*` (auth is bearer-only, no cookies).

**3e. Errors.** Web API shape `{"error":{"status","message"}}` for `/v1`; 401 "Invalid access token"/"The access token
expired"/"No token provided"; 502 when upstream is unreachable. Upstream 4xx/5xx (including 403 for non-allowlisted
users) are relayed untouched.

**3f. Per-app rate limit.** Optional requests/minute (fixed window, in memory). Exceeded → 429 + `Retry-After`.

**3g. Upstream back-off.** Spotify's limit is per real app (rolling 30 s) and shared by every virtual app. On any
upstream 429 we remember `Retry-After` (capped 24 h) and answer subsequent proxy calls locally with 429 + remaining
`Retry-After` instead of extending the penalty. Shown on the dashboard "Spotify app" page.

## 4. Data model (SQLite, `DATA_DIR/spotify-proxy.db`, WAL, foreign keys on)

**4a. Tables.**
- `settings(key,value)`: `upstream_client_id` (plain), `upstream_client_secret` (sealed).
- `apps(id, name, client_id UNIQUE, secret_enc, redirect_uris JSON, allowed_scopes, rate_limit, enabled, created_at)`.
  Client ID = 32 hex, secret = 32 hex (Spotify's own format).
- `connections(id, app_id→apps, user_id, display_name, scope, refresh_enc, access_enc, access_exp, created_at, last_used, UNIQUE(app_id,user_id))`.
- `pending(id, app_id, redirect_uri, state, scope, challenge, verifier_enc, expires)`: in-flight `/authorize`.
- `codes(hash, app_id, connection_id, redirect_uri, challenge, expires)`: our auth codes (hash only).
- `tokens(hash, kind access|refresh, app_id, connection_id NULL for client_credentials, scope, expires)`: our tokens (hash only; `expires=0` = no expiry).
- `requests(id, ts, app_id, user_id, method, path, status, ms)`: path only, no query string, no bodies.

**4b. Deletion cascade.** Deleting a connection deletes its codes and tokens (instant revocation). Deleting an app
deletes everything of that app except `requests` rows.

**4c. Encryption.** Sealed columns: upstream access/refresh tokens, app client secrets (sealed, not hashed, so the
dashboard can reveal them like Spotify does), the upstream client secret, PKCE verifiers. AES-256-GCM, random nonce
prepended, base64url. Key = SHA-256(`SECRET_KEY`). Our own bearer tokens/codes are high-entropy random and stored as
SHA-256 hashes only.

**4d. Retention.** Janitor (hourly) deletes expired pending/codes/tokens and request logs older than 7 days.

## 5. Routing and auth

**5a. Public paths (no Traefik auth; protected by the app's own client auth/tokens).**
`GET /authorize`, `GET <UPSTREAM_CALLBACK_PATH>` (default `/callback`; Spotify's browser redirect lands here, so it
cannot sit behind Tinyauth), `POST|OPTIONS /api/token`, `ANY /v1/*`, `GET /healthz`, `GET /` (landing page),
`GET /assets/*`, `GET /favicon.svg`.

**5b. Protected paths.** Everything under `/dashboard` (the SPA shell at `/dashboard…` and the JSON API at
`/dashboard/api/*`). The static JS/CSS bundle in `/assets` is public but contains no secrets.

**5c. Traefik.** Two routers on the same host (see `docker-compose.yml`): `spotify-public` (priority 10, no middleware)
and `spotify-dashboard` (priority 100, `PathPrefix(/dashboard)`, middleware `${TINYAUTH_MIDDLEWARE}`). The Tinyauth
middleware must forward the user header (`authResponseHeaders` including `Remote-User`).

**5d. Dashboard guard (defence in depth).** Even behind Traefik, `dash()` returns 403 unless the header named by
`DASHBOARD_USER_HEADER` is non-empty, and, if `DASHBOARD_ALLOWED_USERS` is set, unless the user is listed. The app
container must **not** publish ports, otherwise that header could be forged. Traefik strips client-supplied copies of
headers listed in `authResponseHeaders` before copying the real ones.

**5e. CSRF on the dashboard API.** Mutating requests must have a same-host `Origin` (when present) and
`Content-Type: application/json` (forces a CORS preflight cross-site).

**5f. Client auth summary.** Authorize: client_id + exact redirect URI. Token: secret (Basic or body) and/or PKCE
verifier. API: bearer token. Redirect URIs for virtual apps: `https`, `http` only for loopback/localhost, or custom
schemes; `javascript:`, `data:` etc. and fragments are rejected.

## 6. Configuration

**6a. Environment variables.**
| Var | Default | Notes |
|---|---|---|
| `SECRET_KEY` | required, ≥32 chars | encryption key source (4c) |
| `PUBLIC_URL` | `https://spotify.matthiaz.dev` | no trailing slash; used for redirect URI, rewriting, Secure cookie |
| `LISTEN` | `:8080` | |
| `DATA_DIR` | `./data` (`/data` in image) | SQLite location, volume |
| `UPSTREAM_CALLBACK_PATH` | `/callback` | registered at Spotify as `PUBLIC_URL`+path |
| `DASHBOARD_USER_HEADER` | `Remote-User` | header set by Tinyauth |
| `DASHBOARD_ALLOWED_USERS` | empty | comma list |
| `SPOTIFY_CLIENT_ID` / `SPOTIFY_CLIENT_SECRET` | empty | only seed the DB when no client id is saved yet |
| `SPOTIFY_ACCOUNTS_URL` / `SPOTIFY_API_URL` | real Spotify | overridden by tests |
Compose-only vars: `DOMAIN`, `TRAEFIK_NETWORK`, `TRAEFIK_ENTRYPOINT`, `TRAEFIK_CERTRESOLVER`, `TINYAUTH_MIDDLEWARE`.

**6b. Runtime settings.** Upstream client ID/secret are edited in the dashboard and stored in `settings`.
Changing the client ID deletes all connections (their refresh tokens belong to the old Spotify client).

**6c. Key management.** Losing/changing `SECRET_KEY` makes sealed data unreadable: users must log in again and app
secrets must be rotated. No key rotation tooling exists.

## 7. Dashboard (frontend)

**7a. Stack.** React 19 + TypeScript + Vite + Tailwind v4 + shadcn/ui (new-york, zinc), components in
`web/src/components/ui` (added with `npx shadcn@latest add …`; the CLI wrote `from "cn"` imports that had to be
changed to `@/lib/utils`). **Dark only**: `<html class="dark">`, the CSS defines only dark tokens, no theme toggle,
Sonner forced to dark.

**7b. Pages.** Landing (`/`, public, button → `/dashboard`, which triggers Tinyauth and returns). Dashboard
(`/dashboard#/…`, hash routing): *Apps* (list, create), *App detail* tabs (Credentials with reveal/rotate, Connect a
client snippets, Users with revoke/revoke-all, Settings incl. redirect URIs/scopes/rate limit/delete, Activity), *Usage*
(hourly chart, per-app table, recent requests), *Spotify app* (upstream settings + allowlist explanation).

**7c. Dev workflow.** `cd web && npm ci && npm run build` then `go run .` (needs `SECRET_KEY`; send `Remote-User`
header, e.g. with a browser extension or curl). `npm run dev` proxies `/dashboard/api` to :8080 but also needs that header.

**7d. Dashboard JSON API** (all under `/dashboard/api`): `GET /me`, `GET|PUT /upstream`, `GET|POST /apps`,
`GET|PATCH|DELETE /apps/{id}`, `POST /apps/{id}/rotate-secret`, `GET|DELETE /apps/{id}/connections`,
`DELETE /apps/{id}/connections/{cid}`, `GET /usage?hours=&app=`, `GET /requests?limit=&app=`.

## 8. Deployment

**8a. Compose.** `docker-compose.yml` runs the GHCR image (also buildable with `build: .`), joins the external Traefik
network, mounts volume `/data`, no published ports. Copy `.env.example` → `.env`.

**8b. Image / CI.** `Dockerfile` is 3-stage (node build → go build cross-compiled via `$BUILDPLATFORM`/`TARGETARCH` →
distroless nonroot, uid 65532, `/data` pre-owned). `.github/workflows/docker.yml`: job `test` (frontend build, `go vet`,
`go test -race`) then job `image` (buildx, linux/amd64+arm64, push to `ghcr.io/<repo>` on `main` and `v*` tags; PRs
build without pushing). Tags: `latest` (default branch), branch, semver, sha. The `HEALTHCHECK` runs
`/spotify-proxy healthcheck`.

**8c. Spotify side.** One real app; Redirect URI = `<PUBLIC_URL><UPSTREAM_CALLBACK_PATH>`; users allowlisted in
User Management (1d). Spotify's rule: https required except loopback IP literals; `localhost` not allowed there (our
virtual apps are more permissive, 5f, since Spotify never sees them).

**8d. Backups/upgrades.** Back up the `/data` volume and keep `SECRET_KEY` with it. The schema uses
`CREATE TABLE IF NOT EXISTS`; any future schema change needs an explicit migration step added in `db.go`.

## 9. Spotify research notes (as of 2026-10)

**9a. OAuth.** Authorization Code with PKCE (S256, verifier 43–128 chars) and Authorization Code with secret are
supported; implicit grant is gone. Token endpoint: `POST https://accounts.spotify.com/api/token`,
`application/x-www-form-urlencoded`; Basic auth for secret flows, `client_id` in body for PKCE. Refresh responses may
omit `refresh_token`; `invalid_grant` means start over. Refresh tokens last ~6 months from authorization and refreshing
does not extend that. Access tokens last 3600 s.

**9b. Dev mode (Feb/Mar 2026 changes).** Owner needs Premium; max 5 users per app (allowlist); limited client IDs per
developer; removed endpoints (batch gets, browse, artist top tracks, other users' profiles, `/markets`), `/tracks`
→ `/items` for playlists, library endpoints unified (`PUT|DELETE /me/library`), search `limit` max 10, many response
fields removed. All of this is upstream behaviour we proxy transparently; we do not filter or emulate anything.

**9c. Errors and limits.** Web API: `{"error":{"status","message"[,"reason"]}}`; auth endpoints: RFC 6749. 429 with
`Retry-After` (rolling 30 s window, shared per developer's dev-mode apps; `reason: QUOTA_EXCEEDED` possible).

## 10. Testing

**10a. E2E (`e2e_test.go`).** Starts a fake Spotify (authorize, token with PKCE check, `/v1/me`, paginated playlists,
client-credentials search, 429) and the real server, then exercises: dashboard guard, upstream config, app creation and
redirect URI validation, authorize validation, PKCE login, wrong verifier, single-use codes, `/v1` proxy, `next` link
rewriting, refresh, confidential flow with Basic auth, bad secret, client_credentials, connection upsert and revoke
(access + refresh stop working), secret rotation, allowlist-403 page, 429 passthrough and local back-off, usage logging.
Run: `go test -race ./...` (needs `web/dist` to exist; a `.gitkeep` is committed so `go:embed` compiles without a frontend build).

**10b. Not covered.** Real Spotify, Traefik/Tinyauth integration, UI (manually screenshot-checked with Playwright).

## 11. Gotchas

**11a.** Go 1.22 `ServeMux` panics on ambiguous patterns; the dashboard SPA uses `GET /dashboard/{rest...}` and the
unknown-API-route 404 lives inside `serveIndex`.
**11b.** Never run `pkill -f` with a pattern that matches your own shell command.
**11c.** Tokens in query strings of proxied requests are not logged (only path is stored).
**11d.** The consent screen users see belongs to the real Spotify app, not to the virtual app's name.
**11e.** Each virtual app has its own upstream grant per user: the same Spotify user connecting to two virtual apps
means two grants (and Spotify may show consent again if scopes differ). Scopes are therefore naturally isolated per app.
**11f.** Revoking a connection deletes our copy of the upstream refresh token; Spotify has no revoke endpoint, so the
grant stays visible at spotify.com/account/apps until the user removes it.
**11g.** Changing `PUBLIC_URL` or `UPSTREAM_CALLBACK_PATH` requires updating the Redirect URI at Spotify.
**11h.** No Docker daemon was available while building; the Dockerfile steps were verified by running them by hand
(`npm ci && npm run build`, `CGO_ENABLED=0 go build`, cross-compile arm64). CI is the first real image build.

## 12. Decisions made without asking (revisit if wrong)

**12a.** Fixed redirect path `/callback` (configurable via `UPSTREAM_CALLBACK_PATH`).
**12b.** Per-(app, user) upstream grants rather than one shared grant per user (11e).
**12c.** Opaque random tokens (hash-stored) instead of JWTs; access TTL 3600 s like Spotify; refresh tokens non-expiring until revoked.
**12d.** Client secrets stored encrypted (revealable), rotation invalidates the old secret immediately with no grace period.
**12e.** "API keys": not implemented separately; the client secret + `client_credentials` grant cover server-to-server use.
Add per-app API keys only if a concrete need appears.
**12f.** Virtual-app redirect URIs allow `https`, loopback/localhost `http`, and custom schemes.
**12g.** Dashboard authorization = whoever Tinyauth lets through (optionally narrowed by `DASHBOARD_ALLOWED_USERS`).
**12h.** Go + SQLite + React/shadcn; image published to GHCR as `ghcr.io/matthiaslubbertsen/spotify`.
