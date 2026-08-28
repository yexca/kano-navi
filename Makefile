.PHONY: help install dev dev-client dev-server build preview start seed sync format format-check check-sensitive sensitive-check privacy-check test-sensitive test-server check-docs check ci

NPM ?= npm

help:
	@printf '%s\n' \
		'Usage: make <target>' \
		'' \
		'install          Install locked npm dependencies' \
		'dev              Start Vite and Express together' \
		'dev-client       Start the Vite client only' \
		'dev-server       Start the Express API only' \
		'build            Create the production frontend bundle' \
		'preview          Preview the Vite bundle' \
		'start            Start the production server' \
		'seed             Seed the local SQLite snapshot' \
		'sync             Sync public X and YouTube sources' \
		'format           Format supported files with Prettier' \
		'format-check     Check formatting with Prettier' \
		'check-sensitive  Scan the workspace for sensitive information' \
		'test-sensitive   Run scanner unit tests' \
		'test-server      Run database and media-cache tests' \
		'check-docs       Check local Markdown links' \
		'check            Run checks without reinstalling dependencies' \
		'ci               Install dependencies and run the full local CI check'

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

start:
	$(NPM) start

seed:
	$(NPM) run seed

sync:
	$(NPM) run sync

format:
	$(NPM) run format

format-check:
	$(NPM) run format:check

check-sensitive:
	$(NPM) run check-sensitive

sensitive-check privacy-check: check-sensitive

test-sensitive:
	$(NPM) run test:sensitive

test-server:
	$(NPM) run test:server

check-docs:
	$(NPM) run docs:check-links

check:
	$(NPM) run check

ci: install
	$(NPM) run check
