import fs from "node:fs"
import path from "node:path"
import { spawnSync } from "node:child_process"
import { fileURLToPath } from "node:url"

const scriptDirectory = path.dirname(fileURLToPath(import.meta.url))
export const repositoryRoot = path.resolve(scriptDirectory, "..")
export const privacyAllowlistFile = "scripts/privacy-allowlist.json"

const urlPattern = /\bhttps?:\/\/[^\s<>"'`]+/giu
const ipv4Pattern = /\b(?:\d{1,3}\.){3}\d{1,3}\b/gu
const knownTokenPatterns = [
  /\bgh[pousr]_[A-Za-z0-9_]{20,}\b/gu,
  /\bgithub_pat_[A-Za-z0-9_]{20,}\b/gu,
  /\bglpat-[A-Za-z0-9_-]{20,}\b/gu,
  /\bxox[baprs]-[A-Za-z0-9-]{20,}\b/gu,
  /\bAKIA[0-9A-Z]{16}\b/gu,
  /\bAIza[0-9A-Za-z_-]{30,}\b/gu,
  /\bsk_(?:live|test)_[A-Za-z0-9]{16,}\b/gu,
  /\bBearer\s+[A-Za-z0-9._~+/=-]{20,}\b/giu,
  /-----BEGIN(?: [A-Z]+)? PRIVATE KEY-----/gu,
]
const assignmentPattern =
  /\b(?<key>[A-Za-z_][A-Za-z0-9_.-]*)\s*(?::=|=>|=|:)\s*(?<value>"[^"\n]*"|'[^'\n]*'|`[^`\n]*`|[^\s,;}#]+)/gu
const sensitiveKeyPattern =
  /(?:password|passwd|secret|token|api[-_]?key|access[-_]?key|private[-_]?key|client[-_]?secret|auth[-_]?token)/iu
const sensitiveQueryParameter =
  /(?:password|passwd|secret|token|api[-_]?key|access[-_]?key|private[-_]?key|auth)/iu

const reservedHosts = new Set([
  "localhost",
  "0.0.0.0",
  "::1",
  "registry.npmjs.org",
  "example",
  "example.com",
  "example.net",
  "example.org",
  "example.invalid",
])

const ignoredDirectoryNames = new Set([
  ".git",
  "node_modules",
  "dist",
  "coverage",
  ".vite",
  ".cache",
  "data",
])

const binaryExtensions = new Set([
  ".7z",
  ".avi",
  ".bmp",
  ".gif",
  ".gz",
  ".ico",
  ".jpeg",
  ".jpg",
  ".mov",
  ".mp3",
  ".mp4",
  ".pdf",
  ".png",
  ".sqlite",
  ".svg",
  ".tar",
  ".webp",
  ".woff",
  ".woff2",
  ".zip",
])

function normalizePath(file) {
  return file.replaceAll("\\", "/")
}

function runGit(argumentsList) {
  return spawnSync("git", argumentsList, {
    cwd: repositoryRoot,
    encoding: "buffer",
  })
}

function walkFiles(directory, relativeDirectory = "") {
  const files = []
  let entries
  try {
    entries = fs.readdirSync(directory, { withFileTypes: true })
  } catch {
    return files
  }

  for (const entry of entries) {
    const relativePath = normalizePath(path.join(relativeDirectory, entry.name))
    if (entry.isDirectory()) {
      if (!ignoredDirectoryNames.has(entry.name)) {
        files.push(...walkFiles(path.join(directory, entry.name), relativePath))
      }
      continue
    }
    if (entry.isFile()) files.push(relativePath)
  }
  return files
}

function repositoryFiles() {
  const result = runGit(["ls-files", "--cached", "--others", "--exclude-standard", "-z"])
  if (result.status === 0) {
    return result.stdout
      .toString("utf8")
      .split("\0")
      .filter(Boolean)
      .map(normalizePath)
  }
  return walkFiles(repositoryRoot)
}

function isIgnoredPath(file) {
  const normalized = normalizePath(file)
  return normalized.split("/").some((part) => ignoredDirectoryNames.has(part))
}

function isBinaryPath(file) {
  return binaryExtensions.has(path.posix.extname(normalizePath(file)).toLowerCase())
}

export function isSensitivePath(file) {
  const normalized = normalizePath(file)
  if (isIgnoredPath(normalized)) return false
  const baseName = path.posix.basename(normalized).toLowerCase()

  if (baseName === ".env" || (baseName.startsWith(".env.") && baseName !== ".env.example")) {
    return true
  }
  if (
    baseName === ".npmrc" ||
    /^(?:credentials?|secrets?)(?:\.[A-Za-z0-9._-]+)?$/iu.test(baseName)
  ) {
    return true
  }
  return /\.(?:db|sqlite(?:3)?|pem|key|p12|pfx|jks|keystore|log)$/iu.test(baseName)
}

function findSensitiveFiles(directory, relativeDirectory = "") {
  const findings = []
  let entries
  try {
    entries = fs.readdirSync(directory, { withFileTypes: true })
  } catch {
    return findings
  }

  for (const entry of entries) {
    const relativePath = normalizePath(path.join(relativeDirectory, entry.name))
    if (entry.isDirectory()) {
      if (!ignoredDirectoryNames.has(entry.name)) {
        findings.push(...findSensitiveFiles(path.join(directory, entry.name), relativePath))
      }
      continue
    }
    if (entry.isFile() && isSensitivePath(relativePath)) {
      findings.push({ file: relativePath, line: null, kind: "sensitive runtime or credential file" })
    }
  }
  return findings
}

function canonicalHost(value) {
  return String(value).toLowerCase().replace(/\.$/u, "")
}

export function parsePrivacyAllowlist(contents) {
  let parsed
  try {
    parsed = JSON.parse(contents)
  } catch (error) {
    throw new Error(`invalid JSON: ${error instanceof Error ? error.message : String(error)}`)
  }
  if (parsed === null || Array.isArray(parsed) || typeof parsed !== "object") {
    throw new Error("root must be an object")
  }
  const rootKeys = Object.keys(parsed).sort()
  if (rootKeys.join(",") !== "hosts,version" || parsed.version !== 1 || !Array.isArray(parsed.hosts)) {
    throw new Error("root must contain only version 1 and a hosts array")
  }

  const hosts = new Map()
  for (const [index, entry] of parsed.hosts.entries()) {
    const entryNumber = index + 1
    if (entry === null || Array.isArray(entry) || typeof entry !== "object") {
      throw new Error(`host ${entryNumber} must be an object`)
    }
    const entryKeys = Object.keys(entry).sort()
    if (entryKeys.join(",") !== "host,reason") {
      throw new Error(`host ${entryNumber} must contain only host and reason`)
    }
    if (typeof entry.host !== "string" || entry.host.trim() === "") {
      throw new Error(`host ${entryNumber} has an invalid host`)
    }
    const host = canonicalHost(entry.host)
    if (
      host !== entry.host ||
      host.includes("/") ||
      host.includes(":") ||
      !/^[a-z0-9.-]+$/u.test(host)
    ) {
      throw new Error(`host ${entryNumber} must be a lowercase hostname without a scheme`)
    }
    if (typeof entry.reason !== "string" || entry.reason.trim() === "") {
      throw new Error(`host ${entryNumber} must have a reason`)
    }
    if (hosts.has(host)) throw new Error(`host ${entryNumber} duplicates an approved host`)
    hosts.set(host, entry.reason)
  }
  return hosts
}

function loadPrivacyAllowlist() {
  const fullPath = path.resolve(repositoryRoot, privacyAllowlistFile)
  try {
    return parsePrivacyAllowlist(fs.readFileSync(fullPath, "utf8"))
  } catch (error) {
    throw new Error(`${privacyAllowlistFile}: ${error instanceof Error ? error.message : String(error)}`)
  }
}

export function isAllowedHost(host, approvedHosts = new Map()) {
  const normalized = canonicalHost(host).replace(/^\[|\]$/gu, "")
  if (reservedHosts.has(normalized)) return true
  if (normalized.startsWith("127.")) return true
  if (normalized.endsWith(".localhost") || normalized.endsWith(".test") || normalized.endsWith(".invalid")) return true
  for (const approvedHost of approvedHosts.keys()) {
    if (normalized === approvedHost || normalized.endsWith(`.${approvedHost}`)) return true
  }
  return false
}

function isDocumentationIPv4(value) {
  const octets = value.split(".").map(Number)
  if (
    octets.length !== 4 ||
    octets.some((octet) => !Number.isInteger(octet) || octet < 0 || octet > 255)
  ) {
    return false
  }
  return (
    (octets[0] === 192 && octets[1] === 0 && octets[2] === 2) ||
    (octets[0] === 198 && octets[1] === 51 && octets[2] === 100) ||
    (octets[0] === 203 && octets[1] === 0 && octets[2] === 113)
  )
}

function isAllowedIPv4(value) {
  return value === "0.0.0.0" || value.startsWith("127.") || isDocumentationIPv4(value)
}

function normalizedValue(value) {
  return value.trim().replace(/^['"`]|['"`,;]+$/gu, "")
}

export function isSafePlaceholder(value) {
  const normalized = normalizedValue(value).toLowerCase()
  return (
    normalized === "" ||
    /^(?:false|true|null|undefined|none|unset|unknown|any|boolean|number|string(?:\[\])?)$/u.test(normalized) ||
    /^(?:change-me|redacted|placeholder|dummy|example(?:[-_].*)?|synthetic(?:[-_].*)?|replace(?:[-_].*)?|your(?:[-_].*)?|not-a-real(?:[-_].*)?)$/u.test(normalized) ||
    /^\$\{\{[^}]+\}\}$/u.test(normalized) ||
    /^\$\{[^}]+\}$/u.test(normalized) ||
    /^\$[A-Za-z_][A-Za-z0-9_]*$/u.test(normalized) ||
    /^(?:process\.env|import\.meta\.env|os\.getenv|env\.)/u.test(normalized) ||
    /^<[^>]+>$/u.test(normalized) ||
    /^\*{3,}$/u.test(normalized)
  )
}

function isLiteralSensitiveValue(value) {
  const normalized = normalizedValue(value)
  if (isSafePlaceholder(normalized)) return false
  return /^['"`]/u.test(value.trim()) || /^[A-Za-z0-9+/_=-]{8,}$/u.test(normalized)
}

function parseURL(candidate) {
  const normalized = candidate
    .replace(/\$\([^)]*\)/gu, "0")
    .replace(/\$\{[^}]+\}/gu, "0")
    .replace(/[),.;!?\]}]+$/gu, "")
  return new URL(normalized)
}

function containsPrivatePath(text) {
  return /(?:[A-Za-z]:[\\/](?:Users|home)[\\/]|\/(?:home|Users)\/)/u.test(text)
}

export function scanLine(entry, findings, approvedHosts = new Map()) {
  const text = entry.text || ""
  if (containsPrivatePath(text)) findings.push({ ...entry, kind: "private local path" })

  for (const pattern of knownTokenPatterns) {
    pattern.lastIndex = 0
    if (pattern.test(text)) {
      findings.push({ ...entry, kind: "credential-like value" })
      break
    }
  }

  assignmentPattern.lastIndex = 0
  for (const assignment of text.matchAll(assignmentPattern)) {
    if (
      assignment.groups &&
      sensitiveKeyPattern.test(assignment.groups.key) &&
      isLiteralSensitiveValue(assignment.groups.value)
    ) {
      findings.push({ ...entry, kind: "literal value assigned to a sensitive key" })
    }
  }

  urlPattern.lastIndex = 0
  for (const match of text.matchAll(urlPattern)) {
    try {
      const url = parseURL(match[0])
      if (url.username !== "" || url.password !== "") {
        findings.push({ ...entry, kind: "URL contains embedded credentials" })
      }
      if (!isAllowedHost(url.hostname, approvedHosts)) {
        findings.push({ ...entry, kind: "non-approved service URL" })
      }
      for (const [name, value] of url.searchParams) {
        if (sensitiveQueryParameter.test(name) && !isSafePlaceholder(value)) {
          findings.push({ ...entry, kind: "URL query contains a sensitive parameter" })
          break
        }
      }
    } catch {
      findings.push({ ...entry, kind: "malformed URL requires review" })
    }
  }

  ipv4Pattern.lastIndex = 0
  for (const match of text.matchAll(ipv4Pattern)) {
    if (!isAllowedIPv4(match[0])) {
      findings.push({ ...entry, kind: "non-documentation IPv4 address" })
      break
    }
  }
}

function readText(file) {
  const fullPath = path.resolve(repositoryRoot, file)
  let stat
  try {
    stat = fs.lstatSync(fullPath)
  } catch {
    return null
  }
  if (!stat.isFile() || isBinaryPath(file)) return null
  const contents = fs.readFileSync(fullPath)
  if (contents.includes(0)) return null
  return contents.toString("utf8")
}

export function runSensitiveCheck() {
  const approvedHosts = loadPrivacyAllowlist()
  const files = repositoryFiles().filter((file) => !isIgnoredPath(file))
  const findings = findSensitiveFiles(repositoryRoot)
  let scannedTextFiles = 0
  let skippedBinaryFiles = 0

  for (const file of files) {
    const contents = readText(file)
    if (contents === null) {
      skippedBinaryFiles += 1
      continue
    }
    scannedTextFiles += 1
    for (const [index, text] of contents.split(/\r?\n/u).entries()) {
      scanLine({ file, line: index + 1, text }, findings, approvedHosts)
    }
  }

  const uniqueFindings = [
    ...new Map(
      findings.map((finding) => [`${finding.file}:${finding.line}:${finding.kind}`, finding]),
    ).values(),
  ]
  if (uniqueFindings.length > 0) {
    console.error(`Sensitive-information scan found ${uniqueFindings.length} item(s):`)
    for (const finding of uniqueFindings) {
      const location = finding.line === null ? finding.file : `${finding.file}:${finding.line}`
      console.error(`- ${location}: ${finding.kind}`)
    }
    console.error("Remove the sensitive value or document a public host in scripts/privacy-allowlist.json.")
    return 1
  }

  console.log(`Sensitive-information scan passed (${scannedTextFiles} text file(s), ${skippedBinaryFiles} binary file(s) skipped).`)
  return 0
}

function main() {
  try {
    process.exitCode = runSensitiveCheck()
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error))
    process.exitCode = 2
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main()
}
