import { execFileSync } from "node:child_process"
import { setTimeout as delay } from "node:timers/promises"

import { verifySnapshot } from "./smoke.ts"

const image = process.argv[2] || "kano-navi:ci"
const dockerExecutable = process.env.DOCKER || "docker"

function docker(args) {
  return execFileSync(dockerExecutable, args, {
    encoding: "utf8",
    timeout: 30000,
    maxBuffer: 1024 * 1024,
    stdio: ["ignore", "pipe", "pipe"],
  }).trim()
}

async function runProductionSmoke() {
  let container
  try {
    container = docker([
      "create",
      "--pull",
      "never",
      "--publish",
      "127.0.0.1::7657",
      "--env",
      "APP_MODE=production",
      "--env",
      "ADMIN_PASSWORD=synthetic-password",
      "--env",
      "LLM_SECRETS_KEY=synthetic-encryption-key",
      "--env",
      "WORKFLOW_SCHEDULER_ENABLED=0",
      "--env",
      "MCP_CONTROL_TOKEN=",
      image,
    ])
    docker(["start", container])
    const publishedAddress = docker(["port", container, "7657/tcp"])
    assertLoopbackAddress(publishedAddress)
    const origin = `http://${publishedAddress}`
    let healthy = false
    for (let attempt = 0; attempt < 30; attempt += 1) {
      if (
        docker(["inspect", "--format", "{{.State.Running}}", container]) !==
        "true"
      )
        throw new Error("Production container exited before health was ready")
      try {
        const response = await fetch(`${origin}/api/health`, {
          signal: AbortSignal.timeout(1000),
          redirect: "error",
        })
        if (response.ok && (await response.json()).ok === true) {
          healthy = true
          break
        }
      } catch {
        // Startup may still be creating SQLite; retry within the fixed budget.
      }
      await delay(1000)
    }
    if (!healthy) throw new Error("Production health did not become ready")
    await verifySnapshot(origin, { staticPages: true })
    console.log(
      "Production smoke passed (entry point, fresh volume, API, pages, auth).",
    )
  } finally {
    if (container) docker(["rm", "--force", "--volumes", container])
  }
}

function assertLoopbackAddress(address) {
  if (!/^127\.0\.0\.1:\d+$/u.test(address))
    throw new Error("Docker did not publish the expected loopback port")
}

runProductionSmoke().catch((error) => {
  console.error(error.message)
  process.exitCode = 1
})
