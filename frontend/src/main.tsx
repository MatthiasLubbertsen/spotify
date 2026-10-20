import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { BrowserRouter, Route, Routes } from "react-router-dom"
import "./index.css"
import Layout from "@/components/layout"
import { Toaster } from "@/components/ui/sonner"
import Landing from "@/pages/landing"
import Overview from "@/pages/overview"
import Apps from "@/pages/apps"
import AppDetail from "@/pages/app-detail"
import UsersPage from "@/pages/users"
import Requests from "@/pages/requests"
import Upstream from "@/pages/upstream"

const qc = new QueryClient({ defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } } })

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={qc}>
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<Landing />} />
          <Route path="/dashboard" element={<Layout />}>
            <Route index element={<Overview />} />
            <Route path="apps" element={<Apps />} />
            <Route path="apps/:id" element={<AppDetail />} />
            <Route path="users" element={<UsersPage />} />
            <Route path="requests" element={<Requests />} />
            <Route path="upstream" element={<Upstream />} />
          </Route>
          <Route path="*" element={<Landing />} />
        </Routes>
      </BrowserRouter>
      <Toaster richColors />
    </QueryClientProvider>
  </StrictMode>,
)
