const BASE = "/dashboard/api"

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

export async function api<T = unknown>(path: string, init?: RequestInit & { json?: unknown }): Promise<T> {
  const { json, ...rest } = init ?? {}
  const res = await fetch(BASE + path, {
    ...rest,
    headers: { ...(json !== undefined ? { "Content-Type": "application/json" } : {}), ...rest.headers },
    body: json !== undefined ? JSON.stringify(json) : rest.body,
  })
  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new ApiError(res.status, (data as { error?: string }).error ?? res.statusText)
  return data as T
}

export type App = {
  id: string
  name: string
  redirect_uris: string[]
  allowed_scopes: string[]
  rate_limit_per_min: number
  allow_client_credentials: boolean
  enabled: boolean
  created_at: number
  secret_rotated_at: number
  users_count: number
  requests_24h: number
}
export type Grant = {
  id: number
  app_id: string
  app_name: string
  user_id: number
  spotify_id: string
  display_name: string
  scope: string
  status: "ok" | "reauth"
  created_at: number
  last_used_at: number
}
export type Req = {
  id: number
  ts: number
  app_id: string
  app_name: string
  grant_id: number
  kind: string
  method: string
  path: string
  status: number
  upstream_status: number
  duration_ms: number
  bytes: number
}
export type Usage = {
  series: { t: number; total: number; errors: number }[]
  total: number
  errors: number
  rate_limited: number
  avg_ms: number
  top_paths: { path: string; count: number }[]
  per_app: { app_id: string; app_name: string; total: number; errors: number }[]
}
export type Config = {
  public_url: string
  redirect_uri: string
  authorize_url: string
  token_url: string
  api_base: string
  upstream_ready: boolean
  cooldown_seconds: number
}
export type Upstream = { client_id: string; has_secret: boolean; configured: boolean; redirect_uri: string }
export type Secret = { app: App; client_secret: string }
export type ApiKey = {
  id: number
  grant_id: number
  app_id: string
  app_name: string
  spotify_id: string
  display_name: string
  label: string
  prefix: string
  created_at: number
  last_used_at: number
}
