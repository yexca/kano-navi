import assert from "node:assert/strict"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"
import test from "node:test"
import { createServer, preview } from "vite"

import { verifySnapshot } from "../scripts/smoke.ts"
import { createApp } from "./app.ts"
import { initializeDatabase } from "./database.ts"
import { listeningPort } from "./http-address.ts"
import { localApiPlugin } from "./local-api.ts"

for (const mode of ["development", "preview"] as const) {
  test(`${mode} serves pages and protected API routes through one listener`, async () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "kano-local-api-"))
    const database = initializeDatabase({ filename: ":memory:" })
    let server
    try {
      fs.mkdirSync(path.join(root, "dist"))
      const html =
        '<!doctype html><html><body><div id="root"></div></body></html>'
      fs.writeFileSync(path.join(root, "index.html"), html)
      fs.writeFileSync(path.join(root, "dist/index.html"), html)
      const app = createApp({
        database,
        adminMode: "production",
        adminPassword: "synthetic-password",
        startWorkflowScheduler: false,
        mcpControlToken: "",
      })
      const config = {
        root,
        configFile: false as const,
        envFile: false as const,
        logLevel: "silent" as const,
        plugins: [localApiPlugin(async () => app)],
        server: { host: "127.0.0.1", port: 0, strictPort: true },
        preview: { host: "127.0.0.1", port: 0, strictPort: true },
      }
      if (mode === "development") {
        server = await createServer(config)
        await server.listen()
      } else server = await preview(config)
      const origin = `http://127.0.0.1:${listeningPort(server.httpServer)}`
      await verifySnapshot(origin, { staticPages: true })
      const missing = await fetch(`${origin}/api/not-a-route`)
      assert.equal(missing.status, 404)
      assert.deepEqual(await missing.json(), { error: "api_not_found" })
      const mcp = await fetch(`${origin}/mcp`)
      assert.equal(mcp.status, 405)
      const page = await fetch(`${origin}/api-page`)
      assert.equal(page.status, 200)
      assert.match(page.headers.get("content-type") || "", /text\/html/u)

      const login = await fetch(`${origin}/api/admin/login`, {
        method: "POST",
        headers: { "content-type": "application/json", origin },
        body: JSON.stringify({ password: "synthetic-password" }),
      })
      assert.equal(login.status, 200)
      const cookie = login.headers.get("set-cookie")?.split(";")[0]
      assert.ok(cookie)
      const configResponse = await fetch(`${origin}/api/admin/config`, {
        headers: { cookie },
      })
      assert.equal(configResponse.status, 200)
    } finally {
      if (server) {
        server.httpServer?.closeAllConnections()
        if ("close" in server) await server.close()
        else
          await new Promise<void>((resolve, reject) =>
            server.httpServer.close((error) =>
              error ? reject(error) : resolve(),
            ),
          )
      }
      database.close()
      fs.rmSync(root, { recursive: true, force: true })
    }
  })
}
