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
  },
  Validation: {
    format: "Format supported files (writes changes)",
    "format-check": "Check formatting",
    typecheck: "Check the TypeScript frontend, scripts, and tests",
    "docs-check": "Check local Markdown links",
    "sensitive-check": "Scan workspace source for sensitive information",
    "test-sensitive": "Test scanner behavior",
    "test-frontend":
      "Test frontend request hooks, polling, and the history catalog",
    "go-test": "Run Go backend contracts and vet without Docker",
    "go-lint": "Run golangci-lint over the Go backend",
    "go-vet": "Run go vet over the Go backend",
    "go-race": "Run Go backend contracts with the race detector (needs cgo)",
    actionlint: "Check GitHub Actions workflows",
    "go-check":
      "Check Go formatting, contracts, cross-builds, and disposable containers",
    smoke: "Check a seeded temporary SQLite snapshot through the Go backend",
    "ci-style":
      "Check formatting, documentation, workflows, and source privacy",
    "ci-backend": "Run Go lint, vet, and race-enabled contract tests",
    "ci-frontend": "Type-check and build the frontend, then run its tests",
    check: "Run installed-dependency checks and local API smoke",
  },
  "Dependency audit": {
    audit: "Run every dependency advisory check (queries live databases)",
    "npm-audit-production": "Audit locked production npm dependencies",
    "npm-audit": "Audit the full locked npm dependency tree",
    "npm-audit-signatures":
      "Verify registry signatures of the installed npm packages",
    "go-audit":
      "Check reachable Go module and standard library vulnerabilities",
  },
  "Docker and full CI": {
    "docker-build": "Build the Go image DOCKER_IMAGE (default kano-navi:ci)",
    "production-smoke": "Check the built image in a disposable container",
    "ci-local": "Run checks, build the image, and check production runtime",
    ci: "Install and verify dependencies, then run ci-local (requires Docker)",
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
