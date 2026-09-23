FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/relay ./cmd/relay
COPY internal/protocol ./internal/protocol
COPY internal/relay ./internal/relay
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/codex-relay ./cmd/relay

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/codex-relay /codex-relay
EXPOSE 8080
ENTRYPOINT ["/codex-relay", "-listen", ":8080"]

