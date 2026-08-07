FROM node:22.19-bookworm-slim AS build
WORKDIR /workspace/services/openwa-gateway

ENV PUPPETEER_SKIP_DOWNLOAD=true \
    PUPPETEER_SKIP_CHROMIUM_DOWNLOAD=true

RUN apt-get update \
    && apt-get install -y --no-install-recommends patch \
    && rm -rf /var/lib/apt/lists/*

COPY services/openwa-gateway/package.json services/openwa-gateway/package-lock.json ./
COPY services/openwa-gateway/tsconfig.json services/openwa-gateway/nest-cli.json ./
RUN npm ci --workspaces=false --no-audit --no-fund

COPY services/openwa-gateway/scripts ./scripts
COPY services/openwa-gateway/src ./src
COPY third_party/openwa/upstream/src /workspace/third_party/openwa/upstream/src
COPY third_party/openwa/upstream/scripts/patch-wwebjs-201832.js ./scripts/
COPY third_party/openwa/upstream/scripts/patch-wwebjs-newsletter-preview.js ./scripts/
COPY third_party/openwa/upstream/scripts/wwebjs-201832.patch ./scripts/

RUN node scripts/patch-wwebjs-201832.js \
    && node scripts/patch-wwebjs-newsletter-preview.js \
    && npm run build \
    && npm prune --omit=dev --workspaces=false
FROM node:22.19-bookworm-slim AS runtime
ARG TARGETARCH
WORKDIR /app

ENV NODE_ENV=production \
    PUPPETEER_SKIP_DOWNLOAD=true \
    PUPPETEER_SKIP_CHROMIUM_DOWNLOAD=true \
    XDG_CONFIG_HOME=/tmp/.chromium-config \
    XDG_CACHE_HOME=/tmp/.chromium-cache \
    PUPPETEER_EXECUTABLE_PATH=/usr/local/bin/puppeteer-chrome

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
       ca-certificates curl unzip dumb-init procps \
       libnss3 libatk1.0-0 libatk-bridge2.0-0 libcups2 \
       libdrm2 libxkbcommon0 libxcomposite1 libxdamage1 libxfixes3 \
       libxrandr2 libgbm1 libasound2 libpango-1.0-0 libcairo2 \
    && if [ "$TARGETARCH" = "amd64" ]; then \
         curl -fsSL -o /tmp/chrome.zip \
           https://storage.googleapis.com/chrome-for-testing-public/146.0.7680.31/linux64/chrome-linux64.zip; \
         unzip -q /tmp/chrome.zip -d /opt; \
         ln -s /opt/chrome-linux64/chrome /usr/local/bin/puppeteer-chrome; \
         rm -f /tmp/chrome.zip; \
       else \
         apt-get install -y --no-install-recommends chromium; \
         ln -s /usr/bin/chromium /usr/local/bin/puppeteer-chrome; \
       fi
RUN apt-get purge -y curl unzip \
    && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/* /tmp/*

RUN mkdir -p \
      /app/session-data \
      /data/gateway-idempotency \
      /data/gateway-event-outbox \
      /data/gateway-inbound-outbox \
      /data/gateway-session-authority \
      /data/gateway-command-nonces \
      /data/openwa-session-registry \
    && chown -R node:node /app /data

COPY --from=build --chown=node:node /workspace/services/openwa-gateway/node_modules ./node_modules
COPY --from=build --chown=node:node /workspace/services/openwa-gateway/dist ./dist

USER node
EXPOSE 2785
ENTRYPOINT ["dumb-init", "--"]
CMD ["node", "dist/main.js"]
