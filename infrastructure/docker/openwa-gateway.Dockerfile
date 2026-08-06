FROM node:22.16-alpine AS build
WORKDIR /app
COPY services/openwa-gateway/package.json services/openwa-gateway/package-lock.json services/openwa-gateway/tsconfig.json services/openwa-gateway/nest-cli.json ./
RUN npm ci --no-audit --no-fund
COPY services/openwa-gateway/src ./src
RUN npm run build && npm prune --omit=dev

FROM node:22.16-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production
RUN mkdir -p /app/session-data /data/gateway-idempotency /data/gateway-event-outbox && chown -R node:node /app /data
COPY --from=build --chown=node:node /app/node_modules ./node_modules
COPY --from=build --chown=node:node /app/dist ./dist
USER node
EXPOSE 2785
CMD ["node", "dist/main.js"]
