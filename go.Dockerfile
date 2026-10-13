# Standalone Go image. One static binary serves the public API, the operator
# console, workflows and timers, MCP and the built frontend; no Node runtime is
# shipped. The TypeScript image in ./Dockerfile remains the rollback path.
FROM node:24.19.0-bookworm-slim AS frontend

WORKDIR /app

COPY package.json package-lock.json ./
# Only the Vite build runs here, so native add-ons are not compiled.
RUN npm ci --ignore-scripts

COPY . .
RUN npx vite build

FROM golang:1.26.4-bookworm AS backend

WORKDIR /src

COPY backend-go/go.mod backend-go/go.sum ./
RUN go mod download

COPY backend-go/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/kano-go ./cmd/kano-go \
  && mkdir -p /out/data

FROM scratch AS runtime

ENV APP_MODE=production \
    PORT=7657

WORKDIR /app

COPY --from=backend /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=backend /out/kano-go /app/kano-go
COPY --from=backend /out/data /app/data
COPY --from=frontend /app/dist /app/dist
COPY --from=frontend /app/public /app/public
COPY LICENSE /app/LICENSE

VOLUME ["/app/data"]
EXPOSE 7657

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD ["/app/kano-go", "-healthcheck"]

ENTRYPOINT ["/app/kano-go", "-standalone"]
