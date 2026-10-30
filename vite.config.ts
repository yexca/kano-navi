import path from "node:path"
import { defineConfig, loadEnv } from "vite"
import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"
import { precompress } from "./scripts/vite-precompress.ts"
import { pagePreload } from "./scripts/vite-page-preload.ts"

export default defineConfig(({ mode }) => {
  const environment = loadEnv(mode, process.cwd(), "")
  const port = Number(environment.PORT || 7657)
  // The Go backend owns /api, /media and /mcp. Vite only serves the frontend
  // and forwards those paths with the browser's Host, so the admin origin
  // guard sees the same origin the page was loaded from.
  const apiPort = Number(environment.KANO_API_PORT || 7658)
  const backend = process.env.KANO_API_ORIGIN || `http://127.0.0.1:${apiPort}`
  const proxy = { "^/(?:api|media|mcp)(?:/|$)": { target: backend } }
  // A Windows or macOS checkout mounted into the development container sends
  // no file events, so the container polls the sources instead. Runtime data,
  // the Go module and local tool state are left out of the polled tree.
  const watch =
    environment.KANO_WATCH_POLLING === "1"
      ? {
          usePolling: true,
          interval: 500,
          binaryInterval: 1000,
          ignored: [
            "**/data/**",
            "**/backend-go/**",
            "**/dist/**",
            "**/.claude/**",
            "**/.codegraph/**",
          ],
        }
      : undefined
  return {
    build: { manifest: true },
    plugins: [
      react(),
      tailwindcss(),
      // The modules src/main.tsx imports for the board.
      pagePreload({
        "/": [
          "src/dashboard/dashboard-app.tsx",
          "src/dashboard/dashboard-i18n.ts",
        ],
      }),
      // Runs after the bundle is written, so index.html already holds the
      // preload script.
      precompress(),
    ],
    server: { port, strictPort: true, proxy, watch },
    preview: { port, strictPort: true, proxy },
    resolve: {
      alias: {
        "@": path.resolve(process.cwd(), "./src"),
      },
    },
  }
})
