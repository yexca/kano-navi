.DEFAULT_GOAL := help

NPM ?= npm
NODE ?= node
GO ?= go
DOCKER ?= docker
DOCKER_BUILD ?= $(DOCKER) build
DOCKER_BUILD_ARGS ?=
DOCKER_IMAGE ?= kano-navi:ci
GO_STANDALONE_IMAGE ?= kano-navi-go-standalone:ci
GO_BINARY ?= backend-go/bin/kano-go$(if $(filter Windows_NT,$(OS)),.exe,)

.PHONY: help install dev dev-client dev-server build preview start seed sync go-check go-test
.PHONY: go-build go-start go-docker-build
.PHONY: format format-check typecheck docs-check check-docs sensitive-check check-sensitive privacy-check test-sensitive test-server
.PHONY: smoke side-by-side-http public-read-gate public-read-benchmark public-read-deployment-smoke docker-build production-smoke ci-style ci-backend ci-frontend check ci-local ci

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

start: export NODE_ENV := production
start:
	$(NODE) server/index.ts

seed:
	$(NPM) run seed

sync:
	$(NPM) run sync

go-check:
	$(NODE) scripts/go-check.mjs

go-test:
	$(GO) -C backend-go test ./...
	$(GO) -C backend-go vet ./...

go-build:
	$(GO) -C backend-go build -trimpath -o $(patsubst backend-go/%,%,$(GO_BINARY)) ./cmd/kano-go

# The Go binary owns the whole site: it reads .env, data/ and dist/ from the
# repository root exactly like `make start` does for the TypeScript service.
go-start: go-build
	$(GO_BINARY) -standalone

go-docker-build:
	$(DOCKER_BUILD) $(DOCKER_BUILD_ARGS) -f go.Dockerfile -t $(GO_STANDALONE_IMAGE) .

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

test-server:
	$(NPM) run test:server

smoke:
	$(NODE) scripts/smoke.ts

side-by-side-http:
	$(NODE) scripts/side-by-side-http.mjs

public-read-benchmark:
	$(NODE) scripts/public-read-benchmark.mjs

public-read-deployment-smoke:
	$(NODE) scripts/public-read-deployment-smoke.mjs

# The public-read gate is intentionally ordered.  CI runs this target after
# provisioning Node and Go; local runs must not turn a dependency-missing
# comparison into a false success (the comparison scripts exit 2 and stop).
public-read-gate:
	$(MAKE) install
	$(MAKE) ci-backend
	$(NODE) scripts/dashboard-parity.mjs
	$(NODE) scripts/side-by-side-http.mjs
	$(MAKE) go-check
	$(MAKE) sensitive-check
	$(MAKE) docs-check

docker-build:
	$(DOCKER_BUILD) $(DOCKER_BUILD_ARGS) -t $(DOCKER_IMAGE) .

production-smoke:
	$(NODE) scripts/production-smoke.ts $(DOCKER_IMAGE)

# Narrow checks use the existing locked installation and never fetch source data.
ci-style: format-check docs-check test-sensitive sensitive-check

ci-backend: typecheck test-server

ci-frontend: build

check: ci-style ci-backend ci-frontend smoke

# The recursive calls enforce build-before-smoke even when make uses -j.
ci-local: check
	$(MAKE) docker-build
	$(MAKE) production-smoke

# Installation completes before any checks start, including parallel invocations.
ci: install
	$(MAKE) ci-local
