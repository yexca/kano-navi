import fs from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"

const scriptDirectory = path.dirname(fileURLToPath(import.meta.url))
const repositoryRoot = path.resolve(scriptDirectory, "..")
const externalTargetPattern = /^(?:[a-z][a-z0-9+.-]*:|\/\/)/iu

function normalizePath(file) {
  return file.replaceAll("\\", "/")
}

function markdownFiles(directory, relativeDirectory = "") {
  const files = []
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    const relativePath = normalizePath(path.join(relativeDirectory, entry.name))
    if (entry.isDirectory()) {
      files.push(
        ...markdownFiles(path.join(directory, entry.name), relativePath),
      )
    } else if (entry.isFile() && entry.name.toLowerCase().endsWith(".md")) {
      files.push(relativePath)
    }
  }
  return files
}

function targetPath(rawTarget) {
  const withoutTitle = rawTarget.trim()
  const target = withoutTitle.startsWith("<")
    ? withoutTitle.slice(1, withoutTitle.indexOf(">"))
    : withoutTitle.split(/\s+/u, 1)[0]
  if (!target || externalTargetPattern.test(target) || target.startsWith("#"))
    return null
  const withoutFragment = target.split(/[?#]/u, 1)[0]
  if (!withoutFragment) return null
  try {
    return decodeURIComponent(withoutFragment)
  } catch {
    return withoutFragment
  }
}

export function checkMarkdownLinks() {
  const files = [
    "README.md",
    ...markdownFiles(path.join(repositoryRoot, "docs"), "docs"),
    "SECURITY.md",
    "AGENTS.md",
  ]
    .filter((file, index, all) => all.indexOf(file) === index)
    .filter((file) => fs.existsSync(path.resolve(repositoryRoot, file)))
  const missing = []
  let linksChecked = 0
  const linkPattern = /!?\[[^\]]*\]\(([^)]+)\)/gu

  for (const file of files) {
    const contents = fs.readFileSync(path.resolve(repositoryRoot, file), "utf8")
    for (const [index, line] of contents.split(/\r?\n/u).entries()) {
      linkPattern.lastIndex = 0
      for (const match of line.matchAll(linkPattern)) {
        const relativeTarget = targetPath(match[1])
        if (relativeTarget === null) continue
        linksChecked += 1
        const fullTarget = path.resolve(
          path.dirname(path.resolve(repositoryRoot, file)),
          relativeTarget,
        )
        if (!fs.existsSync(fullTarget)) {
          missing.push({ file, line: index + 1, target: relativeTarget })
        }
      }
    }
  }

  if (missing.length > 0) {
    console.error(
      `Documentation link check found ${missing.length} missing target(s):`,
    )
    for (const item of missing)
      console.error(`- ${item.file}:${item.line}: ${item.target}`)
    return 1
  }
  console.log(
    `Documentation link check passed (${files.length} Markdown file(s), ${linksChecked} local link(s)).`,
  )
  return 0
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  process.exitCode = checkMarkdownLinks()
}
