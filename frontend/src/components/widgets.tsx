import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Empty, StatusBadge } from "@/components/common"
import type { Req, Usage } from "@/lib/api"
import { num, relTime } from "@/lib/format"

export function StatCards({ u, loading }: { u?: Usage; loading: boolean }) {
  const items = [
    ["Requests", u && num(u.total)],
    ["Errors", u && num(u.errors)],
    ["Rate limited", u && num(u.rate_limited)],
    ["Avg latency", u && `${Math.round(u.avg_ms)} ms`],
  ]
  return (
    <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
      {items.map(([label, v]) => (
        <Card key={label as string} className="gap-1 py-4">
          <CardHeader className="px-4">
            <CardDescription>{label}</CardDescription>
          </CardHeader>
          <CardContent className="px-4 text-2xl font-semibold">{loading || v === undefined ? <Skeleton className="h-8 w-20" /> : v}</CardContent>
        </Card>
      ))}
    </div>
  )
}

const cfg = {
  total: { label: "Requests", color: "var(--chart-1)" },
  errors: { label: "Errors", color: "var(--chart-2)" },
} satisfies ChartConfig

export function UsageChart({ u, range }: { u?: Usage; range: string }) {
  const fmt = (t: number) => {
    const d = new Date(t)
    return range === "7d" || range === "30d" ? d.toLocaleDateString(undefined, { month: "short", day: "numeric" }) : d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
  }
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Requests over time</CardTitle>
      </CardHeader>
      <CardContent>
        {!u ? (
          <Skeleton className="h-56 w-full" />
        ) : (
          <ChartContainer config={cfg} className="h-56 w-full">
            <AreaChart data={u.series} margin={{ left: 0, right: 8 }}>
              <CartesianGrid vertical={false} />
              <XAxis dataKey="t" tickFormatter={fmt} tickLine={false} axisLine={false} minTickGap={32} />
              <YAxis allowDecimals={false} width={32} tickLine={false} axisLine={false} />
              <ChartTooltip content={<ChartTooltipContent labelFormatter={(_, p) => (p[0] ? fmt(p[0].payload.t) : "")} />} />
              <Area dataKey="total" type="monotone" stroke="var(--color-total)" fill="var(--color-total)" fillOpacity={0.2} />
              <Area dataKey="errors" type="monotone" stroke="var(--color-errors)" fill="var(--color-errors)" fillOpacity={0.2} />
            </AreaChart>
          </ChartContainer>
        )}
      </CardContent>
    </Card>
  )
}

export function RequestsTable({ rows, showApp = true, loading }: { rows?: Req[]; showApp?: boolean; loading?: boolean }) {
  if (loading || !rows) return <Skeleton className="h-40 w-full" />
  if (rows.length === 0) return <Empty>No requests yet.</Empty>
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Time</TableHead>
          {showApp && <TableHead>App</TableHead>}
          <TableHead>Request</TableHead>
          <TableHead>Status</TableHead>
          <TableHead className="text-right">Time (ms)</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((r) => (
          <TableRow key={r.id}>
            <TableCell className="text-muted-foreground whitespace-nowrap">{relTime(r.ts, true)}</TableCell>
            {showApp && <TableCell>{r.app_name || "—"}</TableCell>}
            <TableCell className="max-w-[28rem] truncate font-mono text-xs" title={r.path}>
              <span className="text-muted-foreground mr-2">{r.method}</span>
              {r.path}
            </TableCell>
            <TableCell>
              <StatusBadge status={r.status} />
              {r.upstream_status > 0 && r.upstream_status !== r.status && (
                <span className="text-muted-foreground ml-2 text-xs">upstream {r.upstream_status}</span>
              )}
            </TableCell>
            <TableCell className="text-right tabular-nums">{r.duration_ms}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
