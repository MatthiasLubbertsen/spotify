# Spotify Proxy

Self-hosted wrapper around Spotify's OAuth and Web API. You register **one** real Spotify app; this server
then lets you create as many **virtual apps** as you like. Each has its own client ID, secret and redirect URIs, and
speaks Spotify's own protocol (`/authorize`, `/api/token`, `/v1/*`), so a normal Spotify client library only needs its
three base URLs swapped.

Full internals: [AGENTS.md](AGENTS.md).

## 1. Set up the real Spotify app

1. Open the [Spotify Developer Dashboard](https://developer.spotify.com/dashboard) and create your one app.
2. Add this **Redirect URI** (exactly): `https://spotify.matthiaz.dev/callback`
3. Copy the **Client ID** (and the **Client secret** if you want virtual apps to use the client credentials grant).
4. **Allowlist users.** Spotify apps in development mode only work for users listed under *User Management*
   (name + Spotify account email), max 5, and the app owner needs Premium. Every person who will ever log in through
   any virtual app must be added there. This is enforced by Spotify and cannot be bypassed by this server; users not on
   the list see a "Not allowed by Spotify" page.

## 2. Run it

```sh
cp .env.example .env
# set SECRET_KEY (openssl rand -base64 32); adjust DOMAIN / PUBLIC_URL if not spotify.matthiaz.dev
docker compose up -d
```

Needs an external Traefik network and a Tinyauth forward-auth middleware (`tinyauth@docker` by default, change
`TINYAUTH_MIDDLEWARE`). Images are built by GitHub Actions and published to `ghcr.io/matthiaslubbertsen/spotify`
(`docker compose build` also works locally).

## 3. First run

1. Open `https://spotify.matthiaz.dev/` and click **Log in to the dashboard** (Tinyauth login).
2. **Spotify app** page: paste the real app's Client ID (and secret, optional) and confirm the Redirect URI.
3. **Apps → New app**: name it, add its redirect URIs. Copy the client ID/secret.
4. Open **Connect a client** for a copy-paste snippet. In short:

| Spotify default | Replace with |
|---|---|
| `https://accounts.spotify.com/authorize` | `https://spotify.matthiaz.dev/authorize` |
| `https://accounts.spotify.com/api/token` | `https://spotify.matthiaz.dev/api/token` |
| `https://api.spotify.com/v1` | `https://spotify.matthiaz.dev/v1` |

## Test

```sh
cd web && npm ci && npm run build && cd ..   # the Go binary embeds the built frontend
go test -race ./...                          # end-to-end test against a fake Spotify
```
