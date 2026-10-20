import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"
import { api, type App, type Config, type Grant, type Req, type Upstream, type Usage } from "./api"

export const useConfig = () => useQuery({ queryKey: ["config"], queryFn: () => api<Config>("/config"), refetchInterval: 15000 })
export const useMe = () => useQuery({ queryKey: ["me"], queryFn: () => api<{ user: string; email: string }>("/me") })
export const useApps = () => useQuery({ queryKey: ["apps"], queryFn: () => api<App[]>("/apps") })
export const useApp = (id: string) => useQuery({ queryKey: ["apps", id], queryFn: () => api<App>(`/apps/${id}`) })
export const useUpstream = () => useQuery({ queryKey: ["upstream"], queryFn: () => api<Upstream>("/upstream") })
export const useGrants = (app?: string) =>
  useQuery({ queryKey: ["grants", app ?? ""], queryFn: () => api<Grant[]>(`/grants${app ? `?app=${app}` : ""}`) })
export const useUsage = (range: string, app?: string) =>
  useQuery({
    queryKey: ["usage", range, app ?? ""],
    queryFn: () => api<Usage>(`/usage?range=${range}${app ? `&app=${app}` : ""}`),
    refetchInterval: 15000,
  })
export const useRequests = (opts: { app?: string; errors?: boolean; limit?: number; refresh?: number }) =>
  useQuery({
    queryKey: ["requests", opts.app ?? "", !!opts.errors, opts.limit ?? 100],
    queryFn: () => api<Req[]>(`/requests?limit=${opts.limit ?? 100}${opts.app ? `&app=${opts.app}` : ""}${opts.errors ? "&errors=1" : ""}`),
    refetchInterval: opts.refresh ?? 5000,
  })

/** Mutation that toasts errors and invalidates the given query keys on success. */
export function useAction<V, R = unknown>(fn: (v: V) => Promise<R>, invalidate: string[], success?: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: fn,
    onSuccess: () => {
      invalidate.forEach((k) => qc.invalidateQueries({ queryKey: [k] }))
      if (success) toast.success(success)
    },
    onError: (e: Error) => toast.error(e.message),
  })
}
