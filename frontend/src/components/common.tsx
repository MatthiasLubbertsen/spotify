import { Check, Copy } from "lucide-react"
import { useState, type ReactNode } from "react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle, AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { cn } from "@/lib/utils"

export function copyText(text: string) {
  navigator.clipboard?.writeText(text).then(
    () => toast.success("Copied"),
    () => toast.error("Copy failed"),
  )
}

export function CopyButton({ text, label }: { text: string; label?: string }) {
  const [done, setDone] = useState(false)
  return (
    <Button
      type="button"
      variant="outline"
      size={label ? "sm" : "icon"}
      className={label ? "" : "size-8"}
      aria-label="Copy"
      onClick={() => {
        copyText(text)
        setDone(true)
        setTimeout(() => setDone(false), 1500)
      }}
    >
      {done ? <Check className="size-4" /> : <Copy className="size-4" />}
      {label}
    </Button>
  )
}

export function CopyField({ value, className }: { value: string; className?: string }) {
  return (
    <div className={cn("flex items-center gap-2", className)}>
      <code className="bg-muted min-w-0 flex-1 truncate rounded-md px-3 py-1.5 font-mono text-sm">{value}</code>
      <CopyButton text={value} />
    </div>
  )
}

export function CodeBlock({ code }: { code: string }) {
  return (
    <div className="relative">
      <pre className="bg-muted overflow-x-auto rounded-md p-4 pr-14 font-mono text-xs leading-relaxed">{code}</pre>
      <div className="absolute top-2 right-2">
        <CopyButton text={code} />
      </div>
    </div>
  )
}

export function PageHeader({ title, description, actions }: { title: string; description?: string; actions?: ReactNode }) {
  return (
    <div className="mb-6 flex flex-wrap items-start justify-between gap-3">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
        {description && <p className="text-muted-foreground mt-1 text-sm">{description}</p>}
      </div>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </div>
  )
}

export function StatusBadge({ status }: { status: number }) {
  const cls =
    status >= 500 ? "bg-red-500/15 text-red-400" :
    status === 429 ? "bg-orange-500/15 text-orange-400" :
    status >= 400 ? "bg-amber-500/15 text-amber-400" :
    status >= 300 ? "bg-sky-500/15 text-sky-400" : "bg-emerald-500/15 text-emerald-400"
  return <Badge variant="outline" className={cn("border-transparent font-mono", cls)}>{status}</Badge>
}

export function GrantStatus({ status }: { status: string }) {
  return status === "ok" ? (
    <Badge variant="outline" className="border-transparent bg-emerald-500/15 text-emerald-400">ok</Badge>
  ) : (
    <Badge variant="outline" className="border-transparent bg-amber-500/15 text-amber-400">needs re-login</Badge>
  )
}

export function Confirm({
  trigger, title, description, action, onConfirm, destructive = true,
}: {
  trigger: ReactNode; title: string; description: string; action: string; onConfirm: () => void; destructive?: boolean
}) {
  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>{trigger}</AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            className={destructive ? "bg-destructive text-white hover:bg-destructive/90" : ""}
            onClick={onConfirm}
          >
            {action}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

/** Shows a client secret exactly once. */
export function SecretDialog({ secret, clientId, onClose }: { secret: string | null; clientId?: string; onClose: () => void }) {
  return (
    <Dialog open={!!secret} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Client secret</DialogTitle>
          <DialogDescription>Copy it now. It is stored hashed and will not be shown again.</DialogDescription>
        </DialogHeader>
        {clientId && <CopyField value={clientId} />}
        {secret && <CopyField value={secret} />}
        <DialogFooter>
          <Button onClick={onClose}>Done</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="text-muted-foreground py-10 text-center text-sm">{children}</div>
}
