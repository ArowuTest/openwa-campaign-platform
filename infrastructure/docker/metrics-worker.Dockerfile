FROM golang:1.23.2-alpine AS build
WORKDIR /src
RUN apk add --no-cache build-base postgresql-dev
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=1 go build -trimpath -ldflags='-s -w' -o /out/metrics-worker ./cmd/metrics-worker

FROM alpine:3.20
RUN apk add --no-cache ca-certificates libpq \
    && addgroup -S campaign \
    && adduser -S campaign -G campaign
COPY --from=build --chown=campaign:campaign /out/metrics-worker /metrics-worker
USER campaign:campaign
EXPOSE 8096
ENTRYPOINT ["/metrics-worker"]
