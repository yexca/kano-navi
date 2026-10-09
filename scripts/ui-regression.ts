// Disposable local browser fixture. Never imports server/index.ts or .env.
import path from "node:path"
import type { Request, Response } from "express"
import { createServer } from "vite"
import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"
import { createApp } from "../server/app.ts"
import {
  initializeDatabase,
  upsertEvents,
  upsertPosts,
  upsertAssets,
  updateScheduleAssetManualReview,
} from "../server/database.ts"
const database = initializeDatabase({ seed: true, filename: ":memory:" })
const date = new Intl.DateTimeFormat("en-CA", {
  timeZone: "Asia/Tokyo",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
}).format(new Date())
upsertEvents(database, [
  {
    id: "fixture-event",
    source: "manual",
    title: "Synthetic calendar activity",
    starts_on: date,
    url: "https://example.invalid/activity",
  },
])
const monday = new Date(`${date}T00:00:00Z`)
monday.setUTCDate(monday.getUTCDate() - ((monday.getUTCDay() + 6) % 7))
const previousWeek = new Date(monday.getTime() - 7 * 86400000)
  .toISOString()
  .slice(0, 10)
upsertEvents(
  database,
  Array.from({ length: 101 }, (_, index) => ({
    id: `fixture-history-${index}`,
    source: "x",
    title: `Synthetic history ${index}`,
    starts_on: previousWeek,
  })),
)
upsertEvents(database, [
  {
    id: "fixture-next-week",
    source: "x",
    title: "Synthetic next week activity",
    starts_on: new Date(monday.getTime() + 7 * 86400000)
      .toISOString()
      .slice(0, 10),
  },
])
upsertPosts(database, [
  {
    id: "fixture-post",
    text: "Synthetic local snapshot",
    source: "x",
    published_at: new Date().toISOString(),
    url: "https://x.com/example/status/fixture",
    media_urls: [],
  },
])
upsertAssets(database, [
  {
    id: "fixture-board",
    kind: "schedule",
    url: "https://pbs.twimg.com/media/fixture.png",
    source_url: "https://x.com/example/status/fixture",
    period_start: date,
    period_end: date,
    period_basis: "explicit",
  },
])
updateScheduleAssetManualReview(database, "fixture-board", {
  status: "schedule",
  reason: "Synthetic manual review",
})
const app = createApp({
  database,
  mcpEnabled: false,
  mcpControlToken: "",
  syncJobs: { list: () => [], get: () => null, isBusy: () => false },
  adminFetchImpl: async () => {
    throw new Error("fixture disallows external requests")
  },
})
const vite = await createServer({
  configFile: false,
  root: process.cwd(),
  plugins: [
    react(),
    tailwindcss(),
    {
      name: "isolated-regression",
      configureServer(server) {
        server.middlewares.use(async (request, response, next) => {
          if (
            request.url?.startsWith("/api/") ||
            request.url?.startsWith("/media/")
          )
            return app(request as Request, response as Response, next)
          if (request.url === "/__regression") {
            response.setHeader("Content-Type", "text/html")
            response.end(
              await server.transformIndexHtml(
                request.url,
                `<html><body><div id="root"></div><script type="module" src="/scripts/fixtures/polling-regression.jsx"></script></body></html>`,
              ),
            )
            return
          }
          if (
            request.url === "/__mobile" ||
            request.url === "/__mobile-admin"
          ) {
            response.setHeader("Content-Type", "text/html")
            response.end(
              `<html><body style="margin:0"><iframe style="width:390px;height:844px;border:0" src="${request.url.endsWith("admin") ? "/admin" : "/"}"></iframe></body></html>`,
            )
            return
          }
          next()
        })
      },
    },
  ],
  resolve: { alias: { "@": path.resolve("src") } },
  server: {
    host: "127.0.0.1",
    port: 17657,
    strictPort: true,
    watch: { ignored: ["**/data/**", "**/dist/**"] },
  },
})
await vite.listen()
console.log("Disposable browser fixture: http://127.0.0.1:17657/__regression")
const cleanup = async () => {
  await vite.close()
  database.close()
  process.exit(0)
}
process.once("SIGINT", cleanup)
process.once("SIGTERM", cleanup)
