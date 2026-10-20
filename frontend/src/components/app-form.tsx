import { useState } from "react"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import type { App } from "@/lib/api"

export type AppDraft = {
  name: string
  redirect_uris: string[]
  allowed_scopes: string[]
  rate_limit_per_min: number
  allow_client_credentials: boolean
}

const lines = (s: string) => s.split(/[\n,\s]+/).map((x) => x.trim()).filter(Boolean)

/** Controlled-by-submit form; the parent reads the draft via onChange. */
export function AppFields({ initial, onChange }: { initial?: App; onChange: (d: AppDraft) => void }) {
  const [name, setName] = useState(initial?.name ?? "")
  const [uris, setUris] = useState((initial?.redirect_uris ?? []).join("\n"))
  const [scopes, setScopes] = useState((initial?.allowed_scopes ?? []).join(" "))
  const [rate, setRate] = useState(String(initial?.rate_limit_per_min ?? 0))
  const [cc, setCC] = useState(initial?.allow_client_credentials ?? true)
  const emit = (p: Partial<{ name: string; uris: string; scopes: string; rate: string; cc: boolean }>) => {
    const v = { name, uris, scopes, rate, cc, ...p }
    onChange({
      name: v.name, redirect_uris: lines(v.uris), allowed_scopes: lines(v.scopes),
      rate_limit_per_min: Math.max(0, parseInt(v.rate) || 0), allow_client_credentials: v.cc,
    })
  }
  return (
    <div className="space-y-4">
      <div className="space-y-2">
        <Label htmlFor="name">Name</Label>
        <Input id="name" value={name} onChange={(e) => { setName(e.target.value); emit({ name: e.target.value }) }} placeholder="My playlist tool" />
      </div>
      <div className="space-y-2">
        <Label htmlFor="uris">Redirect URIs</Label>
        <Textarea id="uris" rows={3} value={uris} onChange={(e) => { setUris(e.target.value); emit({ uris: e.target.value }) }}
          placeholder={"https://example.com/callback\nhttp://127.0.0.1:8000/callback"} className="font-mono text-xs" />
        <p className="text-muted-foreground text-xs">One per line. https, or http on 127.0.0.1 / [::1] (a loopback URI without a port allows any port).</p>
      </div>
      <div className="space-y-2">
        <Label htmlFor="scopes">Allowed scopes (optional)</Label>
        <Input id="scopes" value={scopes} onChange={(e) => { setScopes(e.target.value); emit({ scopes: e.target.value }) }}
          placeholder="empty = any scope" className="font-mono text-xs" />
      </div>
      <div className="grid grid-cols-2 items-end gap-4">
        <div className="space-y-2">
          <Label htmlFor="rate">Rate limit (req/min)</Label>
          <Input id="rate" type="number" min={0} value={rate} onChange={(e) => { setRate(e.target.value); emit({ rate: e.target.value }) }} />
          <p className="text-muted-foreground text-xs">0 = unlimited</p>
        </div>
        <div className="flex items-center gap-2 pb-6">
          <Switch id="cc" checked={cc} onCheckedChange={(c) => { setCC(c); emit({ cc: c }) }} />
          <Label htmlFor="cc">Client credentials</Label>
        </div>
      </div>
    </div>
  )
}

export const emptyDraft = (initial?: App): AppDraft => ({
  name: initial?.name ?? "", redirect_uris: initial?.redirect_uris ?? [], allowed_scopes: initial?.allowed_scopes ?? [],
  rate_limit_per_min: initial?.rate_limit_per_min ?? 0, allow_client_credentials: initial?.allow_client_credentials ?? true,
})
