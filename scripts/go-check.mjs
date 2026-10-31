#!/usr/bin/env node

import {
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  writeFileSync,
} from "node:fs"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { spawnSync } from "node:child_process"

const root = join(import.meta.dirname, "..")
const moduleRoot = join(root, "backend")
const go = process.env.GO || "go"
const docker = process.env.DOCKER || "docker"
const dockerImage = process.env.GO_DOCKER_IMAGE || "kano-navi-go:ci"

const goFiles = walk(moduleRoot).filter((file) => file.endsWith(".go"))
const formatRoot = mkdtempSync(join(tmpdir(), "kano-go-format-"))
const formatFiles = goFiles.map((file) => {
  const target = join(formatRoot, file.slice(moduleRoot.length + 1))
  mkdirSync(join(target, ".."), { recursive: true })
  writeFileSync(target, readFileSync(file, "utf8").replaceAll("\r\n", "\n"))
  return target
})
try {
  run("gofmt -d", "gofmt", ["-d", ...formatFiles], {
    cwd: root,
    failOnStdout: true,
  })
} finally {
  rmSync(formatRoot, { recursive: true, force: true })
}
// make go-format-check stops here, so Docker-free checks still catch gofmt.
if (process.argv.includes("--format-only")) {
  console.log("go-check: format PASS")
  process.exit(0)
}
run("go test ./...", go, ["test", "./..."], { cwd: moduleRoot })
run("go vet ./...", go, ["vet", "./..."], { cwd: moduleRoot })
run("go build ./...", go, ["build", "./..."], { cwd: moduleRoot })
run("CGO_ENABLED=0 go test ./...", go, ["test", "./..."], {
  cwd: moduleRoot,
  env: { CGO_ENABLED: "0" },
})
run("CGO_ENABLED=0 go build ./...", go, ["build", "./..."], {
  cwd: moduleRoot,
  env: { CGO_ENABLED: "0" },
})
run(
  "GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...",
  go,
  ["build", "./..."],
  {
    cwd: moduleRoot,
    env: { GOOS: "linux", GOARCH: "amd64", CGO_ENABLED: "0" },
  },
)
run(
  "GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...",
  go,
  ["build", "./..."],
  {
    cwd: moduleRoot,
    env: { GOOS: "windows", GOARCH: "amd64", CGO_ENABLED: "0" },
  },
)
run(
  "Docker independent Go service image",
  docker,
  ["build", "-f", "backend/Dockerfile", "-t", dockerImage, "backend"],
  { cwd: root },
)
run(
  "Docker read-only HTTP smoke",
  process.execPath,
  ["scripts/go-docker-smoke.mjs"],
  { cwd: root, env: { GO: go, DOCKER: docker, GO_DOCKER_IMAGE: dockerImage } },
)
console.log("go-check: PASS")

function run(label, command, args, options = {}) {
  console.log(`go-check: ${label}`)
  const result = spawnSync(command, args, {
    cwd: options.cwd,
    env: { ...process.env, ...(options.env || {}) },
    encoding: "utf8",
  })
  if (result.stdout) process.stdout.write(result.stdout)
  if (result.stderr) process.stderr.write(result.stderr)
  if (options.failOnStdout && result.stdout) {
    console.error(`go-check: ${label} reported formatting differences`)
    process.exit(1)
  }
  if (result.error || result.status !== 0) {
    console.error(`go-check: ${label} failed`)
    process.exit(result.status || 1)
  }
}

function walk(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name)
    return entry.isDirectory() ? walk(path) : [path]
  })
}
