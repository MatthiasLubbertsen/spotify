export type App = {
  id: string; name: string; clientId: string; clientSecret?: string
  redirectUris: string[]; allowedScopes: string; rateLimit: number; enabled: boolean
  createdAt: number; connections: number; requests24h: number; errors24h: number
}
export type Connection = { id: string; userId: string; displayName: string; scope: string; createdAt: number; lastUsed: number }
export type Me = { user: string; publicUrl: string; authorizeUrl: string; tokenUrl: string; apiUrl: string; callbackUrl: string }
export type Upstream = { clientId: string; hasSecret: boolean; configured: boolean; callbackUrl: string; blockedSeconds: number; connections: number }
export type Usage = { hours: number; total: number; errors: number; buckets: { t: number; count: number; errors: number }[]; perApp: { appId: string; name: string; count: number; errors: number; avgMs: number }[] }
export type Req = { ts: number; appId: string; app: string; userId: string; method: string; path: string; status: number; ms: number }

export async function api<T = unknown>(path: string, method = "GET", body?: unknown): Promise<T> {
  const res = await fetch("/dashboard/api" + path, {
    method,
    headers: body === undefined ? {} : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(data.error || res.statusText)
  return data as T
}

export const fmtTime = (ts: number) => (ts ? new Date(ts * 1000).toLocaleString() : "never")
