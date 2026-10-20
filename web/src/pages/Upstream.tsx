import { useEffect, useState } from "react"
import { toast } from "sonner"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { TriangleAlert } from "lucide-react"
import { api, type Me, type Upstream } from "../api"
import { Field } from "../components/bits"

export function UpstreamPage({ me }: { me: Me }) {
  const [u, setU] = useState<Upstream | null>(null)
  const [clientId, setClientId] = useState("")
  const [secret, setSecret] = useState("")
  useEffect(() => { api<Upstream>("/upstream").then((x) => { setU(x); setClientId(x.clientId) }) }, [])

  async function save(clearSecret = false) {
    if (u?.configured && clientId !== u.clientId && u.connections > 0 &&
      !confirm(`Changing the client ID signs out all ${u.connections} connected user(s), because their Spotify tokens belong to the old app. Continue?`)) return
    try {
      const x = await api<Upstream>("/upstream", "PUT", { clientId, clientSecret: secret, clearSecret })
      setU(x); setSecret(""); toast.success("Saved")
    } catch (e) { toast.error((e as Error).message) }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <h1 className="text-xl font-semibold">Spotify app (upstream)</h1>
        {u && <Badge variant={u.configured ? "secondary" : "destructive"}>{u.configured ? "configured" : "not configured"}</Badge>}
        {!!u?.blockedSeconds && <Badge variant="destructive">Spotify rate limit: {u.blockedSeconds}s left</Badge>}
      </div>

      <Alert>
        <TriangleAlert className="size-4" />
        <AlertTitle>Every end user must be on Spotify's allowlist</AlertTitle>
        <AlertDescription>
          <p>
            Virtual apps are our own layer; Spotify only knows the one real app. In development mode Spotify rejects every
            user who is not added under <b>Developer Dashboard → your app → User Management</b> (name and email of their
            Spotify account), with a 403, and allows at most 5 users per app. The app owner also needs Spotify Premium.
            This cannot be bypassed here. Users who are not on the list see a "Not allowed by Spotify" page after signing in.
          </p>
        </AlertDescription>
      </Alert>

      <Card>
        <CardHeader>
          <CardTitle>1. Redirect URI</CardTitle>
          <CardDescription>Add exactly this URI to the real Spotify app. It is the only redirect URI Spotify ever sees; all virtual-app redirect URIs are handled by this server.</CardDescription>
        </CardHeader>
        <CardContent><Field label="Redirect URI" value={me.callbackUrl} /></CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>2. Credentials of the real Spotify app</CardTitle>
          <CardDescription>
            The client ID is enough for user logins (PKCE). The client secret is only needed to let virtual apps use the
            client credentials grant (public catalog data without a user). It is encrypted at rest.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2"><Label htmlFor="cid">Client ID</Label>
            <Input id="cid" className="font-mono" value={clientId} onChange={(e) => setClientId(e.target.value)} placeholder="from developer.spotify.com/dashboard" /></div>
          <div className="space-y-2"><Label htmlFor="sec">Client secret (optional)</Label>
            <Input id="sec" type="password" className="font-mono" value={secret} onChange={(e) => setSecret(e.target.value)}
              placeholder={u?.hasSecret ? "•••••••• saved — type to replace" : "not set"} autoComplete="off" /></div>
          <div className="flex gap-2">
            <Button onClick={() => save()} disabled={!clientId.trim()}>Save</Button>
            {u?.hasSecret && <Button variant="outline" onClick={() => save(true)}>Remove secret</Button>}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>3. Status</CardTitle></CardHeader>
        <CardContent className="text-sm text-muted-foreground space-y-1">
          <p>Connected user grants across all virtual apps: <b className="text-foreground">{u?.connections ?? "…"}</b></p>
          <p>Spotify refresh tokens are not revocable via API: revoking a user here deletes our copy; the user can also remove access at spotify.com/account/apps.</p>
        </CardContent>
      </Card>
    </div>
  )
}
