import { useState } from "react"
import { PageHeader } from "@/components/common"
import { RequestsTable } from "@/components/widgets"
import { Card } from "@/components/ui/card"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { useApps, useRequests } from "@/lib/hooks"

export default function Requests() {
  const [app, setApp] = useState("all")
  const [errors, setErrors] = useState(false)
  const apps = useApps()
  const reqs = useRequests({ app: app === "all" ? undefined : app, errors, limit: 200 })
  return (
    <>
      <PageHeader title="Requests" description="Most recent requests. Refreshes every 5 seconds."
        actions={
          <>
            <div className="flex items-center gap-2"><Switch id="err" checked={errors} onCheckedChange={setErrors} /><Label htmlFor="err">Errors only</Label></div>
            <Select value={app} onValueChange={setApp}>
              <SelectTrigger className="w-44"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All apps</SelectItem>
                {apps.data?.map((a) => <SelectItem key={a.id} value={a.id}>{a.name}</SelectItem>)}
              </SelectContent>
            </Select>
          </>
        } />
      <Card className="py-2"><RequestsTable rows={reqs.data} loading={reqs.isLoading} /></Card>
    </>
  )
}
