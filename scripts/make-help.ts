const groups = {
  Development: {
    install: "Install locked npm dependencies",
    dev: "Start Vite and Express on one port (default 7657)",
    "dev-client": "Alias for the shared Vite and Express development server",
    "dev-server": "Start Express only",
    build: "Build the production frontend",
    preview: "Preview the Vite bundle with the API (default 7657)",
    start: "Start the API and serve dist (configure production auth first)",
    seed: "Add missing static seed records to the runtime database",
    sync: "Fetch configured external sources and update runtime snapshots",
  },
  Validation: {
    format: "Format supported files (writes changes)",
    "format-check": "Check formatting",
    typecheck: "Check TypeScript source, scripts, and tests",
    "docs-check": "Check local Markdown links",
    "sensitive-check": "Scan workspace source for sensitive information",
    "test-sensitive": "Test scanner behavior",
    "test-server": "Test SQLite, HTTP, sync, media, and LLM contracts",
    smoke: "Check a seeded temporary SQLite snapshot through local HTTP",
    "ci-style": "Check formatting, documentation, and source privacy",
    "ci-backend": "Check types and run server contract tests",
    "ci-frontend": "Build the frontend",
    check: "Run installed-dependency checks and local API smoke",
  },
  "Docker and full CI": {
    "docker-build": "Build DOCKER_IMAGE (default kano-navi:ci)",
    "production-smoke": "Check the built image in a disposable container",
    "ci-local": "Run checks, build the image, and check production runtime",
    ci: "Install dependencies, then run ci-local (requires Docker)",
  },
}

console.log("Usage: make <target>\n")
for (const [group, targets] of Object.entries(groups)) {
  console.log(`${group}:`)
  for (const [target, description] of Object.entries(targets))
    console.log(`  ${target.padEnd(20)} ${description}`)
  console.log("")
}
console.log(
  "Aliases: check-docs -> docs-check; check-sensitive/privacy-check -> sensitive-check",
)
