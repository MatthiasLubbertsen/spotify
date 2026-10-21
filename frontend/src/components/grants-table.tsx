import { api, type Grant } from "@/lib/api"
import { Confirm, Empty, GrantStatus } from "@/components/common"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { KeysDialog } from "@/components/keys-dialog"
import { useAction } from "@/lib/hooks"
import { relTime } from "@/lib/format"

export function GrantsTable({ rows, showApp, loading }: { rows?: Grant[]; showApp: boolean; loading: boolean }) {
  const revoke = useAction((id: number) => api(`/grants/${id}`, { method: "DELETE" }), ["grants", "apps"], "Access revoked")
  const revokeAll = useAction((id: number) => api(`/users/${id}`, { method: "DELETE" }), ["grants", "apps"], "User revoked everywhere")
  if (loading || !rows) return <Skeleton className="h-32 w-full" />
  if (rows.length === 0) return <Empty>No connected users yet.</Empty>
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>User</TableHead>
          {showApp && <TableHead>App</TableHead>}
          <TableHead>Status</TableHead>
          <TableHead>Connected</TableHead>
          <TableHead>Last used</TableHead>
          <TableHead />
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((g) => (
          <TableRow key={g.id}>
            <TableCell>
              <div className="font-medium">{g.display_name || g.spotify_id}</div>
              <div className="text-muted-foreground font-mono text-xs">{g.spotify_id}</div>
            </TableCell>
            {showApp && <TableCell>{g.app_name}</TableCell>}
            <TableCell><GrantStatus status={g.status} /></TableCell>
            <TableCell className="text-muted-foreground">{relTime(g.created_at)}</TableCell>
            <TableCell className="text-muted-foreground">{relTime(g.last_used_at)}</TableCell>
            <TableCell className="space-x-2 text-right whitespace-nowrap">
              <KeysDialog grant={g} />
              <Confirm trigger={<Button variant="outline" size="sm">Revoke</Button>} title="Revoke access?"
                description={`${g.display_name || g.spotify_id} will be signed out of ${g.app_name} and must log in again.`}
                action="Revoke" onConfirm={() => revoke.mutate(g.id)} />
              {showApp && (
                <Confirm trigger={<Button variant="ghost" size="sm" className="text-destructive">Revoke everywhere</Button>}
                  title="Revoke everywhere?"
                  description={`${g.display_name || g.spotify_id} will be signed out of every app.`}
                  action="Revoke everywhere" onConfirm={() => revokeAll.mutate(g.user_id)} />
              )}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
