# One static Go binary serves the public API, the operator console, workflows
# and timers, MCP and the built frontend. The runtime also carries yt-dlp and
# its JavaScript runtime for the optional YouTube metadata method. The frontend
# and backend stages run on the build machine and Go cross-compiles for the
# target, so a multi-platform build (linux/amd64, linux/arm64) needs no
# emulation.
FROM --platform=$BUILDPLATFORM node:24.22.1-bookworm-slim AS frontend

WORKDIR /app

COPY package.json package-lock.json ./
# Only the Vite build runs here; no package needs an install script.
RUN npm ci --ignore-scripts

COPY . .
RUN npx vite build

FROM --platform=$BUILDPLATFORM golang:1.26.10-bookworm AS backend
ARG TARGETARCH
ARG YTDLP_VERSION=2026.10.26

WORKDIR /src

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./
# package.json owns the version; the binary reports it in MCP serverInfo.
COPY package.json /tmp/package.json
RUN VERSION="$(grep -m1 '^  "version"' /tmp/package.json | sed 's/.*"version": "\([^"]*\)".*/\1/')" \
  && echo "$VERSION" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' \
  && CGO_ENABLED=0 GOOS=linux GOARCH="$TARGETARCH" go build -trimpath \
    -ldflags "-X github.com/yexca/kano-navi/backend/internal/buildinfo.Version=${VERSION}" \
    -o /out/kano-go ./cmd/kano-go \
  && mkdir -p /out/data

# yt-dlp is bundled for the target architecture. The release checksum is
# checked before the executable enters the runtime image.
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl \
  && rm -rf /var/lib/apt/lists/* \
  && case "$TARGETARCH" in \
       amd64) YTDLP_ASSET=yt-dlp_linux ;; \
       arm64) YTDLP_ASSET=yt-dlp_linux_aarch64 ;; \
       *) echo "unsupported yt-dlp architecture: $TARGETARCH" >&2; exit 1 ;; \
     esac \
  && curl --fail --location --silent --show-error --retry 3 --max-time 120 \
       "https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/${YTDLP_ASSET}" \
       --output /out/yt-dlp \
  && curl --fail --location --silent --show-error --retry 3 --max-time 30 \
       "https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/SHA2-256SUMS" \
       --output /tmp/yt-dlp-sha256sums \
  && EXPECTED_SHA="$(awk -v asset="$YTDLP_ASSET" '$2 == asset { print $1; exit }' /tmp/yt-dlp-sha256sums)" \
  && test -n "$EXPECTED_SHA" \
  && printf '%s  %s\n' "$EXPECTED_SHA" /out/yt-dlp | sha256sum --check --status \
  && chmod 0755 /out/yt-dlp \
  && rm -f /tmp/yt-dlp-sha256sums

# Node for the development image, on its own platform rather than the build
# machine's.
FROM node:24.22.1-bookworm-slim AS node

# Development: the Go toolchain builds and runs the backend, Vite serves the
# frontend with hot reload and proxies /api, /media and /mcp to it.
FROM golang:1.26.10-bookworm AS development

COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
  && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx

ENV NODE_ENV=development \
    APP_MODE=development \
    PORT=7657

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci --ignore-scripts
COPY backend/go.mod backend/go.sum ./backend/
RUN go -C backend mod download

COPY . .
# Warm the build cache so the first start does not compile every dependency.
RUN go -C backend build -o /tmp/kano-go ./cmd/kano-go && rm /tmp/kano-go
EXPOSE 7657

HEALTHCHECK --interval=30s --timeout=5s --start-period=60s --retries=3 \
  CMD node -e "fetch('http://127.0.0.1:7657/api/health').then((response) => process.exit(response.ok ? 0 : 1)).catch(() => process.exit(1))"

CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0"]

FROM node:24.22.1-bookworm-slim AS runtime

ENV APP_MODE=production \
    PORT=7657 \
    YTDLP_BINARY=/app/yt-dlp

WORKDIR /app

COPY --from=backend /out/kano-go /app/kano-go
COPY --from=backend /out/yt-dlp /app/yt-dlp
COPY --from=backend /out/data /app/data
COPY --from=frontend /app/dist /app/dist
# Bundled profile and schedule fallbacks are read from public/assets.
COPY --from=frontend /app/public /app/public
COPY LICENSE /app/LICENSE

VOLUME ["/app/data"]
EXPOSE 7657

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD ["/app/kano-go", "healthcheck"]

# `docker run <image> seed|sync|migrate-llm` runs a maintenance command
# against the mounted /app/data instead of starting the service.
ENTRYPOINT ["/app/kano-go"]
CMD ["serve"]
