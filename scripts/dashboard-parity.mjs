#!/usr/bin/env node

// Compare one synthetic SQLite snapshot through the TypeScript public
// projection and the read-only Go projection. This deliberately uses a fixed
// clock so generatedAt and time-window fields are part of the comparison.
import { existsSync, mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { spawnSync } from "node:child_process"

const root = join(import.meta.dirname, "..")
const go = process.env.GO || "go"
const now = new Date("2026-10-11T03:00:00.000+09:00")

let databaseModule
try {
  databaseModule = await import("../server/database.ts")
} catch (error) {
  // A developer checkout may intentionally omit node_modules.  CI runs
  // `make install` first, so an import failure there is a real gate failure,
  // never a permitted skip.
  if (
    error?.code === "ERR_MODULE_NOT_FOUND" &&
    !existsSync(join(root, "node_modules"))
  ) {
    const smoke = spawnSync(
      go,
      [
        "test",
        "./cmd/kano-parity",
        "-run",
        "^TestRenderReadsSyntheticSnapshot$",
        "-count=1",
      ],
      {
        cwd: join(root, "backend-go"),
        encoding: "utf8",
        maxBuffer: 4 * 1024 * 1024,
      },
    )
    if (smoke.stdout) process.stdout.write(smoke.stdout)
    if (smoke.stderr) process.stderr.write(smoke.stderr)
    if (smoke.error || smoke.status !== 0) {
      console.error(
        `dashboard-parity: dependency-free Go smoke failed (exit status ${smoke.status ?? "unknown"})`,
      )
      process.exit(1)
    }
    console.error(
      "dashboard-parity: SKIPPED (Node dependencies are unavailable; Go synthetic snapshot smoke passed; run npm ci before the live TypeScript comparison)",
    )
    process.exit(2)
  }
  throw error
}
const { initializeDatabase, getDashboard } = databaseModule
const { publicEvent, publicProfileMediaSlots, publicSyncRun } =
  await import("../server/public-view.ts")

const directory = mkdtempSync(join(tmpdir(), "kano-dashboard-parity-"))
const databasePath = join(directory, "snapshot.sqlite")
let database
try {
  database = initializeDatabase({ seed: false, filename: databasePath })
  database.exec(`
    INSERT INTO profiles (id, display_name, romanized_name, bio, x_url, youtube_url, updated_at)
      VALUES ('profile', '鹿乃まほろ', 'Kano Mahoro', 'synthetic', 'https://x.com/kano_2525', 'https://youtube.com/@kano', '2026-10-11T00:00:00.000Z');
    INSERT INTO posts (id, source, type, text, published_at, url, likes, reposts, replies)
      VALUES ('post-1', 'x', 'daily', 'synthetic', '2026-10-10T12:00:00.000Z', 'https://x.com/kano_2525/status/1', 1, 2, 3);
    INSERT INTO videos (id, source, title, published_at, scheduled_at, url, kind, is_upcoming)
      VALUES ('parity-video', 'youtube', 'Synthetic stream', '2026-10-10T00:00:00.000Z', '2026-10-11T04:00:00Z', 'https://www.youtube.com/watch?v=parity-video', 'stream', 1);
    INSERT INTO events (id, source, title, starts_on, starts_at, ends_at, timezone, time_precision, status, event_type, url, provenance, manual_locked, cancellation_status, created_at, updated_at)
      VALUES ('parity-event', 'youtube', 'Synthetic stream', '2026-10-11', '2026-10-11T04:00:00Z', '2026-10-11T05:00:00Z', 'Asia/Tokyo', 'exact', 'scheduled', 'stream', 'https://youtu.be/parity-video?si=fixture', 'automatic', 0, 'none', '', '');
    INSERT INTO focus (id, video_id, date_label, title, description, updated_at)
      VALUES (1, 'parity-video', '2026-10-11', 'Focus', 'Synthetic focus', '2026-10-11T00:00:00.000Z');
    INSERT INTO timeline (id, year, title, detail, sort_order) VALUES (1, '2026', 'Timeline', 'Synthetic timeline', 1);
    INSERT INTO resources (id, title, detail, icon, tone, url, sort_order) VALUES (1, 'Resource', 'Synthetic resource', 'link', 'blue', 'https://example.invalid/resource', 1);
    INSERT INTO app_settings (key, value, updated_at) VALUES ('dashboard_revision', '7', '2026-10-11T00:00:00.000Z');
  `)

  const snapshot = getDashboard(database, { days: 3, now })
  const visitorEvent = (event) =>
    publicEvent(event, ["manualLocked", "isUpcoming"])
  const typescriptProjection = {
    ...snapshot,
    profileMedia: publicProfileMediaSlots(snapshot.profileMedia),
    events: snapshot.events.map(visitorEvent),
    summary: {
      ...snapshot.summary,
      nextEvent: snapshot.summary.nextEvent
        ? visitorEvent(snapshot.summary.nextEvent)
        : null,
    },
    meta: { ...snapshot.meta, lastSync: publicSyncRun(snapshot.meta.lastSync) },
  }
  const goResult = spawnSync(
    go,
    [
      "run",
      "./cmd/kano-parity",
      "-database",
      databasePath,
      "-days",
      "3",
      "-now",
      now.toISOString(),
    ],
    {
      cwd: join(root, "backend-go"),
      encoding: "utf8",
      maxBuffer: 8 * 1024 * 1024,
    },
  )
  if (goResult.error || goResult.status !== 0) {
    const detail = goResult.error
      ? `spawn error: ${goResult.error.code || "unknown"}`
      : `exit status ${goResult.status}`
    throw new Error(
      `Go parity projection failed (${detail}); command: ${go} run ./cmd/kano-parity`,
    )
  }
  const goProjection = JSON.parse(goResult.stdout)
  const differences = []
  compare(typescriptProjection, goProjection, "$", differences)
  if (differences.length > 0) {
    console.error("dashboard-parity: FAILED")
    console.error(differences.join("\n"))
    process.exitCode = 1
  } else {
    console.log("dashboard-parity: PASS (TypeScript and Go public JSON match)")
  }
} finally {
  database?.close()
  rmSync(directory, { recursive: true, force: true })
}

function compare(left, right, path, differences) {
  if (differences.length >= 20) return
  if (
    left === null ||
    right === null ||
    typeof left !== "object" ||
    typeof right !== "object"
  ) {
    if (Object.is(left, right)) return
    differences.push(
      `${path}: ${JSON.stringify(left)} !== ${JSON.stringify(right)}`,
    )
    return
  }
  if (Array.isArray(left) || Array.isArray(right)) {
    if (
      !Array.isArray(left) ||
      !Array.isArray(right) ||
      left.length !== right.length
    ) {
      differences.push(`${path}: array length/type differs`)
      return
    }
    left.forEach((value, index) =>
      compare(value, right[index], `${path}[${index}]`, differences),
    )
    return
  }
  const keys = new Set([...Object.keys(left), ...Object.keys(right)])
  for (const key of [...keys].sort()) {
    if (!(key in left) || !(key in right)) {
      differences.push(
        `${path}.${key}: field missing from ${key in left ? "Go" : "TypeScript"}`,
      )
      continue
    }
    compare(left[key], right[key], `${path}.${key}`, differences)
  }
}
