import { Button } from "@/components/ui/button"

export default function Landing() {
  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-6 p-6 text-center">
      <div className="flex items-center gap-3">
        <span className="size-4 rounded-full bg-emerald-500" />
        <h1 className="text-4xl font-semibold tracking-tight">spotis</h1>
      </div>
      <p className="text-muted-foreground">One Spotify app, as many as you need.</p>
      <Button asChild size="lg">
        <a href="/dashboard">Log in</a>
      </Button>
    </div>
  )
}
