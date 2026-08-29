import fs from "node:fs"
import path from "node:path"
import express from "express"

import { createAdminRouter } from "./admin-api.js"
import {
  getActiveProfileMedia,
  getDashboard,
  getLatestSync,
  getMediaAsset,
} from "./database.js"
import {
  avatarMediaDirectory,
  isAllowedMediaMimeType,
  isSafeContentHash,
  isSafeMediaId,
  mediaCacheDirectory,
  resolveMediaCachePath,
} from "./media-cache.js"

function createProfileMediaHandler(database) {
  return (request, response) => {
    const slot = String(request.params.slot || "").toLowerCase()
    if (slot !== "avatar" && slot !== "banner") {
      response.status(404).json({ error: "profile_media_not_found" })
      return
    }
    const asset = getActiveProfileMedia(database, slot)
    const filePath = asset?.activeCachePath
      ? resolveMediaCachePath(asset.activeCachePath)
      : null
    if (
      !asset ||
      !asset.isActive ||
      asset.status !== "ready" ||
      !isSafeContentHash(asset.sha256) ||
      !filePath
    ) {
      response.status(404).json({ error: "profile_media_not_ready" })
      return
    }
    const requestedVersion = request.query.v
    if (
      requestedVersion != null &&
      (!isSafeContentHash(String(requestedVersion)) ||
        requestedVersion !== asset.sha256)
    ) {
      response.status(404).json({ error: "profile_media_version_not_found" })
      return
    }
    let resolvedFilePath
    try {
      resolvedFilePath = fs.realpathSync(filePath)
      const avatarRoot = fs.realpathSync(avatarMediaDirectory)
      if (
        resolvedFilePath !== avatarRoot &&
        !resolvedFilePath.startsWith(`${avatarRoot}${path.sep}`)
      ) {
        response.status(404).json({ error: "profile_media_not_found" })
        return
      }
      if (!fs.statSync(resolvedFilePath).isFile()) throw new Error("not a file")
    } catch {
      response.status(404).json({ error: "profile_media_not_found" })
      return
    }
    const mimeType = String(asset.mimeType || "")
      .split(";", 1)[0]
      .trim()
      .toLowerCase()
    if (!isAllowedMediaMimeType(mimeType)) {
      response.status(404).json({ error: "profile_media_not_found" })
      return
    }
    response.set("Content-Type", mimeType)
    response.set("Content-Security-Policy", "default-src 'none'; sandbox")
    response.set("X-Content-Type-Options", "nosniff")
    response.set(
      "Cache-Control",
      requestedVersion == null
        ? "public, max-age=0, must-revalidate"
        : "public, max-age=31536000, immutable",
    )
    response.set("ETag", `"${asset.sha256}"`)
    response.sendFile(resolvedFilePath)
  }
}

function createCachedMediaHandler(database) {
  return (request, response) => {
    const { id } = request.params
    if (!isSafeMediaId(id)) {
      response.status(404).json({ error: "media_not_found" })
      return
    }

    const asset = getMediaAsset(database, id)
    const filePath = asset?.cachePath
      ? resolveMediaCachePath(asset.cachePath)
      : null
    if (
      !asset ||
      asset.status !== "ready" ||
      !isSafeContentHash(asset.sha256) ||
      !filePath
    ) {
      response.status(404).json({ error: "media_not_ready" })
      return
    }

    const requestedVersion = request.query.v
    if (
      requestedVersion != null &&
      (!isSafeContentHash(String(requestedVersion)) ||
        requestedVersion !== asset.sha256)
    ) {
      response.status(404).json({ error: "media_version_not_found" })
      return
    }

    let stat
    let resolvedFilePath
    try {
      resolvedFilePath = fs.realpathSync(filePath)
      const cacheRoot = fs.realpathSync(mediaCacheDirectory)
      if (
        resolvedFilePath !== cacheRoot &&
        !resolvedFilePath.startsWith(`${cacheRoot}${path.sep}`)
      ) {
        response.status(404).json({ error: "media_not_found" })
        return
      }
      stat = fs.statSync(resolvedFilePath)
    } catch {
      response.status(404).json({ error: "media_not_found" })
      return
    }
    if (!stat.isFile()) {
      response.status(404).json({ error: "media_not_found" })
      return
    }

    const mimeType = String(asset.mimeType || "")
      .split(";", 1)[0]
      .trim()
      .toLowerCase()
    if (!isAllowedMediaMimeType(mimeType)) {
      response.status(404).json({ error: "media_not_found" })
      return
    }
    response.set("Content-Type", mimeType)
    response.set("Content-Security-Policy", "default-src 'none'; sandbox")
    response.set("X-Content-Type-Options", "nosniff")
    response.set(
      "Cache-Control",
      requestedVersion == null
        ? "public, max-age=0, must-revalidate"
        : "public, max-age=31536000, immutable",
    )
    if (asset.sha256) response.set("ETag", `"${asset.sha256}"`)
    if (asset.fetchedAt) {
      const fetchedAt = new Date(asset.fetchedAt)
      if (!Number.isNaN(fetchedAt.getTime()))
        response.set("Last-Modified", fetchedAt.toUTCString())
    }
    response.sendFile(resolvedFilePath)
  }
}

export function createApp({
  database,
  databaseLabel = "data/database/kano.sqlite",
  staticDirectory = null,
  adminMode = "development",
  adminPassword = "",
  openAiKeyConfigured = Boolean(process.env.OPENAI_API_KEY),
}) {
  if (!database) throw new Error("createApp requires a database")

  const app = express()
  app.disable("x-powered-by")
  app.use(express.json({ limit: "32kb" }))

  // Profile slots are stable public URLs while their selected files remain
  // replaceable under the ignored runtime directory.
  app.get("/media/profile/:slot", createProfileMediaHandler(database))

  // The route performs a database lookup instead of accepting a file path.
  app.get("/media/:id", createCachedMediaHandler(database))
  app.use("/media", (_request, response) =>
    response.status(404).json({ error: "media_not_found" }),
  )

  app.get("/api/health", (_request, response) => {
    response.json({
      ok: true,
      service: "kano-status-board",
      database: databaseLabel,
      now: new Date().toISOString(),
      lastSync: getLatestSync(database),
    })
  })

  app.get("/api/dashboard", (request, response) => {
    const requestedDays = Number.parseInt(String(request.query.days ?? "3"), 10)
    const days = Number.isFinite(requestedDays)
      ? Math.min(30, Math.max(1, requestedDays))
      : 3
    response.set("Cache-Control", "no-store")
    response.json(getDashboard(database, { days }))
  })

  app.use(
    "/api/admin",
    createAdminRouter({
      database,
      mode: adminMode,
      adminPassword,
      openAiKeyConfigured,
    }),
  )

  app.use("/api", (_request, response) =>
    response.status(404).json({ error: "api_not_found" }),
  )

  if (staticDirectory && fs.existsSync(staticDirectory)) {
    app.use(express.static(staticDirectory, { index: "index.html" }))
    app.get(/^(?!\/(?:api|media)(?:\/|$)).*/, (_request, response) => {
      response.sendFile(path.join(staticDirectory, "index.html"))
    })
  }

  app.use((error, _request, response, _next) => {
    console.error(error)
    response.status(500).json({ error: "internal_error" })
  })

  return app
}
