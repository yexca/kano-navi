import assert from "node:assert/strict"
import { spawnSync } from "node:child_process"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"
import test from "node:test"

import {
  isAllowedHost,
  isSensitivePath,
  isSafePlaceholder,
  parsePrivacyAllowlist,
  repositoryFiles,
  scanLine,
  type ApprovedHosts,
  type SensitiveFinding,
} from "./check-sensitive.ts"

function findingsFor(
  text: string,
  approvedHosts: ApprovedHosts = new Map(),
): SensitiveFinding[] {
  const findings: SensitiveFinding[] = []
  scanLine({ file: "fixture.txt", line: 1, text }, findings, approvedHosts)
  return findings
}

test("accepts reserved and explicitly approved public hosts", () => {
  const hosts = parsePrivacyAllowlist(
    JSON.stringify({
      version: 1,
      hosts: [{ host: "public.example", reason: "Fixture host." }],
    }),
  )
  assert.equal(isAllowedHost("localhost", hosts), true)
  assert.equal(isAllowedHost("cdn.public.example", hosts), true)
  assert.equal(isAllowedHost("unknown.fan", hosts), false)
})

test("detects credential-like tokens without accepting public URLs", () => {
  const token = ["ghp_", "a".repeat(24)].join("")
  const findings = findingsFor(`${token} https://example.invalid/reference`)
  assert.equal(
    findings.some((finding) => finding.kind === "credential-like value"),
    true,
  )
  assert.equal(
    findings.some((finding) => finding.kind === "non-approved service URL"),
    false,
  )
})

test("detects literal sensitive assignments and allows placeholders", () => {
  const key = ["api", "Key"].join("")
  const findings = findingsFor(`${key} = "real-looking-value"`)
  assert.equal(
    findings.some(
      (finding) => finding.kind === "literal value assigned to a sensitive key",
    ),
    true,
  )
  assert.equal(isSafePlaceholder("${API_KEY}"), true)
  assert.equal(isSafePlaceholder("change-me"), true)
})

test("detects embedded URL credentials and sensitive query values", () => {
  const credentialUrl = [
    "https",
    "://user",
    ":pass@example.invalid/private",
  ].join("")
  const queryUrl = [
    "https",
    "://example.invalid/callback?token=",
    "actual-value",
  ].join("")
  const findings = findingsFor(`${credentialUrl} ${queryUrl}`)
  assert.equal(
    findings.some(
      (finding) => finding.kind === "URL contains embedded credentials",
    ),
    true,
  )
  assert.equal(
    findings.some(
      (finding) => finding.kind === "URL query contains a sensitive parameter",
    ),
    true,
  )
})

test("detects private paths and non-approved hosts", () => {
  const privatePath = ["/", "Users", "/maintainer/project"].join("")
  const unknownUrl = ["https", "://private.fan/endpoint"].join("")
  const findings = findingsFor(`${privatePath} ${unknownUrl}`)
  assert.equal(
    findings.some((finding) => finding.kind === "private local path"),
    true,
  )
  assert.equal(
    findings.some((finding) => finding.kind === "non-approved service URL"),
    true,
  )
})

test("rejects malformed allowlist documents", () => {
  assert.throws(
    () =>
      parsePrivacyAllowlist(
        JSON.stringify({
          version: 1,
          hosts: [{ host: "X.com", reason: "test" }],
        }),
      ),
    /lowercase hostname/,
  )
  assert.throws(
    () =>
      parsePrivacyAllowlist(
        JSON.stringify({
          version: 1,
          hosts: [
            { host: "x.test", reason: "test" },
            { host: "x.test", reason: "duplicate" },
          ],
        }),
      ),
    /duplicates an approved host/,
  )
})

test("lists Git-visible files, skipping ignored ones but keeping tracked ones", (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "kano-sensitive-"))
  t.after(() => fs.rmSync(root, { recursive: true, force: true }))
  const git = (...args: string[]) =>
    assert.equal(spawnSync("git", args, { cwd: root }).status, 0)
  git("init", "--quiet")
  fs.writeFileSync(path.join(root, ".gitignore"), "*.log\n.tool/\n")
  fs.mkdirSync(path.join(root, ".tool"))
  fs.writeFileSync(path.join(root, ".tool", "index.db"), "")
  fs.writeFileSync(path.join(root, "daemon.log"), "")
  fs.writeFileSync(path.join(root, "forced.log"), "")
  fs.writeFileSync(path.join(root, "notes.txt"), "")
  git("add", "--force", "forced.log")

  const files = repositoryFiles(root).sort()
  assert.deepEqual(files, [".gitignore", "forced.log", "notes.txt"])
  assert.deepEqual(files.filter(isSensitivePath), ["forced.log"])
})

test("flags sensitive file names", () => {
  assert.equal(isSensitivePath(".env"), true)
  assert.equal(isSensitivePath(".env.example"), false)
  assert.equal(isSensitivePath("runtime/database.db"), true)
  assert.equal(isSensitivePath("logs/daemon.log"), true)
})
