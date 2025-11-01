###
# Multi-stage Dockerfile for hls-proxy
# - Builds with Go 1.25
# - Produces a minimal distroless image (includes CA certs for HTTPS to HLS origins)
# - Runs as non-root
# - Supports build metadata injection
###

ARG GO_VERSION=1.25
# NOTE: Use an exact Go minor version (e.g. 1.25, 1.25.1). Do not use wildcards like 1.25.x; Docker Hub tags do not support that.

#############################
# Builder stage
#############################
FROM golang:${GO_VERSION}-bookworm AS builder

WORKDIR /src

# Only set CGO (disabled) for static linking. Do not force GOARCH so buildx can override.
ENV CGO_ENABLED=0 GOOS=linux

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build-time metadata (optional; overridable via --build-arg)
ARG VCS_REF="unknown"
ARG BUILD_DATE="unknown"
ARG VERSION="dev"

# -trimpath removes local paths; -ldflags reduce size and embed version info
RUN go build -trimpath -ldflags "-s -w -X main.Version=${VERSION} -X main.BuildDate=${BUILD_DATE} -X main.Commit=${VCS_REF}" -o /out/hls-proxy .

#############################
# Final (minimal) stage
#############################
# Distroless static includes CA certs.
FROM gcr.io/distroless/static:nonroot

WORKDIR /app
COPY --from=builder /out/hls-proxy /app/hls-proxy

ENV PORT=1323
EXPOSE 1323

# Healthcheck calls the binary with a special flag that pings /health
HEALTHCHECK --interval=30s --timeout=3s --start-period=15s --retries=3 \
  CMD ["/app/hls-proxy", "-healthcheck"]

USER nonroot:nonroot

ENTRYPOINT ["/app/hls-proxy"]
CMD []

# Example build:
#   docker build -t bariiss/hls-proxy:dev \
#     --build-arg VCS_REF=$(git rev-parse --short HEAD) \
#     --build-arg BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
#     --build-arg VERSION=dev .
# Multi-arch build (requires buildx):
#   docker buildx build --platform linux/amd64,linux/arm64 -t bariiss/hls-proxy:dev --push .
