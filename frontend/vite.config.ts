import path from "node:path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

export default defineConfig({
  base: "/",
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": path.resolve(import.meta.dirname, "./src") } },
  build: { outDir: "../internal/webui/dist", emptyOutDir: true, chunkSizeWarningLimit: 1000, assetsDir: "assets" },
  server: {
    proxy: {
      "/dashboard/api": {
        target: "http://localhost:8080",
        headers: { "Remote-User": "dev" },
      },
    },
  },
})
