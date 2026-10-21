.DEFAULT_GOAL := help

NPM ?= npm
NODE ?= node
GO ?= go
DOCKER ?= docker
DOCKER_BUILD ?= $(DOCKER) build
DOCKER_BUILD_ARGS ?=
DOCKER_IMAGE ?= kano-navi:ci
GOVULNCHECK_VERSION ?= v1.8.0
GO_BINARY ?= backend-go/bin/kano-go$(if $(filter Windows_NT,$(OS)),.exe,)
# package.json owns the version; Go builds receive it at link time.
APP_VERSION = $(shell $(NODE) -p "require('./package.json').version")
GO_LDFLAGS = -X github.com/yexca/kano-navi/backend-go/internal/buildinfo.Version=$(APP_VERSION)

.PHONY: help install dev dev-client dev-server build preview start seed sync go-check go-test
.PHONY: go-build go-start go-docker-build
.PHONY: format format-check typecheck docs-check check-docs sensitive-check check-sensitive privacy-check test-sensitive test-frontend
.PHONY: audit npm-audit npm-audit-production go-audit
.PHONY: smoke docker-build production-smoke ci-style ci-backend ci-frontend check ci-local ci

help:
	@$(NODE) scripts/make-help.ts

install:
	$(NPM) ci

dev:
	$(NPM) run dev

dev-client:
	$(NPM) run dev:client

dev-server:
	$(NPM) run dev:server

build:
	$(NPM) run build

preview:
	$(NPM) run preview

# The Go binary owns the whole site: it reads .env, data/ and dist/ from the
# repository root. Run `make build` first so dist/ exists.
start: go-build
	$(GO_BINARY) serve

seed: go-build
	$(GO_BINARY) seed

sync: go-build
	$(GO_BINARY) sync

go-check:
	$(NODE) scripts/go-check.mjs

go-test:
	$(GO) -C backend-go test ./...
	$(GO) -C backend-go vet ./...

go-build:
	$(GO) -C backend-go build -trimpath -ldflags "$(GO_LDFLAGS)" -o $(patsubst backend-go/%,%,$(GO_BINARY)) ./cmd/kano-go

go-start: start

go-docker-build: docker-build

format:
	$(NPM) run format

format-check:
	$(NPM) run format:check

typecheck:
	$(NPM) run typecheck

docs-check:
	$(NPM) run docs:check-links

check-docs: docs-check

sensitive-check:
	$(NPM) run check-sensitive

check-sensitive privacy-check: sensitive-check

test-sensitive:
	$(NPM) run test:sensitive

test-frontend:
	$(NPM) run test:frontend

smoke:
	$(NODE) scripts/smoke.ts

# Advisory checks query live databases, so they stay outside check and ci.
audit: npm-audit-production npm-audit go-audit

npm-audit-production:
	$(NPM) audit --omit=dev

npm-audit:
	$(NPM) audit

go-audit:
	$(GO) -C backend-go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

docker-build:
	$(DOCKER_BUILD) $(DOCKER_BUILD_ARGS) -t $(DOCKER_IMAGE) .

production-smoke:
	$(NODE) scripts/production-smoke.ts $(DOCKER_IMAGE)

# Narrow checks use the existing locked installation and never fetch source data.
ci-style: format-check docs-check test-sensitive sensitive-check

ci-backend: go-test

# The build type-checks the frontend, the maintenance scripts and the tests.
ci-frontend: build test-frontend

check: ci-style ci-backend ci-frontend smoke

# The recursive calls enforce build-before-smoke even when make uses -j.
ci-local: check
	$(MAKE) docker-build
	$(MAKE) production-smoke

# Installation completes before any checks start, including parallel invocations.
ci: install
	$(MAKE) ci-local
