#!/usr/bin/env node

// Build the Go backend and run it from the repository root, where it reads
// .env, data/ and dist/. `dev` and `preview` also start Vite, which serves the
// frontend on PORT and proxies /api, /media and /mcp to the backend.
//
//   dev [vite args]      backend on KANO_API_PORT + Vite dev server
//   preview [vite args]  backend on KANO_API_PORT + Vite preview of dist/
//   server               backend only on KANO_API_PORT (pair with `vite`)
//   start                the whole site from the backend on PORT
//   seed | sync | migrate-llm [flags]  one-shot maintenance commands
import { spawn, spawnSync } from "node:child_process"
import { join } from "node:path"

const root = join(import.meta.dirname, "..")
const moduleRoot = join(root, "backend-go")
const go = process.env.GO || "go"
const binary = join(
  moduleRoot,
  "bin",
  process.platform === "win32" ? "kano-go.exe" : "kano-go",
)
const [first = "dev", ...rest] = process.argv.slice(2)
const modes = [
  "dev",
  "preview",
  "server",
  "start",
  "seed",
  "sync",
  "migrate-llm",
]
const mode = modes.includes(first) ? first : "dev"
const args = modes.includes(first) ? rest : process.argv.slice(2)

try {
  // Only KANO_API_PORT is needed here; variables already set always win.
  process.loadEnvFile(join(root, ".env"))
} catch {
  // A missing .env is normal: the environment may carry the configuration.
}
const apiPort = Number(process.env.KANO_API_PORT || 7658)
if (!Number.isInteger(apiPort) || apiPort < 1 || apiPort > 65535) {
  console.error("run-app: KANO_API_PORT must be a port number")
  process.exit(2)
}

const build = spawnSync(
  go,
  ["build", "-trimpath", "-o", binary, "./cmd/kano-go"],
  { cwd: moduleRoot, stdio: "inherit" },
)
if (build.error || build.status !== 0) {
  console.error(
    "run-app: the Go backend could not be built (is Go installed and on PATH?)",
  )
  process.exit(build.status || 1)
}

const children = []
let stopping = false

function stop(code) {
  if (stopping) return
  stopping = true
  for (const child of children) if (child.exitCode == null) child.kill()
  process.exitCode = code
}

function start(command, commandArgs, env = {}) {
  const child = spawn(command, commandArgs, {
    cwd: root,
    stdio: "inherit",
    env: { ...process.env, ...env },
  })
  children.push(child)
  child.on("error", () => stop(1))
  // Either process ending stops the other, so a failed backend start (for
  // example a missing ADMIN_PASSWORD) is not hidden behind a running Vite.
  child.on("exit", (code) => stop(code ?? 1))
  return child
}

for (const signal of ["SIGINT", "SIGTERM"]) process.on(signal, () => stop(0))

if (mode === "start") {
  start(binary, ["serve", ...args])
} else if (["seed", "sync", "migrate-llm"].includes(mode)) {
  start(binary, [mode, ...args])
} else {
  start(binary, ["serve", "-listen", `127.0.0.1:${apiPort}`])
  if (mode !== "server") {
    start(
      process.execPath,
      [
        join(root, "node_modules", "vite", "bin", "vite.js"),
        ...(mode === "preview" ? ["preview"] : []),
        ...args,
      ],
      { KANO_API_ORIGIN: `http://127.0.0.1:${apiPort}` },
    )
  }
}
