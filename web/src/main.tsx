import React from "react"
import { createRoot } from "react-dom/client"
import "./index.css"
import { Toaster } from "@/components/ui/sonner"
import { Landing } from "./Landing"
import { Dashboard } from "./Dashboard"

const onDashboard = location.pathname.startsWith("/dashboard")

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    {onDashboard ? <Dashboard /> : <Landing />}
    <Toaster />
  </React.StrictMode>,
)
