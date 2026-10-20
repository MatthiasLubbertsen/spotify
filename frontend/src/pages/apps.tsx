import { Plus } from "lucide-react"
import { useState } from "react"
import { Link } from "react-router-dom"
import { api, type App, type Secret } from "@/lib/api"
import { AppFields, emptyDraft, type AppDraft } from "@/components/app-form"
import { Empty, PageHeader, SecretDialog } from "@/components/common"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useAction, useApps } from "@/lib/hooks"
import { num, relTime } from "@/lib/format"

export default function Apps() {
  const apps = useApps()
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState<AppDraft>(emptyDraft())
  const [created, setCreated] = useState<Secret | null>(null)
  const create = useAction(
    (d: AppDraft) => api<Secret>("/apps", { method: "POST", json: d }),
    ["apps"],
  )
  return (
    <>
      <PageHeader title="Apps" description="Virtual apps clients sign in with."
        actions={<Button onClick={() => { setDraft(emptyDraft()); setOpen(true) }}><Plus className="size-4" />New app</Button>} />
      <Card className="py-2">
        {apps.isLoading ? <Skeleton className="m-4 h-32" /> : apps.data?.length === 0 ? (
          <Empty>No apps yet. Create one to get a client ID and secret.</Empty>
        ) : (
          <Table>
            <TableHeader>
              <TableRow><TableHead>Name</TableHead><TableHead>Client ID</TableHead><TableHead className="text-right">Users</TableHead>
                <TableHead className="text-right">Requests 24h</TableHead><TableHead>Created</TableHead><TableHead /></TableRow>
            </TableHeader>
            <TableBody>
              {apps.data?.map((a: App) => (
                <TableRow key={a.id}>
                  <TableCell className="font-medium"><Link to={`/dashboard/apps/${a.id}`} className="hover:underline">{a.name}</Link></TableCell>
                  <TableCell className="text-muted-foreground font-mono text-xs">{a.id}</TableCell>
                  <TableCell className="text-right tabular-nums">{a.users_count}</TableCell>
                  <TableCell className="text-right tabular-nums">{num(a.requests_24h)}</TableCell>
                  <TableCell className="text-muted-foreground">{relTime(a.created_at)}</TableCell>
                  <TableCell>{!a.enabled && <Badge variant="secondary">disabled</Badge>}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New app</DialogTitle>
            <DialogDescription>You get a client ID and secret to use in place of Spotify's.</DialogDescription>
          </DialogHeader>
          <AppFields onChange={setDraft} />
          <DialogFooter>
            <Button disabled={!draft.name.trim() || create.isPending}
              onClick={() => create.mutate(draft, { onSuccess: (r) => { setOpen(false); setCreated(r) } })}>Create</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <SecretDialog secret={created?.client_secret ?? null} clientId={created?.app.id} onClose={() => setCreated(null)} />
    </>
  )
}
