import { AlertTriangle, Info } from "lucide-react"
import { useState } from "react"
import { Link } from "react-router-dom"
import { PageHeader } from "@/components/common"
import { StatCards, UsageChart } from "@/components/widgets"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useConfig, useUsage } from "@/lib/hooks"
import { num } from "@/lib/format"

export const RangeSelect = ({ value, onChange }: { value: string; onChange: (v: string) => void }) => (
  <Select value={value} onValueChange={onChange}>
    <SelectTrigger className="w-32"><SelectValue /></SelectTrigger>
    <SelectContent>
      {[["1h", "Last hour"], ["24h", "Last 24h"], ["7d", "Last 7 days"], ["30d", "Last 30 days"]].map(([v, l]) => (
        <SelectItem key={v} value={v}>{l}</SelectItem>
      ))}
    </SelectContent>
  </Select>
)

export default function Overview() {
  const [range, setRange] = useState("24h")
  const usage = useUsage(range)
  const cfg = useConfig()
  const u = usage.data
  return (
    <>
      <PageHeader title="Overview" description="Traffic across all virtual apps." actions={<RangeSelect value={range} onChange={setRange} />} />
      <div className="space-y-4">
        {cfg.data && !cfg.data.upstream_ready && (
          <Alert>
            <Info className="size-4" />
            <AlertTitle>Upstream app not configured</AlertTitle>
            <AlertDescription>
              Sign-in and API calls will fail until you add the Spotify client ID and secret.{" "}
              <Link to="/dashboard/upstream" className="underline">Open upstream settings</Link>
            </AlertDescription>
          </Alert>
        )}
        {cfg.data && cfg.data.cooldown_seconds > 0 && (
          <Alert variant="destructive">
            <AlertTriangle className="size-4" />
            <AlertTitle>Spotify is rate limiting the upstream app</AlertTitle>
            <AlertDescription>Requests are paused for about {cfg.data.cooldown_seconds}s. All virtual apps share this quota.</AlertDescription>
          </Alert>
        )}
        <StatCards u={u} loading={usage.isLoading} />
        <UsageChart u={u} range={range} />
        <div className="grid gap-4 lg:grid-cols-2">
          <Card>
            <CardHeader><CardTitle className="text-base">Top paths</CardTitle></CardHeader>
            <CardContent>
              {u && u.top_paths.length === 0 ? <p className="text-muted-foreground text-sm">No API traffic yet.</p> : (
                <Table>
                  <TableBody>
                    {u?.top_paths.map((p) => (
                      <TableRow key={p.path}>
                        <TableCell className="max-w-64 truncate font-mono text-xs" title={p.path}>{p.path}</TableCell>
                        <TableCell className="text-right tabular-nums">{num(p.count)}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>
          <Card>
            <CardHeader><CardTitle className="text-base">By app</CardTitle></CardHeader>
            <CardContent>
              {u && u.per_app.length === 0 ? <p className="text-muted-foreground text-sm">No traffic yet.</p> : (
                <Table>
                  <TableHeader>
                    <TableRow><TableHead>App</TableHead><TableHead className="text-right">Requests</TableHead><TableHead className="text-right">Errors</TableHead></TableRow>
                  </TableHeader>
                  <TableBody>
                    {u?.per_app.map((a) => (
                      <TableRow key={a.app_id}>
                        <TableCell><Link to={`/dashboard/apps/${a.app_id}`} className="hover:underline">{a.app_name}</Link></TableCell>
                        <TableCell className="text-right tabular-nums">{num(a.total)}</TableCell>
                        <TableCell className="text-right tabular-nums">{num(a.errors)}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>
        </div>
      </div>
    </>
  )
}
