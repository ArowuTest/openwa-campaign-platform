FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/inbound-governance-worker ./cmd/inbound-governance-worker
FROM alpine:3.20
RUN addgroup -S app && adduser -S -G app app
COPY --from=build /out/inbound-governance-worker /inbound-governance-worker
USER app
EXPOSE 8094
ENTRYPOINT ["/inbound-governance-worker"]
