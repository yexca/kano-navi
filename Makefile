.DEFAULT_GOAL := help

NPM ?= npm
NODE ?= node
DOCKER ?= docker
DOCKER_BUILD ?= $(DOCKER) build
DOCKER_BUILD_ARGS ?=
DOCKER_IMAGE ?= kano-navi:ci

.PHONY: help install dev dev-client dev-server build preview start seed sync
.PHONY: format format-check typecheck docs-check check-docs sensitive-check check-sensitive privacy-check test-sensitive test-server
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

start: export NODE_ENV := production
start:
	$(NODE) server/index.ts

seed:
	$(NPM) run seed

sync:
	$(NPM) run sync

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
