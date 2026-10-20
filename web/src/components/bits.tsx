import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Check, Copy } from "lucide-react"
import { toast } from "sonner"

export function CopyButton({ value, label }: { value: string; label?: string }) {
  const [done, setDone] = useState(false)
  return (
    <Button type="button" variant="outline" size="sm" onClick={async () => {
      try { await navigator.clipboard.writeText(value); setDone(true); setTimeout(() => setDone(false), 1500) }
      catch { toast.error("Copy failed") }
    }}>
      {done ? <Check className="size-4" /> : <Copy className="size-4" />}{label}
    </Button>
  )
}

export function Field({ label, value, mono = true, extra }: { label: string; value: string; mono?: boolean; extra?: React.ReactNode }) {
  return (
    <div className="space-y-1">
      <div className="text-sm text-muted-foreground">{label}</div>
      <div className="flex items-center gap-2">
        <code className={"flex-1 min-w-0 truncate rounded-md border bg-background px-3 py-2 text-sm " + (mono ? "font-mono" : "")}>{value}</code>
        {extra}
        <CopyButton value={value} />
      </div>
    </div>
  )
}

export function CodeBlock({ code }: { code: string }) {
  return (
    <div className="relative">
      <pre className="overflow-x-auto rounded-md border bg-background p-4 text-xs leading-relaxed"><code>{code}</code></pre>
      <div className="absolute right-2 top-2"><CopyButton value={code} /></div>
    </div>
  )
}

export function StatusBadge({ status }: { status: number }) {
  const cls = status >= 500 ? "text-red-400" : status >= 400 ? "text-amber-400" : status >= 300 ? "text-sky-400" : "text-emerald-400"
  return <span className={"font-mono " + cls}>{status}</span>
}
