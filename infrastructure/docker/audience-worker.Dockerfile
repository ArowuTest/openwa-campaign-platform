FROM golang:1.23.2-alpine AS build
WORKDIR /src
RUN apk add --no-cache build-base postgresql-dev
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=1 go build -trimpath -ldflags='-s -w' -o /out/audience-worker ./cmd/audience-worker \
    && mkdir -p /out/object-store

FROM alpine:3.20
RUN apk add --no-cache ca-certificates libpq \
    && addgroup -S campaign \
    && adduser -S campaign -G campaign

RUN mkdir -p /var/lib/campaign-platform/objects && chown -R campaign:campaign /var/lib/campaign-platform
COPY --from=build --chown=campaign:campaign /out/audience-worker /audience-worker
USER campaign:campaign
EXPOSE 8091
ENTRYPOINT ["/audience-worker"]
