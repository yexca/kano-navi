import path from "node:path"
import { defineConfig, loadEnv } from "vite"
import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"
import { localApiPlugin } from "./server/local-api.ts"

export default defineConfig(({ mode }) => {
  const environment = loadEnv(mode, process.cwd(), "")
  const port = Number(environment.PORT || 7657)
  return {
    build: { manifest: true },
    plugins: [react(), tailwindcss(), localApiPlugin()],
    server: { port, strictPort: true },
    preview: { port, strictPort: true },
    resolve: {
      alias: {
        "@": path.resolve(process.cwd(), "./src"),
      },
    },
  }
})
