FROM golang:1.23.2-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/campaign-worker ./cmd/campaign-worker

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/campaign-worker /campaign-worker
USER nonroot:nonroot
EXPOSE 8092
ENTRYPOINT ["/campaign-worker"]
