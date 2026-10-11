FROM node:22.19-alpine AS dependencies
WORKDIR /app
COPY apps/admin-web/package.json apps/admin-web/package-lock.json ./
RUN npm ci --no-audit --no-fund

FROM node:22.19-alpine AS build
WORKDIR /app
ENV NEXT_TELEMETRY_DISABLED=1
COPY --from=dependencies /app/node_modules ./node_modules
COPY apps/admin-web/ ./
RUN npm run build

FROM node:22.19-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production
ENV HOSTNAME=0.0.0.0
ENV PORT=3000
RUN addgroup -S campaign && adduser -S campaign -G campaign
COPY --from=build --chown=campaign:campaign /app/.next/standalone ./
COPY --from=build --chown=campaign:campaign /app/.next/static ./.next/static
USER campaign
EXPOSE 3000
CMD ["node", "server.js"]
