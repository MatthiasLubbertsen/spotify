import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { api, type Me } from "./api"
import { AppsPage, AppDetail } from "./pages/Apps"
import { UsagePage } from "./pages/Usage"
import { UpstreamPage } from "./pages/Upstream"
import { cn } from "@/lib/utils"

function useHash() {
  const [h, setH] = useState(location.hash.slice(1) || "/apps")
  useEffect(() => {
    const f = () => setH(location.hash.slice(1) || "/apps")
    addEventListener("hashchange", f)
    return () => removeEventListener("hashchange", f)
  }, [])
  return h
}

export const go = (to: string) => { location.hash = to }

export function Dashboard() {
  const hash = useHash()
  const [me, setMe] = useState<Me | null>(null)
  const [err, setErr] = useState("")
  useEffect(() => { api<Me>("/me").then(setMe).catch((e) => setErr(e.message)) }, [])

  const nav = [
    { to: "/apps", label: "Apps" },
    { to: "/usage", label: "Usage" },
    { to: "/upstream", label: "Spotify app" },
  ]
  const section = "/" + hash.split("/")[1]

  let page = <AppsPage />
  if (me) {
    if (hash.startsWith("/apps/")) page = <AppDetail id={hash.split("/")[2]} me={me} />
    else if (section === "/usage") page = <UsagePage />
    else if (section === "/upstream") page = <UpstreamPage me={me} />
  }

  return (
    <div className="min-h-screen">
      <header className="border-b">
        <div className="mx-auto max-w-5xl px-4 py-3 flex flex-wrap items-center gap-x-6 gap-y-2">
          <span className="font-semibold">Spotify Proxy dashboard</span>
          <nav className="flex gap-1">
            {nav.map((n) => (
              <Button key={n.to} variant="ghost" size="sm" onClick={() => go(n.to)}
                className={cn(section === n.to && "bg-accent text-accent-foreground")}>
                {n.label}
              </Button>
            ))}
          </nav>
          <span className="ml-auto text-sm text-muted-foreground">{me?.user}</span>
        </div>
      </header>
      <main className="mx-auto max-w-5xl px-4 py-6">
        {err ? <p className="text-destructive">{err}</p> : me ? page : <p className="text-muted-foreground">Loading…</p>}
      </main>
    </div>
  )
}
