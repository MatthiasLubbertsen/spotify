import { Activity, AppWindow, Cloud, Users } from "lucide-react"
import { LayoutDashboard } from "lucide-react"
import { NavLink, Outlet, useLocation } from "react-router-dom"
import { Sidebar, SidebarContent, SidebarFooter, SidebarGroup, SidebarGroupContent, SidebarHeader, SidebarInset, SidebarMenu, SidebarMenuButton, SidebarMenuItem, SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar"
import { TooltipProvider } from "@/components/ui/tooltip"
import { useMe } from "@/lib/hooks"

const nav = [
  { to: "/dashboard", label: "Overview", icon: LayoutDashboard, end: true },
  { to: "/dashboard/apps", label: "Apps", icon: AppWindow },
  { to: "/dashboard/users", label: "Users", icon: Users },
  { to: "/dashboard/requests", label: "Requests", icon: Activity },
  { to: "/dashboard/upstream", label: "Upstream", icon: Cloud },
]

export default function Layout() {
  const me = useMe()
  const { pathname } = useLocation()
  return (
    <TooltipProvider>
      <SidebarProvider>
        <Sidebar>
          <SidebarHeader className="px-4 py-4">
            <div className="flex items-center gap-2 font-semibold">
              <span className="size-3 rounded-full bg-emerald-500" />
              spotis
            </div>
          </SidebarHeader>
          <SidebarContent>
            <SidebarGroup>
              <SidebarGroupContent>
                <SidebarMenu>
                  {nav.map((n) => (
                    <SidebarMenuItem key={n.to}>
                      <SidebarMenuButton asChild isActive={n.end ? pathname === n.to : pathname.startsWith(n.to)}>
                        <NavLink to={n.to} end={n.end}>
                          <n.icon />
                          <span>{n.label}</span>
                        </NavLink>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  ))}
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>
          </SidebarContent>
          <SidebarFooter className="px-4 py-3">
            <div className="text-muted-foreground truncate text-xs" title={me.data?.email}>
              Signed in as <span className="text-foreground">{me.data?.user ?? "…"}</span>
            </div>
          </SidebarFooter>
        </Sidebar>
        <SidebarInset>
          <header className="flex h-12 items-center gap-2 border-b px-4 md:hidden">
            <SidebarTrigger />
            <span className="font-semibold">spotis</span>
          </header>
          <main className="mx-auto w-full max-w-6xl p-4 md:p-8">
            <Outlet />
          </main>
        </SidebarInset>
      </SidebarProvider>
    </TooltipProvider>
  )
}
