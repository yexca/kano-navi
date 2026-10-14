.DEFAULT_GOAL := help

NPM ?= npm
NODE ?= node
GO ?= go
DOCKER ?= docker
DOCKER_BUILD ?= $(DOCKER) build
DOCKER_BUILD_ARGS ?=
DOCKER_IMAGE ?= kano-navi:ci
TS_DOCKER_IMAGE ?= kano-navi-ts:ci
GO_BINARY ?= backend-go/bin/kano-go$(if $(filter Windows_NT,$(OS)),.exe,)

.PHONY: help install dev dev-client dev-server build preview start seed sync go-check go-test
.PHONY: go-build go-start go-docker-build ts-start ts-docker-build
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

# The Go binary owns the whole site: it reads .env, data/ and dist/ from the
# repository root. Run `make build` first so dist/ exists.
start: go-build
	$(GO_BINARY) serve

seed: go-build
	$(GO_BINARY) seed

sync: go-build
	$(GO_BINARY) sync

# Rollback path: the previous TypeScript service on the same data/ directory.
# Never run it while the Go service is running against that snapshot.
ts-start: export NODE_ENV := production
ts-start:
	$(NODE) server/index.ts

go-check:
	$(NODE) scripts/go-check.mjs

go-test:
	$(GO) -C backend-go test ./...
	$(GO) -C backend-go vet ./...

go-build:
	$(GO) -C backend-go build -trimpath -o $(patsubst backend-go/%,%,$(GO_BINARY)) ./cmd/kano-go

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

ts-docker-build:
	$(DOCKER_BUILD) $(DOCKER_BUILD_ARGS) -f ts.Dockerfile -t $(TS_DOCKER_IMAGE) .

production-smoke:
	$(NODE) scripts/production-smoke.ts $(DOCKER_IMAGE)

# Narrow checks use the existing locked installation and never fetch source data.
ci-style: format-check docs-check test-sensitive sensitive-check

# go-test covers the shipped backend; typecheck and test-server keep the
# TypeScript reference implementation and the maintenance scripts honest.
ci-backend: typecheck test-server go-test

ci-frontend: build

check: ci-style ci-backend ci-frontend smoke

# The recursive calls enforce build-before-smoke even when make uses -j.
ci-local: check
	$(MAKE) docker-build
	$(MAKE) production-smoke

# Installation completes before any checks start, including parallel invocations.
ci: install
	$(MAKE) ci-local
