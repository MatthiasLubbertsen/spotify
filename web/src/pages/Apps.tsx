import { useEffect, useState } from "react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import { Eye, EyeOff, Plus } from "lucide-react"
import { api, fmtTime, type App, type Connection, type Me } from "../api"
import { CodeBlock, Field } from "../components/bits"
import { go } from "../Dashboard"
import { RequestsTable } from "./Usage"

const lines = (s: string) => s.split("\n").map((x) => x.trim()).filter(Boolean)

export function AppsPage() {
  const [apps, setApps] = useState<App[] | null>(null)
  const [open, setOpen] = useState(false)
  const [name, setName] = useState("")
  const [uris, setUris] = useState("")
  const load = () => api<App[]>("/apps").then(setApps)
  useEffect(() => { load() }, [])

  async function create() {
    try {
      const a = await api<App>("/apps", "POST", { name, redirectUris: lines(uris) })
      toast.success("App created")
      go("/apps/" + a.id)
    } catch (e) { toast.error((e as Error).message) }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center">
        <h1 className="text-xl font-semibold mr-auto">Virtual apps</h1>
        <Button onClick={() => setOpen(true)}><Plus className="size-4" />New app</Button>
      </div>
      {apps && apps.length === 0 && (
        <Card><CardHeader><CardTitle>No apps yet</CardTitle>
          <CardDescription>Create a virtual app to get a client ID and secret, then point any Spotify client library at this server. First make sure the real Spotify app is set up under “Spotify app”.</CardDescription></CardHeader></Card>
      )}
      <div className="grid gap-4 sm:grid-cols-2">
        {apps?.map((a) => (
          <Card key={a.id} className="cursor-pointer hover:bg-accent/40 transition-colors" onClick={() => go("/apps/" + a.id)}>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">{a.name}{!a.enabled && <Badge variant="destructive">disabled</Badge>}</CardTitle>
              <CardDescription className="font-mono text-xs truncate">{a.clientId}</CardDescription>
            </CardHeader>
            <CardContent className="flex gap-6 text-sm text-muted-foreground">
              <span><b className="text-foreground">{a.connections}</b> users</span>
              <span><b className="text-foreground">{a.requests24h}</b> requests / 24h</span>
              {a.errors24h > 0 && <span><b className="text-amber-400">{a.errors24h}</b> errors</span>}
            </CardContent>
          </Card>
        ))}
      </div>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader><DialogTitle>New virtual app</DialogTitle>
            <DialogDescription>You can change everything later. A client ID and secret are generated for you.</DialogDescription></DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2"><Label htmlFor="n">Name</Label><Input id="n" value={name} onChange={(e) => setName(e.target.value)} /></div>
            <div className="space-y-2"><Label htmlFor="r">Redirect URIs (one per line)</Label>
              <Textarea id="r" className="font-mono text-sm" rows={3} value={uris} onChange={(e) => setUris(e.target.value)} placeholder="https://myapp.example/callback" /></div>
          </div>
          <DialogFooter><Button onClick={create} disabled={!name.trim()}>Create</Button></DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

export function AppDetail({ id, me }: { id: string; me: Me }) {
  const [app, setApp] = useState<App | null>(null)
  const [missing, setMissing] = useState(false)
  useEffect(() => { api<App>("/apps/" + id).then(setApp).catch(() => setMissing(true)) }, [id])
  if (missing) return <p className="text-muted-foreground">App not found. <Button variant="link" onClick={() => go("/apps")}>Back</Button></p>
  if (!app) return <p className="text-muted-foreground">Loading…</p>

  async function toggle(enabled: boolean) {
    setApp(await api<App>("/apps/" + id, "PATCH", { enabled }))
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <Button variant="ghost" size="sm" onClick={() => go("/apps")}>← Apps</Button>
        <h1 className="text-xl font-semibold mr-auto">{app.name}</h1>
        <Label htmlFor="en" className="text-muted-foreground">Enabled</Label>
        <Switch id="en" checked={app.enabled} onCheckedChange={toggle} />
      </div>
      <Tabs defaultValue="credentials">
        <TabsList className="flex-wrap h-auto">
          <TabsTrigger value="credentials">Credentials</TabsTrigger>
          <TabsTrigger value="connect">Connect a client</TabsTrigger>
          <TabsTrigger value="users">Users ({app.connections})</TabsTrigger>
          <TabsTrigger value="settings">Settings</TabsTrigger>
          <TabsTrigger value="activity">Activity</TabsTrigger>
        </TabsList>
        <TabsContent value="credentials"><Credentials app={app} me={me} onChange={setApp} /></TabsContent>
        <TabsContent value="connect"><Snippets app={app} me={me} /></TabsContent>
        <TabsContent value="users"><Users app={app} onChange={() => api<App>("/apps/" + id).then(setApp)} /></TabsContent>
        <TabsContent value="settings"><Settings app={app} onChange={setApp} /></TabsContent>
        <TabsContent value="activity"><Card><CardContent><RequestsTable appId={app.id} showApp={false} /></CardContent></Card></TabsContent>
      </Tabs>
    </div>
  )
}

function Credentials({ app, me, onChange }: { app: App; me: Me; onChange: (a: App) => void }) {
  const [show, setShow] = useState(false)
  const [confirm, setConfirm] = useState(false)
  async function rotate() {
    try { onChange(await api<App>(`/apps/${app.id}/rotate-secret`, "POST")); setShow(true); setConfirm(false); toast.success("Secret rotated") }
    catch (e) { toast.error((e as Error).message) }
  }
  return (
    <Card>
      <CardHeader><CardTitle>Credentials</CardTitle>
        <CardDescription>Use these in place of the ones from the Spotify Developer Dashboard.</CardDescription></CardHeader>
      <CardContent className="space-y-4">
        <Field label="Client ID" value={app.clientId} />
        <Field label="Client secret" value={show ? app.clientSecret ?? "" : "•".repeat(32)}
          extra={<Button type="button" variant="outline" size="sm" onClick={() => setShow(!show)}>{show ? <EyeOff className="size-4" /> : <Eye className="size-4" />}</Button>} />
        <div><Button variant="outline" onClick={() => setConfirm(true)}>Rotate secret</Button></div>
        <div className="border-t pt-4 space-y-4">
          <Field label="Authorize URL" value={me.authorizeUrl} />
          <Field label="Token URL" value={me.tokenUrl} />
          <Field label="Web API base URL" value={me.apiUrl} />
        </div>
      </CardContent>
      <Dialog open={confirm} onOpenChange={setConfirm}>
        <DialogContent>
          <DialogHeader><DialogTitle>Rotate client secret?</DialogTitle>
            <DialogDescription>The old secret stops working immediately. Existing user tokens keep working; clients that use the secret must be updated.</DialogDescription></DialogHeader>
          <DialogFooter><Button variant="destructive" onClick={rotate}>Rotate</Button></DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  )
}

function Snippets({ app, me }: { app: App; me: Me }) {
  const redirect = app.redirectUris[0] ?? "https://myapp.example/callback"
  const py = `# pip install spotipy
import spotipy
from spotipy.oauth2 import SpotifyOAuth

auth = SpotifyOAuth(
    client_id="${app.clientId}",
    client_secret="<client secret>",
    redirect_uri="${redirect}",
    scope="user-read-private",
)
# the only change versus normal Spotify usage: swap the base URLs
auth.OAUTH_AUTHORIZE_URL = "${me.authorizeUrl}"
auth.OAUTH_TOKEN_URL = "${me.tokenUrl}"

sp = spotipy.Spotify(auth_manager=auth)
sp.prefix = "${me.apiUrl}/"
print(sp.me())`
  const curl = `# 1. Open in a browser (add code_challenge + code_challenge_method=S256 for PKCE instead of a secret)
${me.authorizeUrl}?client_id=${app.clientId}&response_type=code&redirect_uri=${encodeURIComponent(redirect)}&scope=user-read-private&state=xyz

# 2. Exchange the code (secret flow; for PKCE send client_id + code_verifier instead of -u)
curl -u ${app.clientId}:<client secret> \\
  -d grant_type=authorization_code -d code=<code> -d redirect_uri=${redirect} \\
  ${me.tokenUrl}

# 3. Call the API
curl -H "Authorization: Bearer <access_token>" ${me.apiUrl}/me

# Refresh
curl -u ${app.clientId}:<client secret> -d grant_type=refresh_token -d refresh_token=<refresh_token> ${me.tokenUrl}

# No user needed (catalog data) - needs the upstream client secret to be set
curl -u ${app.clientId}:<client secret> -d grant_type=client_credentials ${me.tokenUrl}`
  const generic = `Any Spotify client library: change three URLs, keep everything else.

  https://accounts.spotify.com/authorize   ->  ${me.authorizeUrl}
  https://accounts.spotify.com/api/token   ->  ${me.tokenUrl}
  https://api.spotify.com/v1               ->  ${me.apiUrl}

client_id:     ${app.clientId}
client_secret: <client secret>   (not needed with PKCE)
redirect_uri:  one of ${app.redirectUris.length ? app.redirectUris.join(", ") : "(add one under Settings)"}

Notes
- Pagination "next"/"href" links are rewritten to this server, so they keep working.
- Every user who logs in must also be on the real Spotify app's allowlist.`
  return (
    <Card>
      <CardHeader><CardTitle>Point a client at this app</CardTitle>
        <CardDescription>Reveal the client secret on the Credentials tab and paste it where shown.</CardDescription></CardHeader>
      <CardContent>
        <Tabs defaultValue="generic">
          <TabsList><TabsTrigger value="generic">Any library</TabsTrigger><TabsTrigger value="py">Python (spotipy)</TabsTrigger><TabsTrigger value="curl">curl</TabsTrigger></TabsList>
          <TabsContent value="generic"><CodeBlock code={generic} /></TabsContent>
          <TabsContent value="py"><CodeBlock code={py} /></TabsContent>
          <TabsContent value="curl"><CodeBlock code={curl} /></TabsContent>
        </Tabs>
      </CardContent>
    </Card>
  )
}

function Users({ app, onChange }: { app: App; onChange: () => void }) {
  const [rows, setRows] = useState<Connection[] | null>(null)
  const load = () => api<Connection[]>(`/apps/${app.id}/connections`).then(setRows)
  useEffect(() => { load() }, [app.id])
  async function revoke(id?: string) {
    if (!confirm(id ? "Revoke this user? Their tokens stop working immediately." : "Revoke ALL users of this app?")) return
    await api(`/apps/${app.id}/connections${id ? "/" + id : ""}`, "DELETE")
    toast.success("Revoked"); load(); onChange()
  }
  return (
    <Card>
      <CardHeader className="flex flex-row items-center">
        <div className="mr-auto"><CardTitle>Connected users</CardTitle>
          <CardDescription>Spotify accounts that logged in through this app.</CardDescription></div>
        {!!rows?.length && <Button variant="destructive" size="sm" onClick={() => revoke()}>Revoke all</Button>}
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader><TableRow><TableHead>User</TableHead><TableHead>Scopes</TableHead><TableHead>Connected</TableHead><TableHead>Last used</TableHead><TableHead /></TableRow></TableHeader>
          <TableBody>
            {rows?.map((c) => (
              <TableRow key={c.id}>
                <TableCell><div>{c.displayName || c.userId}</div><div className="font-mono text-xs text-muted-foreground">{c.userId}</div></TableCell>
                <TableCell className="max-w-56 whitespace-normal text-xs text-muted-foreground">{c.scope || "—"}</TableCell>
                <TableCell className="whitespace-nowrap text-muted-foreground">{fmtTime(c.createdAt)}</TableCell>
                <TableCell className="whitespace-nowrap text-muted-foreground">{fmtTime(c.lastUsed)}</TableCell>
                <TableCell className="text-right"><Button size="sm" variant="outline" onClick={() => revoke(c.id)}>Revoke</Button></TableCell>
              </TableRow>
            ))}
            {rows && rows.length === 0 && <TableRow><TableCell colSpan={5} className="text-center text-muted-foreground">Nobody has connected yet.</TableCell></TableRow>}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}

function Settings({ app, onChange }: { app: App; onChange: (a: App) => void }) {
  const [name, setName] = useState(app.name)
  const [uris, setUris] = useState(app.redirectUris.join("\n"))
  const [scopes, setScopes] = useState(app.allowedScopes)
  const [rate, setRate] = useState(String(app.rateLimit))
  async function save() {
    try {
      onChange(await api<App>("/apps/" + app.id, "PATCH", { name, redirectUris: lines(uris), allowedScopes: scopes, rateLimit: parseInt(rate) || 0 }))
      toast.success("Saved")
    } catch (e) { toast.error((e as Error).message) }
  }
  async function del() {
    if (!confirm(`Delete "${app.name}" and revoke all of its users? This cannot be undone.`)) return
    await api("/apps/" + app.id, "DELETE"); go("/apps")
  }
  return (
    <div className="space-y-6">
      <Card>
        <CardHeader><CardTitle>Settings</CardTitle></CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2"><Label htmlFor="sn">Name</Label><Input id="sn" value={name} onChange={(e) => setName(e.target.value)} /></div>
          <div className="space-y-2"><Label htmlFor="sr">Redirect URIs (one per line, exact match)</Label>
            <Textarea id="sr" className="font-mono text-sm" rows={4} value={uris} onChange={(e) => setUris(e.target.value)} />
            <p className="text-xs text-muted-foreground">https, loopback http (127.0.0.1, localhost) or custom schemes like myapp://callback.</p></div>
          <div className="space-y-2"><Label htmlFor="ss">Allowed scopes (space separated, empty = any)</Label>
            <Input id="ss" className="font-mono" value={scopes} onChange={(e) => setScopes(e.target.value)} placeholder="user-read-private user-read-email" /></div>
          <div className="space-y-2"><Label htmlFor="rl">Rate limit (requests per minute, 0 = unlimited)</Label>
            <Input id="rl" type="number" min={0} className="w-40" value={rate} onChange={(e) => setRate(e.target.value)} />
            <p className="text-xs text-muted-foreground">Spotify's own limit is shared by all virtual apps; use this to stop one app from starving the others.</p></div>
          <Button onClick={save}>Save</Button>
        </CardContent>
      </Card>
      <Card>
        <CardHeader><CardTitle>Danger zone</CardTitle></CardHeader>
        <CardContent><Button variant="destructive" onClick={del}>Delete app</Button></CardContent>
      </Card>
    </div>
  )
}
