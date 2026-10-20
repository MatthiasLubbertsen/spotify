import { useEffect, useState } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Button } from "@/components/ui/button"
import { api, fmtTime, type App, type Req, type Usage } from "../api"
import { StatusBadge } from "../components/bits"

export function RequestsTable({ appId, limit = 50, showApp = true }: { appId?: string; limit?: number; showApp?: boolean }) {
  const [rows, setRows] = useState<Req[] | null>(null)
  const load = () => api<Req[]>(`/requests?limit=${limit}${appId ? "&app=" + appId : ""}`).then(setRows)
  useEffect(() => { load() }, [appId])
  return (
    <div className="space-y-2">
      <div className="flex justify-end"><Button size="sm" variant="outline" onClick={load}>Refresh</Button></div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Time</TableHead>{showApp && <TableHead>App</TableHead>}<TableHead>User</TableHead>
            <TableHead>Request</TableHead><TableHead>Status</TableHead><TableHead className="text-right">ms</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows?.map((r, i) => (
            <TableRow key={i}>
              <TableCell className="whitespace-nowrap text-muted-foreground">{fmtTime(r.ts)}</TableCell>
              {showApp && <TableCell>{r.app || "—"}</TableCell>}
              <TableCell className="font-mono text-xs">{r.userId || "—"}</TableCell>
              <TableCell className="font-mono text-xs"><span className="text-muted-foreground">{r.method}</span> {r.path}</TableCell>
              <TableCell><StatusBadge status={r.status} /></TableCell>
              <TableCell className="text-right text-muted-foreground">{r.ms}</TableCell>
            </TableRow>
          ))}
          {rows && rows.length === 0 && <TableRow><TableCell colSpan={6} className="text-center text-muted-foreground">No requests yet.</TableCell></TableRow>}
        </TableBody>
      </Table>
    </div>
  )
}

function Bars({ data }: { data: Usage["buckets"] }) {
  const max = Math.max(1, ...data.map((b) => b.count))
  return (
    <div className="flex h-32 items-end gap-px">
      {data.map((b) => (
        <div key={b.t} className="group relative flex-1 flex flex-col justify-end h-full"
          title={`${new Date(b.t * 1000).toLocaleString()}: ${b.count} requests, ${b.errors} errors`}>
          <div className="w-full bg-red-400/80" style={{ height: `${(b.errors / max) * 100}%` }} />
          <div className="w-full bg-emerald-400/70" style={{ height: `${((b.count - b.errors) / max) * 100}%`, minHeight: b.count ? 1 : 0 }} />
        </div>
      ))}
    </div>
  )
}

export function UsagePage() {
  const [hours, setHours] = useState(24)
  const [appId, setAppId] = useState("")
  const [apps, setApps] = useState<App[]>([])
  const [u, setU] = useState<Usage | null>(null)
  useEffect(() => { api<App[]>("/apps").then(setApps) }, [])
  useEffect(() => { api<Usage>(`/usage?hours=${hours}${appId ? "&app=" + appId : ""}`).then(setU) }, [hours, appId])

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-2">
        <h1 className="text-xl font-semibold mr-auto">Usage</h1>
        <select className="h-9 rounded-md border bg-background px-2 text-sm" value={appId} onChange={(e) => setAppId(e.target.value)}>
          <option value="">All apps</option>
          {apps.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
        </select>
        {[[24, "24 h"], [168, "7 d"]].map(([h, l]) => (
          <Button key={h} size="sm" variant={hours === h ? "secondary" : "outline"} onClick={() => setHours(h as number)}>{l}</Button>
        ))}
      </div>
      <div className="grid gap-4 sm:grid-cols-3">
        <Card><CardHeader><CardDescription>Requests</CardDescription><CardTitle className="text-3xl">{u?.total ?? "…"}</CardTitle></CardHeader></Card>
        <Card><CardHeader><CardDescription>Errors (4xx/5xx)</CardDescription><CardTitle className="text-3xl">{u?.errors ?? "…"}</CardTitle></CardHeader></Card>
        <Card><CardHeader><CardDescription>Period</CardDescription><CardTitle className="text-3xl">{hours === 24 ? "24 hours" : "7 days"}</CardTitle></CardHeader></Card>
      </div>
      <Card>
        <CardHeader><CardTitle>Requests per hour</CardTitle><CardDescription>Authorize, token and API calls. Red = errors. Logs are kept for 7 days.</CardDescription></CardHeader>
        <CardContent>{u && <Bars data={u.buckets} />}</CardContent>
      </Card>
      {!appId && u && u.perApp.length > 0 && (
        <Card>
          <CardHeader><CardTitle>By app</CardTitle></CardHeader>
          <CardContent>
            <Table>
              <TableHeader><TableRow><TableHead>App</TableHead><TableHead className="text-right">Requests</TableHead><TableHead className="text-right">Errors</TableHead><TableHead className="text-right">Avg ms</TableHead></TableRow></TableHeader>
              <TableBody>{u.perApp.map((a) => (
                <TableRow key={a.appId}><TableCell>{a.name}</TableCell><TableCell className="text-right">{a.count}</TableCell><TableCell className="text-right">{a.errors}</TableCell><TableCell className="text-right">{a.avgMs}</TableCell></TableRow>
              ))}</TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
      <Card>
        <CardHeader><CardTitle>Recent requests</CardTitle></CardHeader>
        <CardContent><RequestsTable appId={appId || undefined} limit={100} /></CardContent>
      </Card>
    </div>
  )
}
