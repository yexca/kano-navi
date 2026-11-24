import assert from "node:assert/strict"
import { readdirSync } from "node:fs"
import test from "node:test"

import {
  alwaysRun,
  auditJobs,
  createPlan,
  jobs,
  scriptJobs,
  validateResults,
  type Plan,
} from "./ci-plan.ts"

const pr = (paths: string[] | null) => createPlan({ tier: "pr", paths })
const validation = jobs.filter((job) => !auditJobs.includes(job))
// Whether a plan runs every validation job, whatever it decides on audits.
const validates = (plan: Plan) => validation.every((job) => plan[job])
// The jobs a PR selects, in plan order.
const selected = (...paths: string[]) => jobs.filter((job) => pr(paths)[job])
const resultsFor = (plan: Plan) => ({
  plan: { result: "success" },
  style: { result: "success" },
  ...Object.fromEntries(
    jobs.map((job) => [job, { result: plan[job] ? "success" : "skipped" }]),
  ),
})

test("documentation-only PRs skip planned jobs while unknown paths require full validation", () => {
  assert.deepEqual(
    selected(
      "docs/development/testing.md",
      "README.md",
      "AGENTS.md",
      "backend/AGENTS.md",
      "src/AGENTS.md",
      "scripts/AGENTS.md",
      ".claude/skills/release/SKILL.md",
      ".github/SECURITY.md",
      "backend/README.md",
      ".github/ISSUE_TEMPLATE/bug_report.yml",
      ".env.example",
      "docker-compose.yml",
    ),
    [],
  )
  for (const paths of [
    null,
    [],
    ["new-runtime/settings.json"],
    ["Makefile"],
    [".nvmrc"],
    [".github/workflows/validate.yml"],
    [".github/actions/setup-node/action.yml"],
    [".github/dependabot.yml"],
    ["scripts/ci-plan.ts"],
    ["scripts/future-check.ts"],
    ["scripts/nested/smoke.ts"],
    ["docs/overview.md", "new-runtime/settings.json"],
  ]) {
    assert.ok(validates(pr(paths)), String(paths))
  }
})

test("application paths select the jobs that consume them", () => {
  for (const path of [
    "backend/internal/httpapi/server.go",
    "backend/internal/store/sqlite/bootstrap.sql",
  ])
    assert.deepEqual(
      selected(path),
      ["backend", "go-builds", "go-container", "smoke", "docker"],
      path,
    )
  assert.deepEqual(selected("backend/internal/store/sqlite/static-seed.json"), [
    "backend",
    "go-builds",
    "go-container",
    "frontend",
    "smoke",
    "docker",
  ])
  assert.deepEqual(selected("backend/Dockerfile"), ["go-container"])
  for (const path of [
    "src/main.tsx",
    "public/robots.txt",
    "public/assets/history/catalog.json",
    "index.html",
    "vite.config.ts",
    "tsconfig.json",
  ])
    assert.deepEqual(selected(path), ["frontend", "docker"], path)
  assert.deepEqual(selected("tests/polling.test.ts"), ["frontend"])
  assert.deepEqual(selected("scripts/tsconfig.json"), ["frontend", "docker"])
  for (const path of [
    "docker/Dockerfile",
    "docker/Dockerfile.dockerignore",
    "LICENSE",
  ])
    assert.deepEqual(selected(path), ["docker"], path)
})

test("a validation script selects the jobs that run it, and every script has a decision", () => {
  assert.deepEqual(selected("scripts/golangci-lint.yml"), ["backend"])
  assert.deepEqual(selected("scripts/go-check.mjs"), [
    "go-builds",
    "go-container",
  ])
  assert.deepEqual(selected("scripts/go-docker-smoke.mjs"), ["go-container"])
  assert.deepEqual(selected("scripts/go-test-shard.mjs"), ["backend"])
  assert.deepEqual(selected("scripts/go-test-durations.json"), ["backend"])
  assert.deepEqual(selected("scripts/go-test-shard.test.mjs"), [])
  assert.deepEqual(selected("scripts/smoke.ts"), [
    "frontend",
    "smoke",
    "docker",
  ])
  assert.deepEqual(selected("scripts/production-smoke.ts"), [
    "frontend",
    "docker",
  ])
  assert.deepEqual(selected("scripts/check-sensitive.test.ts"), ["frontend"])
  assert.deepEqual(selected("scripts/check-node.ts"), ["frontend"])
  assert.deepEqual(selected("scripts/cdn-base.ts"), ["frontend", "docker"])
  assert.deepEqual(selected("scripts/privacy-allowlist.json"), [])
  assert.deepEqual(
    selected(
      "docs/history/unreleased.md",
      "src/about/AboutPage.tsx",
      "scripts/privacy-allowlist.json",
    ),
    ["frontend", "docker"],
  )
  // The planner decides what every other change may skip, so it alone selects
  // every job; a new script must be classified rather than left unlisted.
  const scripts = readdirSync(new URL(".", import.meta.url))
  for (const name of scripts) {
    const everything = validates(pr([`scripts/${name}`]))
    assert.equal(everything, name === "ci-plan.ts", name)
  }
  for (const name of Object.keys(scriptJobs))
    assert.ok(scripts.includes(name), `${name} is listed but does not exist`)
})

test("mixed and renamed paths plan the union of their jobs", () => {
  assert.deepEqual(selected("backend/internal/old.go", "docs/retired.md"), [
    "backend",
    "go-builds",
    "go-container",
    "smoke",
    "docker",
  ])
  assert.deepEqual(selected("tests/polling.test.ts", "backend/Dockerfile"), [
    "go-container",
    "frontend",
  ])
})

test("dependency manifests also plan their advisory audit, and nothing else does", () => {
  for (const path of ["backend/go.mod", "backend/go.sum"])
    assert.deepEqual(
      selected(path),
      ["backend", "go-builds", "go-container", "smoke", "docker", "go-audit"],
      path,
    )
  for (const path of ["package.json", "package-lock.json"])
    assert.deepEqual(selected(path), ["frontend", "docker", "npm-audit"], path)
  // Paths that select every validation job query no advisory database.
  for (const paths of [
    ["Makefile"],
    [".github/workflows/validate.yml"],
    ["scripts/ci-plan.ts"],
    ["new-runtime/settings.json"],
    ["backend/internal/httpapi/server.go", "src/main.tsx"],
  ]) {
    const plan = pr(paths)
    assert.ok(
      auditJobs.every((job) => !plan[job]),
      String(paths),
    )
  }
  // Without a diff the planner cannot rule a manifest change out.
  for (const paths of [null, []])
    assert.ok(Object.values(pr(paths)).every(Boolean), String(paths))
})

test("the full tier runs every validation job but no audit whatever changed, and an unknown tier is rejected", () => {
  for (const paths of [
    null,
    [],
    ["README.md"],
    ["docs/how-it-works.md"],
    ["package-lock.json"],
  ]) {
    const plan = createPlan({ tier: "full", paths })
    assert.ok(validates(plan), String(paths))
    assert.ok(
      auditJobs.every((job) => !plan[job]),
      String(paths),
    )
  }
  for (const tier of [undefined, "", "complete", "PR"]) {
    assert.throws(() => createPlan({ tier, paths: ["README.md"] }))
  }
})

test("the gate accepts only planned skips and rejects failures, cancellations and missing results", () => {
  for (const plan of [
    pr(["README.md"]),
    pr(["Makefile"]),
    pr(["src/main.tsx"]),
    pr(["backend/go.sum", "package-lock.json"]),
    createPlan({ tier: "full" }),
  ]) {
    assert.doesNotThrow(() => validateResults(plan, resultsFor(plan)))
    for (const job of [...alwaysRun, ...jobs]) {
      for (const result of ["failure", "cancelled", undefined]) {
        const results = { ...resultsFor(plan), [job]: { result } }
        assert.throws(() => validateResults(plan, results), job)
      }
      if (
        (alwaysRun as readonly string[]).includes(job) ||
        plan[job as (typeof jobs)[number]]
      ) {
        const results = { ...resultsFor(plan), [job]: { result: "skipped" } }
        assert.throws(() => validateResults(plan, results), job)
      }
    }
  }
  for (const plan of [
    null,
    {},
    Object.fromEntries(jobs.map((job) => [job, "false"])),
  ]) {
    assert.throws(() => validateResults(plan, {}))
  }
})
