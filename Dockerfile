FROM node:24.19.0-bookworm-slim AS dependencies

WORKDIR /app

COPY package.json package-lock.json ./
RUN apt-get update \
  && apt-get install -y --no-install-recommends python3 make g++ \
  && rm -rf /var/lib/apt/lists/*
RUN npm ci

FROM dependencies AS development

ENV NODE_ENV=development \
    APP_MODE=development \
    PORT=8787

COPY . .
EXPOSE 5173

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD node -e "fetch('http://127.0.0.1:5173/api/health').then((response) => process.exit(response.ok ? 0 : 1)).catch(() => process.exit(1))"

CMD ["npm", "exec", "concurrently", "--", "-k", "npm run dev:server", "npm run dev:client -- --host 0.0.0.0 --port 5173 --strictPort"]

FROM dependencies AS build

COPY . .
RUN npm run build \
  && npm prune --omit=dev

FROM node:24.19.0-bookworm-slim AS runtime

ENV NODE_ENV=production \
    APP_MODE=production \
    PORT=8787

WORKDIR /app

COPY --from=build /app/package.json /app/package-lock.json ./
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/dist ./dist
COPY --from=build /app/public ./public
COPY --from=build /app/server ./server
COPY --from=build /app/scripts ./scripts
COPY --from=build /app/src/history/media.js /app/src/history/media-catalog.json ./src/history/

RUN mkdir -p /app/data

VOLUME ["/app/data"]
EXPOSE 8787

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD node -e "fetch('http://127.0.0.1:8787/api/health').then((response) => process.exit(response.ok ? 0 : 1)).catch(() => process.exit(1))"

CMD ["node", "server/index.js"]
