# One static Go binary serves the public API, the operator console, workflows
# and timers, MCP and the built frontend. Node only builds the frontend; no
# Node runtime is shipped. The frontend and backend stages run on the build
# machine and Go cross-compiles for the target, so a multi-platform build
# (linux/amd64, linux/arm64) needs no emulation: the runtime stage only copies.
FROM --platform=$BUILDPLATFORM node:24.22.1-bookworm-slim AS frontend

WORKDIR /app

COPY package.json package-lock.json ./
# Only the Vite build runs here; no package needs an install script.
RUN npm ci --ignore-scripts

COPY . .
RUN npx vite build

FROM --platform=$BUILDPLATFORM golang:1.26.10-bookworm AS backend
ARG TARGETARCH

WORKDIR /src

COPY backend-go/go.mod backend-go/go.sum ./
RUN go mod download

COPY backend-go/ ./
# package.json owns the version; the binary reports it in MCP serverInfo.
COPY package.json /tmp/package.json
RUN VERSION="$(grep -m1 '^  "version"' /tmp/package.json | sed 's/.*"version": "\([^"]*\)".*/\1/')" \
  && echo "$VERSION" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' \
  && CGO_ENABLED=0 GOOS=linux GOARCH="$TARGETARCH" go build -trimpath \
    -ldflags "-X github.com/yexca/kano-navi/backend-go/internal/buildinfo.Version=${VERSION}" \
    -o /out/kano-go ./cmd/kano-go \
  && mkdir -p /out/data

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
COPY backend-go/go.mod backend-go/go.sum ./backend-go/
RUN go -C backend-go mod download

COPY . .
# Warm the build cache so the first start does not compile every dependency.
RUN go -C backend-go build -o /tmp/kano-go ./cmd/kano-go && rm /tmp/kano-go
EXPOSE 7657

HEALTHCHECK --interval=30s --timeout=5s --start-period=60s --retries=3 \
  CMD node -e "fetch('http://127.0.0.1:7657/api/health').then((response) => process.exit(response.ok ? 0 : 1)).catch(() => process.exit(1))"

CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0"]

FROM scratch AS runtime

ENV APP_MODE=production \
    PORT=7657

WORKDIR /app

COPY --from=backend /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=backend /out/kano-go /app/kano-go
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
