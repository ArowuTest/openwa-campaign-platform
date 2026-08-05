FROM node:22.16-alpine AS dependencies
WORKDIR /app
COPY apps/admin-web/package.json ./
RUN npm install --no-audit --no-fund

FROM node:22.16-alpine AS build
WORKDIR /app
COPY --from=dependencies /app/node_modules ./node_modules
COPY apps/admin-web/ ./
RUN npm run build

FROM node:22.16-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production
RUN addgroup -S campaign && adduser -S campaign -G campaign
COPY --from=build --chown=campaign:campaign /app/.next/standalone ./
COPY --from=build --chown=campaign:campaign /app/.next/static ./.next/static
USER campaign
EXPOSE 3000
CMD ["node", "server.js"]
