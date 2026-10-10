import assert from "node:assert/strict"
import test from "node:test"

import {
  isAllowedHost,
  isSensitivePath,
  isSafePlaceholder,
  parsePrivacyAllowlist,
  scanLine,
} from "./check-sensitive.ts"

function findingsFor(text, approvedHosts = new Map()) {
  const findings = []
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

test("ignores the local CodeGraph database but keeps other databases protected", () => {
  assert.equal(isSensitivePath(".codegraph/codegraph.db"), false)
  assert.equal(isSensitivePath(".CODEGRAPH/CODEGRAPH.DB"), false)
  assert.equal(isSensitivePath("runtime/database.db"), true)
})
