import { useState } from "react"
import { PageHeader } from "@/components/common"
import { GrantsTable } from "@/components/grants-table"
import { Card } from "@/components/ui/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useApps, useGrants } from "@/lib/hooks"

export default function UsersPage() {
  const [app, setApp] = useState("all")
  const apps = useApps()
  const grants = useGrants(app === "all" ? undefined : app)
  return (
    <>
      <PageHeader title="Users" description="Spotify accounts connected to your apps."
        actions={
          <Select value={app} onValueChange={setApp}>
            <SelectTrigger className="w-44"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All apps</SelectItem>
              {apps.data?.map((a) => <SelectItem key={a.id} value={a.id}>{a.name}</SelectItem>)}
            </SelectContent>
          </Select>
        } />
      <Card className="py-2"><GrantsTable rows={grants.data} loading={grants.isLoading} showApp /></Card>
    </>
  )
}
