import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

export function Landing() {
  return (
    <main className="min-h-screen grid place-items-center p-4">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle className="text-2xl">Spotify Proxy</CardTitle>
          <CardDescription>
            One real Spotify app, any number of virtual apps. Each gets its own credentials and talks to Spotify's
            OAuth and Web API through this server.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {/* /dashboard is behind Traefik forward-auth (Tinyauth); it redirects to login and back here. */}
          <Button className="w-full" onClick={() => (window.location.href = "/dashboard")}>
            Log in to the dashboard
          </Button>
        </CardContent>
      </Card>
    </main>
  )
}
