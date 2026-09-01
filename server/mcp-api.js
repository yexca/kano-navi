import crypto from "node:crypto"
import express from "express"
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js"
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js"
import { z } from "zod"

import {
  bumpDashboardRevision,
  getDashboard,
  getDashboardRevision,
  getLatestSync,
  getSyncRun,
  listAdminEventsPage,
  listSyncRuns,
} from "./database.js"

const CONTROL_TOOLS = new Set([
  "dashboard_request_reload",
  "sync_start",
  "scan_run_automatic",
])

const publicEventFields = [
  "id",
  "source",
  "title",
  "detail",
  "startsOn",
  "startsAt",
  "endsAt",
  "timezone",
  "timePrecision",
  "status",
  "eventType",
  "url",
  "provenance",
]

function constantTimeTokenMatch(actual, expected) {
  const actualBuffer = Buffer.from(String(actual || ""))
  const expectedBuffer = Buffer.from(String(expected || ""))
  return (
    actualBuffer.length === expectedBuffer.length &&
    actualBuffer.length > 0 &&
    crypto.timingSafeEqual(actualBuffer, expectedBuffer)
  )
}

function authorizationToken(request) {
  const value = String(request.headers.authorization || "")
  const match = value.match(/^Bearer\s+([^\s]+)$/iu)
  return match?.[1] || null
}

function authState(request, controlToken) {
  const token = authorizationToken(request)
  if (!token) return { supplied: false, valid: false }
  return {
    supplied: true,
    valid: constantTimeTokenMatch(token, controlToken),
  }
}

function unauthorized(response) {
  response.set("WWW-Authenticate", 'Bearer realm="kano-mcp"')
  response.status(401).json({ error: "mcp_control_auth_required" })
}

function forbidden(response) {
  response.status(403).json({ error: "mcp_scope_required" })
}

function jsonToolResult(value) {
  return {
    content: [
      {
        type: "text",
        text: JSON.stringify(value),
      },
    ],
    structuredContent: value,
  }
}

function publicEvent(event) {
  return Object.fromEntries(
    publicEventFields
      .filter((field) =>
        Object.prototype.hasOwnProperty.call(event || {}, field),
      )
      .map((field) => [field, event[field]]),
  )
}

function publicSyncRun(run) {
  if (!run) return null
  return {
    id: run.id,
    source: run.source,
    startedAt: run.startedAt,
    finishedAt: run.finishedAt,
    status: run.status,
    message: run.message || null,
  }
}

function publicJob(job) {
  if (!job) return null
  const source = String(job.source || "")
  const kind = job.kind || (source === "schedule" ? "scan" : "sync")
  return {
    id: job.id ?? job.jobId ?? null,
    kind,
    status: job.status,
    createdAt: job.createdAt,
    startedAt: job.startedAt,
    finishedAt: job.finishedAt,
  }
}

function publicSchedulePage(database, options = {}) {
  // Deleted tombstones, source identities, extraction IDs, and lock flags are
  // operator metadata. Public MCP reads expose only the same event facts that
  // a visitor needs to understand the calendar.
  const page = listAdminEventsPage(database, {
    ...options,
    includeDeleted: false,
  })
  const items = page.items.map(publicEvent)
  return { ...page, items, events: items }
}

function publicDashboard(database, days) {
  const boundedDays = Math.min(30, Math.max(1, Math.trunc(Number(days)) || 3))
  const dashboard = getDashboard(database, { days: boundedDays })
  return {
    ...dashboard,
    events: (dashboard.events || []).map(publicEvent),
    meta: {
      ...(dashboard.meta || {}),
      lastSync: publicSyncRun(dashboard.meta?.lastSync),
    },
  }
}

function createMcpServer({ database, jobs, controlAuthorized }) {
  const server = new McpServer(
    {
      name: "kano-status-board",
      version: "0.1.0",
    },
    {
      instructions:
        "Public tools are read-only. Control tools can refresh the dashboard or start automatic jobs; human confirmation and editing remain available only in /admin.",
    },
  )

  server.registerTool(
    "dashboard_read",
    {
      title: "Read dashboard",
      description:
        "Read the public dashboard snapshot without triggering sync.",
      inputSchema: {
        days: z.number().int().min(1).max(30).optional(),
      },
      annotations: { readOnlyHint: true, openWorldHint: false },
    },
    async ({ days }) => jsonToolResult(publicDashboard(database, days)),
  )

  server.registerTool(
    "schedule_list",
    {
      title: "List schedules",
      description:
        "List schedule records with read-only filters and pagination.",
      inputSchema: {
        page: z.number().int().min(1).optional(),
        pageSize: z.number().int().min(1).max(100).optional(),
        search: z.string().max(120).optional(),
        provenance: z.enum(["automatic", "manual"]).optional(),
        source: z.string().max(40).optional(),
        from: z
          .string()
          .regex(/^\d{4}-\d{2}-\d{2}$/u)
          .optional(),
        to: z
          .string()
          .regex(/^\d{4}-\d{2}-\d{2}$/u)
          .optional(),
      },
      annotations: { readOnlyHint: true, openWorldHint: false },
    },
    async ({ page, pageSize, search, provenance, source, from, to }) =>
      jsonToolResult(
        publicSchedulePage(database, {
          page,
          pageSize,
          search,
          provenance,
          source,
          from,
          to,
        }),
      ),
  )

  server.registerTool(
    "posts_list",
    {
      title: "List posts",
      description: "Read recent public posts from the stored snapshot.",
      inputSchema: {
        days: z.number().int().min(1).max(30).optional(),
        limit: z.number().int().min(1).max(100).optional(),
      },
      annotations: { readOnlyHint: true, openWorldHint: false },
    },
    async ({ days, limit }) => {
      const dashboard = publicDashboard(database, days)
      const boundedLimit = Math.min(
        100,
        Math.max(1, Math.trunc(Number(limit)) || 50),
      )
      return jsonToolResult({
        posts: dashboard.posts.slice(0, boundedLimit),
        days: dashboard.meta.postWindowDays,
        generatedAt: dashboard.meta.generatedAt,
      })
    },
  )

  server.registerTool(
    "health_read",
    {
      title: "Read health",
      description:
        "Read service health and the latest synchronization summary.",
      inputSchema: {},
      annotations: { readOnlyHint: true, openWorldHint: false },
    },
    async () =>
      jsonToolResult({
        ok: true,
        service: "kano-status-board",
        now: new Date().toISOString(),
        revision: getDashboardRevision(database),
        lastSync: publicSyncRun(getLatestSync(database)),
      }),
  )

  // Status is a read-only observation, so it remains available to public
  // clients. Detailed mutation controls below still require the bearer token.
  server.registerTool(
    "sync_status",
    {
      title: "Read synchronization status",
      description:
        "Read the current synchronization job status without starting a job.",
      inputSchema: {
        jobId: z.string().max(100).optional(),
        limit: z.number().int().min(1).max(100).optional(),
      },
      annotations: { readOnlyHint: true, openWorldHint: false },
    },
    async ({ jobId, limit }) => {
      if (jobId) {
        const job = jobs?.get(jobId) || getSyncRun(database, jobId)
        return jsonToolResult({ job: publicJob(job) })
      }
      const persisted = jobs?.list(limit) || []
      if (persisted.length)
        return jsonToolResult({ jobs: persisted.map(publicJob) })
      return jsonToolResult({
        jobs: listSyncRuns(database, { limit }).map((run) => publicJob(run)),
      })
    },
  )

  if (!controlAuthorized) return server

  server.registerTool(
    "dashboard_request_reload",
    {
      title: "Request dashboard reload",
      description:
        "Advance the public dashboard revision so connected pages reload their stored data. This does not fetch external sources.",
      inputSchema: {},
      annotations: { destructiveHint: false, openWorldHint: false },
    },
    async () => jsonToolResult({ revision: bumpDashboardRevision(database) }),
  )

  server.registerTool(
    "sync_start",
    {
      title: "Start synchronization",
      description:
        "Start one asynchronous server-side synchronization job. Existing snapshots are retained when a source fails.",
      inputSchema: {},
      annotations: { destructiveHint: false, openWorldHint: true },
    },
    async () => {
      const result = jobs.start("sync", "mcp")
      return jsonToolResult(result)
    },
  )

  server.registerTool(
    "scan_run_automatic",
    {
      title: "Run automatic schedule scan",
      description:
        "Run keyword and visual schedule extraction asynchronously. It never confirms, edits, deletes, or selects human-managed records.",
      inputSchema: {},
      annotations: { destructiveHint: false, openWorldHint: true },
    },
    async () => jsonToolResult(jobs.start("scan", "mcp")),
  )

  return server
}

function requestUsesControlTool(body) {
  const messages = Array.isArray(body) ? body : [body]
  return messages.some(
    (message) =>
      message?.method === "tools/call" &&
      CONTROL_TOOLS.has(String(message?.params?.name || "")),
  )
}

/**
 * Mount a stateless Streamable HTTP MCP endpoint.  Stateless operation keeps
 * credentials and authorization decisions request-scoped; no MCP session or
 * browser cookie is accepted here.
 */
export function createMcpRouter({
  database,
  jobs,
  enabled = true,
  controlToken = process.env.MCP_CONTROL_TOKEN || "",
} = {}) {
  const router = express.Router()
  router.use((request, response, next) => {
    response.set("Cache-Control", "no-store")
    response.set("X-Content-Type-Options", "nosniff")
    response.set("Vary", "Authorization")
    if (!enabled) {
      response.status(404).json({ error: "mcp_disabled" })
      return
    }
    if (request.method === "OPTIONS") {
      response.set("Allow", "POST")
      response.status(204).end()
      return
    }
    next()
  })

  router.get("/", (_request, response) => {
    response.set("Allow", "POST")
    response.status(405).json({ error: "mcp_get_not_supported" })
  })
  router.delete("/", (_request, response) => {
    response.set("Allow", "POST")
    response.status(405).json({ error: "mcp_delete_not_supported" })
  })
  router.post("/", async (request, response) => {
    const state = authState(request, controlToken)
    const controlRequest = requestUsesControlTool(request.body)
    if (state.supplied && !state.valid) {
      unauthorized(response)
      return
    }
    if (controlRequest && (!controlToken || !state.valid)) {
      unauthorized(response)
      return
    }

    const server = createMcpServer({
      database,
      jobs,
      controlAuthorized: Boolean(controlToken && state.valid),
    })
    const transport = new StreamableHTTPServerTransport({
      sessionIdGenerator: undefined,
      enableJsonResponse: true,
    })
    try {
      await server.connect(transport)
      await transport.handleRequest(request, response, request.body)
    } catch (error) {
      if (!response.headersSent) {
        response.status(500).json({
          jsonrpc: "2.0",
          error: { code: -32603, message: "MCP request failed" },
          id: null,
        })
      }
    } finally {
      await transport.close().catch(() => {})
      await server.close().catch(() => {})
    }
  })
  return router
}

export { CONTROL_TOOLS, constantTimeTokenMatch }
