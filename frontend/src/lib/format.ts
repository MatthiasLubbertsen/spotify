export function relTime(ts: number, ms = false): string {
  if (!ts) return "never"
  const s = Math.max(0, Math.round((Date.now() - (ms ? ts : ts * 1000)) / 1000))
  if (s < 5) return "just now"
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}
export const num = (n: number) => n.toLocaleString()
