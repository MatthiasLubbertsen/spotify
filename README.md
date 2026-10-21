# spotis

Self-hosted wrapper around the Spotify Web API and OAuth. It registers as your **one** real Spotify
app (single fixed redirect URI) and lets you create as many **virtual apps** as you like, each with its own
client ID, secret and redirect URIs. Clients keep using normal Spotify libraries; you only swap the
base URLs:

| Spotify | spotis |
|---|---|
| `https://accounts.spotify.com/authorize` | `https://spotify.matthiaz.dev/authorize` |
| `https://accounts.spotify.com/api/token` | `https://spotify.matthiaz.dev/api/token` |
| `https://api.spotify.com/v1` | `https://spotify.matthiaz.dev/v1` |

> Spotify's allowlist still applies: every end user must be added under *Users management* of the real app in the
> Spotify Developer Portal.

## Run

1. In the Spotify Developer Portal register the redirect URI `https://spotify.matthiaz.dev/oauth/callback`.
2. `docker compose up -d` (see `docker-compose.yml`; expects Traefik's `web` entrypoint on the `proxy` network behind
   Nest's TLS proxy, plus a Tinyauth `tinyauth` middleware that forwards `Remote-User` and allows `spotify`).
3. Open the site, press **Log in**, then **Upstream** and enter the real client ID/secret.
4. Create an app under **Apps**; the **Connect** tab shows how to point a client at it.

Only `/dashboard*` sits behind Tinyauth; `/authorize`, `/oauth/callback`, `/api/token` and `/v1/*` are public and
protected by each virtual app's own credentials and tokens.

## Develop

```sh
make test      # vet + race tests, includes the full-flow test (internal/server/flow_test.go)
make web       # build the dashboard into internal/webui/dist
make run       # http://localhost:8080 (send a Remote-User header for /dashboard)
```

## Now playing (API key, no OAuth)

Dashboard → **Users** → *Now playing* creates a key for one connected user (their login must include the
`user-read-currently-playing` scope). Then poll:

```sh
curl -H "X-API-Key: spk_..." https://spotify.matthiaz.dev/now-playing
# {"is_playing":true,"progress_ms":83000,"item":{"name":"Midnight City","artists":["M83"],"album":"...","image":"...","duration_ms":243000,...}}
```

Design notes and decisions live in [AGENTS.md](AGENTS.md). Images are built by GitHub Actions and published to GHCR.
