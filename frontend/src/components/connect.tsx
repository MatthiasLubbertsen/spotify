import { CodeBlock } from "@/components/common"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import type { App, Config } from "@/lib/api"

export function Connect({ app, cfg }: { app: App; cfg: Config }) {
  const redirect = app.redirect_uris[0] ?? "http://127.0.0.1:8000/callback"
  const id = app.id
  const snippets: Record<string, string> = {
    Environment: `SPOTIFY_CLIENT_ID=${id}
SPOTIFY_CLIENT_SECRET=<client_secret>
SPOTIFY_REDIRECT_URI=${redirect}

# Swap Spotify's base URLs for these:
SPOTIFY_AUTHORIZE_URL=${cfg.authorize_url}   # was https://accounts.spotify.com/authorize
SPOTIFY_TOKEN_URL=${cfg.token_url}           # was https://accounts.spotify.com/api/token
SPOTIFY_API_BASE=${cfg.api_base}             # was https://api.spotify.com/v1`,
    curl: `# 1. Open in a browser, sign in, copy ?code=... from the redirect
${cfg.authorize_url}?client_id=${id}&response_type=code&redirect_uri=${encodeURIComponent(redirect)}&scope=user-read-private

# 2. Exchange the code
curl -s -u ${id}:<client_secret> ${cfg.token_url} \\
  -d grant_type=authorization_code -d code=<code> -d redirect_uri=${redirect}

# 3. Call the API
curl -s -H "Authorization: Bearer <access_token>" ${cfg.api_base}/me${app.allow_client_credentials ? `

# App-only token (client credentials)
curl -s -u ${id}:<client_secret> ${cfg.token_url} -d grant_type=client_credentials` : ""}`,
    Python: `import spotipy
from spotipy.oauth2 import SpotifyOAuth

SpotifyOAuth.OAUTH_AUTHORIZE_URL = "${cfg.authorize_url}"
SpotifyOAuth.OAUTH_TOKEN_URL = "${cfg.token_url}"

sp = spotipy.Spotify(auth_manager=SpotifyOAuth(
    client_id="${id}",
    client_secret="<client_secret>",
    redirect_uri="${redirect}",
    scope="user-read-private",
))
sp.prefix = "${cfg.api_base}/"

print(sp.current_user())`,
    JavaScript: `const AUTHORIZE = "${cfg.authorize_url}"
const TOKEN = "${cfg.token_url}"
const API = "${cfg.api_base}"

// After the user returns to your redirect URI with ?code=...
const res = await fetch(TOKEN, {
  method: "POST",
  headers: {
    "Content-Type": "application/x-www-form-urlencoded",
    Authorization: "Basic " + btoa("${id}:<client_secret>"),
  },
  body: new URLSearchParams({ grant_type: "authorization_code", code, redirect_uri: "${redirect}" }),
})
const { access_token } = await res.json()

const me = await fetch(API + "/me", { headers: { Authorization: "Bearer " + access_token } })
console.log(await me.json())`,
  }
  return (
    <Tabs defaultValue="Environment">
      <TabsList>
        {Object.keys(snippets).map((k) => <TabsTrigger key={k} value={k}>{k}</TabsTrigger>)}
      </TabsList>
      {Object.entries(snippets).map(([k, v]) => (
        <TabsContent key={k} value={k}><CodeBlock code={v} /></TabsContent>
      ))}
    </Tabs>
  )
}
