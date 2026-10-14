import path from "node:path"
import { defineConfig, loadEnv } from "vite"
import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"

export default defineConfig(({ mode }) => {
  const environment = loadEnv(mode, process.cwd(), "")
  const port = Number(environment.PORT || 7657)
  // The Go backend owns /api, /media and /mcp. Vite only serves the frontend
  // and forwards those paths with the browser's Host, so the admin origin
  // guard sees the same origin the page was loaded from.
  const apiPort = Number(environment.KANO_API_PORT || 7658)
  const backend = process.env.KANO_API_ORIGIN || `http://127.0.0.1:${apiPort}`
  const proxy = { "^/(?:api|media|mcp)(?:/|$)": { target: backend } }
  return {
    build: { manifest: true },
    plugins: [react(), tailwindcss()],
    server: { port, strictPort: true, proxy },
    preview: { port, strictPort: true, proxy },
    resolve: {
      alias: {
        "@": path.resolve(process.cwd(), "./src"),
      },
    },
  }
})
