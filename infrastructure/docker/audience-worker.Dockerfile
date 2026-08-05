FROM golang:1.23.2-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/audience-worker ./cmd/audience-worker \
    && mkdir -p /out/object-store

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/audience-worker /audience-worker
COPY --from=build --chown=nonroot:nonroot /out/object-store /var/lib/campaign-platform/objects
USER nonroot:nonroot
EXPOSE 8091
ENTRYPOINT ["/audience-worker"]
