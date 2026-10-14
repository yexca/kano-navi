const groups = {
  Development: {
    install: "Install locked npm dependencies",
    dev: "Start the Go backend and Vite together (frontend on 7657)",
    "dev-client": "Start Vite only; it proxies the API to KANO_API_PORT",
    "dev-server": "Build and start the Go backend only on KANO_API_PORT",
    build: "Build the production frontend",
    preview: "Preview the built frontend with the Go backend (default 7657)",
    start:
      "Build the Go backend and serve the whole site (configure production auth first)",
    seed: "Add missing static seed records to the runtime database",
    sync: "Fetch configured external sources once and update runtime snapshots",
    "go-build": "Build the Go backend binary into backend-go/bin",
    "ts-start": "Rollback: start the previous TypeScript service",
  },
  Validation: {
    format: "Format supported files (writes changes)",
    "format-check": "Check formatting",
    typecheck: "Check TypeScript source, scripts, and tests",
    "docs-check": "Check local Markdown links",
    "sensitive-check": "Scan workspace source for sensitive information",
    "test-sensitive": "Test scanner behavior",
    "test-server": "Test the TypeScript reference implementation's contracts",
    "go-test": "Run Go backend contracts and vet without Docker",
    "go-check":
      "Check Go formatting, contracts, cross-builds, and disposable containers",
    smoke: "Check a seeded temporary SQLite snapshot through the Go backend",
    "public-read-benchmark":
      "Measure TypeScript and Go public reads over 10,000 synthetic records",
    "public-read-deployment-smoke":
      "Run the canary/test TypeScript and Go container rehearsal",
    "ci-style": "Check formatting, documentation, and source privacy",
    "ci-backend": "Check types and run the TypeScript and Go contract tests",
    "ci-frontend": "Build the frontend",
    check: "Run installed-dependency checks and local API smoke",
    "public-read-gate":
      "Install, compare TypeScript/Go public reads, check Go, privacy, and docs",
  },
  "Docker and full CI": {
    "docker-build": "Build the Go image DOCKER_IMAGE (default kano-navi:ci)",
    "production-smoke": "Check the built image in a disposable container",
    "ts-docker-build":
      "Rollback: build the TypeScript image TS_DOCKER_IMAGE (default kano-navi-ts:ci)",
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
  "Aliases: check-docs -> docs-check; check-sensitive/privacy-check -> sensitive-check; go-start -> start; go-docker-build -> docker-build",
)
