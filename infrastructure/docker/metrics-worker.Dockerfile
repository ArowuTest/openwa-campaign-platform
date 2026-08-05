FROM golang:1.23.2-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/metrics-worker ./cmd/metrics-worker

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/metrics-worker /metrics-worker
USER nonroot:nonroot
EXPOSE 8093
ENTRYPOINT ["/metrics-worker"]
