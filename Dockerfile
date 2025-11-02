ARG GO_VERSION=1.25

#############################
# Builder stage
#############################
FROM golang:${GO_VERSION}-bookworm AS builder

WORKDIR /src

ENV CGO_ENABLED=0 GOOS=linux

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VCS_REF="unknown"
ARG BUILD_DATE="unknown"
ARG VERSION="dev"

RUN go build -trimpath -ldflags "-s -w -X main.Version=${VERSION} -X main.BuildDate=${BUILD_DATE} -X main.Commit=${VCS_REF}" -o /out/hls-proxy .

#############################
# Final (minimal) stage
#############################
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