import { spawnSync } from "node:child_process"
import { appendFileSync } from "node:fs"
import { pathToFileURL } from "node:url"

// Validation runs in one of two tiers. `pr` selects jobs from the changed
// paths and is what a pull request must pass to merge. `full` runs every
// validation job and is what main and a release must pass.
export const tiers = ["pr", "full"] as const
export type Tier = (typeof tiers)[number]

// Every planned validation job; each job ID is also its plan key. The Gate
// evaluates all of them in both tiers.
export const jobs = [
  "backend",
  "go-builds",
  "go-container",
  "frontend",
  "smoke",
  "docker",
  "go-audit",
  "npm-audit",
] as const
export type Job = (typeof jobs)[number]
export type Plan = Record<Job, boolean>

// The advisory audits query live databases, so only a pull request that
// changes the matching dependency manifest plans one: a new advisory for an
// unchanged dependency never fails an unrelated pull request. The daily
// Dependency audit workflow covers main, so the full tier plans neither and a
// new advisory cannot hold back a release.
export const auditJobs: readonly Job[] = ["go-audit", "npm-audit"]

// The jobs every tier runs unconditionally, beside the planned ones.
export const alwaysRun = ["plan", "style"] as const

// The application parts and the jobs that consume them.
const backendJobs: Job[] = [
  "backend",
  "go-builds",
  "go-container",
  "smoke",
  "docker",
]
const frontendJobs: Job[] = ["frontend", "docker"]

// The files under scripts/ and the planned jobs that run them. Style always
// runs, and it formats every script and runs the scanner, planner and shard
// tests.
// The frontend build type-checks every TypeScript script, so each one selects
// Frontend. An unlisted script selects every job.
export const scriptJobs: Record<string, Job[]> = {
  "check-doc-links.ts": ["frontend"],
  // Style and Frontend run it before their Node checks.
  "check-node.ts": ["frontend"],
  "check-sensitive.ts": ["frontend"],
  "check-sensitive.test.ts": ["frontend"],
  "ci-plan.test.ts": ["frontend"],
  // The API exports, which their drift tests regenerate.
  "export-journey-api.ts": ["frontend"],
  "export-works-api.ts": ["frontend"],
  "export-lineblog.ts": ["frontend"],
  "go-check.mjs": ["go-builds", "go-container"],
  "go-docker-smoke.mjs": ["go-container"],
  "go-test-shard.mjs": ["backend"],
  "go-test-shard.test.mjs": [],
  "go-test-durations.json": ["backend"],
  "golangci-lint.yml": ["backend"],
  "make-help.ts": ["frontend"],
  "package-history-media.ts": ["frontend"],
  "package-lineblog-comments.ts": ["frontend"],
  "package-lineblog.ts": ["frontend"],
  "package-works.ts": ["frontend"],
  // Packaging's phrase dictionary, which the works tests exercise.
  "works-phrases.ts": ["frontend"],
  "privacy-allowlist.json": [],
  "production-smoke.ts": ["frontend", "docker"],
  "run-app.mjs": [],
  "smoke.ts": ["frontend", "smoke", "docker"],
  // Build plugins and the CDN base check loaded by vite.config.ts; the
  // Docker image's build runs them too.
  "cdn-base.ts": frontendJobs,
  "vite-page-preload.ts": frontendJobs,
  "vite-precompress.ts": frontendJobs,
  // Type-checks the scripts and the frontend tests.
  "tsconfig.json": frontendJobs,
}

const documentation =
  /^(docs\/|[^/]+\.md$|backend\/README\.md$|(backend|src|scripts)\/AGENTS\.md$|\.claude\/skills\/|\.github\/SECURITY\.md$|\.github\/ISSUE_TEMPLATE\/)/
// Read by Style alone: formatting, the scanner and the deployment examples
// that no validation job starts.
const styleOnly = /^(\.env\.example|\.gitignore|docker-compose(\.dev)?\.yml)$/
const frontendConfig = /^(index\.html|tsconfig\.json|vite\.config\.ts)$/

// The jobs a changed path selects, or null when it selects every validation
// job.
function jobsFor(path: string): Job[] | null {
  if (documentation.test(path) || styleOnly.test(path)) return []
  if (path.startsWith("scripts/")) {
    const name = path.slice("scripts/".length)
    return Object.hasOwn(scriptJobs, name) ? scriptJobs[name] : null
  }
  // The frontend tests read the static seed to keep its copy in step.
  if (path === "backend/internal/store/sqlite/static-seed.json")
    return [...backendJobs, "frontend"]
  if (/^backend\/go\.(mod|sum)$/.test(path)) return [...backendJobs, "go-audit"]
  // Go container builds this image; the release image has its own Dockerfile.
  if (path === "backend/Dockerfile") return ["go-container"]
  if (path.startsWith("backend/")) return backendJobs
  if (path.startsWith("src/") || path.startsWith("public/")) return frontendJobs
  if (frontendConfig.test(path)) return frontendJobs
  if (path.startsWith("tests/")) return ["frontend"]
  // The release image installs from the manifest and reads its version.
  if (/^package(-lock)?\.json$/.test(path))
    return [...frontendJobs, "npm-audit"]
  if (path.startsWith("docker/") || path === "LICENSE") return ["docker"]
  // Makefile, .nvmrc, workflows, composite actions, Dependabot and anything
  // unclassified: every validation job, but no advisory audit.
  return null
}

export function createPlan({
  tier,
  paths = null,
}: {
  tier: string | undefined
  paths?: string[] | null
}): Plan {
  if (!tiers.includes(tier as Tier)) throw new Error(`Unknown CI tier: ${tier}`)
  const validation = jobs.filter((job) => !auditJobs.includes(job))
  const plan = Object.fromEntries(jobs.map((job) => [job, false])) as Plan
  if (tier === "full") {
    for (const job of validation) plan[job] = true
    return plan
  }
  // A missing or empty diff is not evidence that it is safe to skip validation
  // or an audit.
  if (!paths?.length) {
    for (const job of jobs) plan[job] = true
    return plan
  }
  for (const path of paths) {
    for (const job of jobsFor(path) ?? validation) plan[job] = true
  }
  return plan
}

type Results = Record<string, { result?: string } | undefined>

export function validateResults(plan: unknown, results: Results) {
  if (
    !plan ||
    typeof plan !== "object" ||
    jobs.some(
      (job) => typeof (plan as Record<string, unknown>)[job] !== "boolean",
    )
  )
    throw new Error("Missing or invalid CI plan")
  for (const job of alwaysRun)
    if (results[job]?.result !== "success")
      throw new Error(
        `CI job ${job} must succeed: ${results[job]?.result ?? "missing"}`,
      )
  for (const job of jobs) {
    const result = results[job]?.result
    if (
      result !== "success" &&
      !((plan as Plan)[job] === false && result === "skipped")
    )
      throw new Error(
        `CI job ${job} did not satisfy its plan: ${result ?? "missing"}`,
      )
  }
}

function changedPaths(base: string | undefined): string[] | null {
  if (!/^[a-f0-9]{40}$/.test(base || "")) return null
  // Without rename detection a renamed file lists both its old and new path.
  const diff = spawnSync(
    "git",
    ["diff", "--name-only", "--no-renames", "-z", base as string, "HEAD"],
    { encoding: "utf8", maxBuffer: 16 * 1024 * 1024 },
  )
  if (diff.status !== 0) return null
  return diff.stdout.split("\0").filter(Boolean)
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  if (process.argv[2] === "check") {
    validateResults(
      JSON.parse(process.env.CI_PLAN || "null"),
      JSON.parse(process.env.CI_RESULTS || "{}"),
    )
    console.log("Every planned job succeeded.")
  } else if (process.argv[2] === "plan") {
    const tier = process.env.CI_TIER
    const paths = tier === "pr" ? changedPaths(process.env.CI_BASE_SHA) : null
    const plan = createPlan({ tier, paths })
    const outputs =
      [
        `plan=${JSON.stringify(plan)}`,
        ...jobs.map((job) => `${job}=${plan[job]}`),
      ].join("\n") + "\n"
    if (process.env.GITHUB_OUTPUT)
      appendFileSync(process.env.GITHUB_OUTPUT, outputs)
    console.log(outputs.trim())
  } else {
    throw new Error("usage: node scripts/ci-plan.ts <plan|check>")
  }
}
