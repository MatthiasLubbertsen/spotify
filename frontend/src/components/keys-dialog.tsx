import { useState } from "react"
import { Radio } from "lucide-react"
import { api, type ApiKey, type Grant } from "@/lib/api"
import { CodeBlock, CopyField, Confirm } from "@/components/common"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { useAction, useConfig, useKeys } from "@/lib/hooks"
import { relTime } from "@/lib/format"

const NEEDED = ["user-read-currently-playing", "user-read-playback-state"]

/** Manage now-playing API keys for one connected user. */
export function KeysDialog({ grant }: { grant: Grant }) {
  const [open, setOpen] = useState(false)
  const [label, setLabel] = useState("")
  const [created, setCreated] = useState<string | null>(null)
  const { data: config } = useConfig()
  const { data: all } = useKeys()
  const keys = (all ?? []).filter((k: ApiKey) => k.grant_id === grant.id)
  const hasScope = NEEDED.some((s) => grant.scope.split(" ").includes(s))
  const create = useAction(
    (l: string) => api<{ key: string }>(`/grants/${grant.id}/keys`, { method: "POST", json: { label: l } }),
    ["keys"],
  )
  const revoke = useAction((id: number) => api(`/keys/${id}`, { method: "DELETE" }), ["keys"], "Key revoked")
  const base = config?.public_url ?? ""

  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        <Radio className="size-3.5" /> Now playing
      </Button>
      <Dialog open={open} onOpenChange={(o) => { setOpen(o); if (!o) setCreated(null) }}>
        <DialogContent className="sm:max-w-xl [&>*]:min-w-0">
          <DialogHeader>
            <DialogTitle>Now playing for {grant.display_name || grant.spotify_id}</DialogTitle>
            <DialogDescription>Poll with an API key. No OAuth needed on the caller's side.</DialogDescription>
          </DialogHeader>

          {!hasScope && (
            <Alert variant="destructive">
              <AlertDescription>
                This login lacks the <code>user-read-currently-playing</code> scope. Request it in the app's authorize
                call and log in again.
              </AlertDescription>
            </Alert>
          )}

          {created ? (
            <div className="space-y-3">
              <p className="text-muted-foreground text-sm">Copy the key now. It is stored hashed and will not be shown again.</p>
              <CopyField value={created} />
              <CodeBlock code={`curl -H "X-API-Key: ${created}" ${base}/now-playing`} />
              <Button onClick={() => setCreated(null)}>Done</Button>
            </div>
          ) : (
            <form
              className="flex gap-2"
              onSubmit={(e) => {
                e.preventDefault()
                create.mutate(label, { onSuccess: (r) => { setCreated(r.key); setLabel("") } })
              }}
            >
              <Input placeholder="Label (optional), e.g. desk display" value={label} onChange={(e) => setLabel(e.target.value)} maxLength={80} />
              <Button type="submit" disabled={create.isPending}>Create key</Button>
            </form>
          )}

          {keys.length > 0 && (
            <ul className="divide-y rounded-md border text-sm">
              {keys.map((k) => (
                <li key={k.id} className="flex items-center justify-between gap-3 px-3 py-2">
                  <div className="min-w-0">
                    <div className="truncate font-medium">{k.label || "Untitled key"}</div>
                    <div className="text-muted-foreground font-mono text-xs">
                      {k.prefix}… · created {relTime(k.created_at)} · last used {relTime(k.last_used_at)}
                    </div>
                  </div>
                  <Confirm
                    trigger={<Button variant="ghost" size="sm" className="text-destructive">Revoke</Button>}
                    title="Revoke key?" description="Anything polling with this key will get 401 right away."
                    action="Revoke" onConfirm={() => revoke.mutate(k.id)}
                  />
                </li>
              ))}
            </ul>
          )}
        </DialogContent>
      </Dialog>
    </>
  )
}
