FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY cmd/ cmd/
COPY internal/ internal/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/hetzner-acme-webhook ./cmd/hetzner-acme-webhook

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/hetzner-acme-webhook /hetzner-acme-webhook
ENTRYPOINT ["/hetzner-acme-webhook"]
