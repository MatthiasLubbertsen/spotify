import { useEffect, useState } from "react"
import { Link, useNavigate, useParams } from "react-router-dom"
import { api, type App, type Secret } from "@/lib/api"
import { AppFields, emptyDraft, type AppDraft } from "@/components/app-form"
import { Confirm, CopyField, PageHeader, SecretDialog } from "@/components/common"
import { Connect } from "@/components/connect"
import { GrantsTable } from "@/components/grants-table"
import { StatCards, RequestsTable } from "@/components/widgets"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useApp, useAction, useConfig, useGrants, useRequests, useUsage } from "@/lib/hooks"
import { relTime } from "@/lib/format"

export default function AppDetail() {
  const { id = "" } = useParams()
  const nav = useNavigate()
  const app = useApp(id)
  const cfg = useConfig()
  const usage = useUsage("24h", id)
  const reqs = useRequests({ app: id, limit: 15 })
  const grants = useGrants(id)
  const [secret, setSecret] = useState<string | null>(null)
  const patch = useAction((p: Partial<AppDraft> & { enabled?: boolean }) => api<App>(`/apps/${id}`, { method: "PATCH", json: p }), ["apps"], "Saved")
  const rotate = useAction(() => api<Secret>(`/apps/${id}/rotate-secret`, { method: "POST" }), ["apps"])
  const del = useAction(() => api(`/apps/${id}`, { method: "DELETE" }), ["apps", "grants"], "App deleted")
  const [draft, setDraft] = useState<AppDraft | null>(null)
  useEffect(() => { if (app.data) setDraft(emptyDraft(app.data)) }, [app.data])

  if (app.isError) return <p className="text-muted-foreground">App not found. <Link to="/dashboard/apps" className="underline">Back to apps</Link></p>
  const a = app.data
  if (!a || !cfg.data || !draft) return <Skeleton className="h-64 w-full" />
  return (
    <>
      <PageHeader title={a.name}
        description={`Created ${relTime(a.created_at)}`}
        actions={
          <>
            {!a.enabled && <Badge variant="secondary">disabled</Badge>}
            <div className="flex items-center gap-2">
              <Switch id="en" checked={a.enabled} onCheckedChange={(enabled) => patch.mutate({ enabled })} />
              <Label htmlFor="en">Enabled</Label>
            </div>
          </>
        } />
      <Tabs defaultValue="overview">
        <TabsList className="mb-4 flex-wrap">
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="credentials">Credentials</TabsTrigger>
          <TabsTrigger value="users">Users ({a.users_count})</TabsTrigger>
          <TabsTrigger value="connect">Connect</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="space-y-4">
          <div><Label className="mb-2 block">Client ID</Label><CopyField value={a.id} /></div>
          <StatCards u={usage.data} loading={usage.isLoading} />
          <Card>
            <CardHeader><CardTitle className="text-base">Recent requests</CardTitle></CardHeader>
            <CardContent><RequestsTable rows={reqs.data} loading={reqs.isLoading} showApp={false} /></CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="credentials" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Client secret</CardTitle>
              <CardDescription>Rotated {relTime(a.secret_rotated_at)}. Rotating invalidates the old secret immediately; existing user tokens keep working.</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              <CopyField value={a.id} />
              <Confirm trigger={<Button variant="outline">Rotate secret</Button>} title="Rotate client secret?"
                description="The current secret stops working right away. Update every client that uses it." action="Rotate"
                destructive={false} onConfirm={() => rotate.mutate(undefined, { onSuccess: (r) => setSecret(r.client_secret) })} />
            </CardContent>
          </Card>
          <Card>
            <CardHeader><CardTitle className="text-base">Settings</CardTitle></CardHeader>
            <CardContent className="max-w-lg space-y-4">
              <AppFields key={a.id + a.name} initial={a} onChange={setDraft} />
              <Button disabled={!draft.name.trim() || patch.isPending} onClick={() => patch.mutate(draft)}>Save changes</Button>
            </CardContent>
          </Card>
          <Card className="border-destructive/40">
            <CardHeader>
              <CardTitle className="text-destructive text-base">Danger zone</CardTitle>
              <CardDescription>Deleting an app disconnects all of its users and removes its credentials.</CardDescription>
            </CardHeader>
            <CardContent>
              <Confirm trigger={<Button variant="destructive">Delete app</Button>} title={`Delete ${a.name}?`}
                description="This cannot be undone." action="Delete"
                onConfirm={() => del.mutate(undefined, { onSuccess: () => nav("/dashboard/apps") })} />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="users">
          <Card className="py-2"><GrantsTable rows={grants.data} loading={grants.isLoading} showApp={false} /></Card>
        </TabsContent>

        <TabsContent value="connect"><Connect app={a} cfg={cfg.data} /></TabsContent>
      </Tabs>
      <SecretDialog secret={secret} clientId={a.id} onClose={() => setSecret(null)} />
    </>
  )
}
