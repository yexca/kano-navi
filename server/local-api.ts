import type { Express, Request, Response } from "express"
import type { Plugin, PreviewServer, ViteDevServer } from "vite"

import path from "node:path"
import { pathToFileURL } from "node:url"

// Load the Node entry point directly, outside Vite's config bundle. It creates
// the API and validates authentication without opening a second HTTP listener.
async function loadRuntimeApp(root: string): Promise<Express> {
  const entry = pathToFileURL(path.resolve(root, "server/index.ts")).href
  const { app } = await import(entry)
  return app
}

export function localApiPlugin(
  loadApp: (root: string) => Promise<Express> = loadRuntimeApp,
): Plugin {
  async function attachApi(server: ViteDevServer | PreviewServer) {
    const app = await loadApp(server.config.root)
    server.middlewares.use((request, response, next) => {
      const pathname = (request.url || "/").split("?")[0]
      if (/^\/(?:api|media|mcp)(?:\/|$)/u.test(pathname))
        app(request as Request, response as Response, next)
      else next()
    })
  }

  return {
    name: "kano-local-api",
    apply: "serve",
    configureServer: attachApi,
    configurePreviewServer: attachApi,
  }
}
