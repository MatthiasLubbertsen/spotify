import { ShieldAlert } from "lucide-react"
import { useEffect, useState } from "react"
import { toast } from "sonner"
import { api } from "@/lib/api"
import { CopyField, PageHeader } from "@/components/common"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { useAction, useConfig, useUpstream } from "@/lib/hooks"

export default function Upstream() {
  const up = useUpstream()
  const cfg = useConfig()
  const [id, setId] = useState("")
  const [secret, setSecret] = useState("")
  useEffect(() => { if (up.data) setId(up.data.client_id) }, [up.data])
  const save = useAction(
    () => api("/upstream", { method: "PUT", json: { client_id: id, client_secret: secret } }),
    ["upstream", "config"], "Saved",
  )
  const test = useAction(() => api<{ ok: boolean; error?: string }>("/upstream/test", { method: "POST" }), [])
  return (
    <>
      <PageHeader title="Upstream" description="The one real Spotify app everything runs through." />
      <div className="space-y-4">
        <Alert>
          <ShieldAlert className="size-4" />
          <AlertTitle>Every user must be allowlisted at Spotify</AlertTitle>
          <AlertDescription>
            <p>
              Add each person under <b>Users management</b> of the real app in the{" "}
              <a className="underline" href="https://developer.spotify.com/dashboard" target="_blank" rel="noreferrer">Spotify Developer dashboard</a>.
              In development mode that is at most 5 users, and the app owner needs Premium. spotis cannot bypass this:
              anyone not on the list gets an access-denied page when they sign in.
            </p>
          </AlertDescription>
        </Alert>
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Redirect URI</CardTitle>
            <CardDescription>Register exactly this one URI on the real Spotify app.</CardDescription>
          </CardHeader>
          <CardContent>{cfg.data ? <CopyField value={cfg.data.redirect_uri} /> : <Skeleton className="h-8" />}</CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              Credentials {up.data?.configured && <Badge variant="secondary">configured</Badge>}
            </CardTitle>
            <CardDescription>From the Spotify Developer dashboard, app settings.</CardDescription>
          </CardHeader>
          <CardContent>
            {up.isLoading ? <Skeleton className="h-32" /> : (
              <form className="max-w-md space-y-4" onSubmit={(e) => { e.preventDefault(); save.mutate(undefined, { onSuccess: () => setSecret("") }) }}>
                <div className="space-y-2">
                  <Label htmlFor="cid">Client ID</Label>
                  <Input id="cid" value={id} onChange={(e) => setId(e.target.value)} className="font-mono" autoComplete="off" />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="sec">Client secret</Label>
                  <Input id="sec" type="password" value={secret} onChange={(e) => setSecret(e.target.value)} autoComplete="new-password"
                    placeholder={up.data?.has_secret ? "•••••••• (set, leave empty to keep)" : "Paste the client secret"} />
                </div>
                <div className="flex gap-2">
                  <Button type="submit" disabled={!id.trim() || save.isPending}>Save</Button>
                  <Button type="button" variant="outline" disabled={test.isPending}
                    onClick={() => test.mutate(undefined, {
                      onSuccess: (r) => (r.ok ? toast.success("Spotify accepted the credentials") : toast.error(r.error ?? "Test failed")),
                    })}>Test credentials</Button>
                </div>
              </form>
            )}
          </CardContent>
        </Card>
      </div>
    </>
  )
}
