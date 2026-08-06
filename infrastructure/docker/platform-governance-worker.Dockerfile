FROM golang:1.23.2-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/platform-governance-worker ./cmd/platform-governance-worker

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/platform-governance-worker /platform-governance-worker
USER nonroot:nonroot
EXPOSE 8095
ENTRYPOINT ["/platform-governance-worker"]
