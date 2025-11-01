# Go hls-proxy 📺

<!-- [START BADGES] -->
<!-- Please keep comment here to allow auto update -->
[![MIT License](https://img.shields.io/github/license/wow-actions/add-badges?style=flat-square)](https://github.com/bariiss/hls-proxy/blob/master/LICENSE)
[![Language](https://img.shields.io/badge/language-golang-teal?style=flat-square)](https://go.dev/)
[![PRs Welcome](https://img.shields.io/badge/PRs-Welcome-brightgreen.svg?style=flat-square)](https://github.com/bariiss/hls-proxy/pulls)
[![build](https://img.shields.io/github/actions/workflow/status/wow-actions/add-badges/release.yml?branch=master&logo=github&style=flat-square)](https://github.com/bariiss/hls-proxy/actions/workflows/go.yml)
<!-- [END BADGES] -->

## ✏️Purpose

A resilient HLS proxy that rewrites manifests, prefetches and decrypts segments, and optionally caches clips in-memory or on disk so streams can be replayed—even when the origin drops segments—while letting you inject custom headers and tuning via configuration.

## 🔧 Changes Compared to bitknox/hls-proxy

- Centralized HTTP timeouts and user-agent handling in `config/config.go` with environment overrides (`HTTP_CLIENT_TIMEOUT`, `HTTP_DIAL_TIMEOUT`, `HTTP_RETRY_REQUEST_DELAY`, `HTTP_RETRY_CLIP_DELAY`, `HTTP_USER_AGENT`).
- Routed all CLI defaults (prefetch behaviour, cache retention, host/port, log level, HTTPS/decrypt flags) through environment-backed settings for easier container configuration.
- Fixed janitor lifecycle by switching `Prefetcher` janitor helpers to pointer receivers and ensuring `fetchClip` reports request-construction errors.
- Updated proxy headers to read the user-agent from configuration instead of a hard-coded constant.
- Added a `/health` endpoint plus a `-healthcheck` flag so containers can perform liveness checks without starting the full server.
- Documented Docker Compose environment variables for the new configuration knobs.
- Bumped GitHub Actions workflow to the latest `actions/checkout` and `actions/setup-go` versions.
- Improved Docker build workflow with dynamic repo-based image naming, default-branch `latest` tagging, and reusable platform matrices.
- Added opt-in segment persistence: in-memory caching (`--segment-cache`) and disk-backed storage (`--segment-store` + `--segment-dir`) with per-manifest limits, idle cleanup, and graceful shutdown purging.
- Reworked manifest processing to stabilize playback sequences, support rewinding via per-manifest histories, and purge idle manifests across cache and disk.
- Introduced configuration-driven inactivity janitor, segment directory sanitization, and signal-based cleanup to prevent stale artifacts.

## 🏎 Getting Started

### Dependencies

- [golang](https://go.dev/doc/install)

### 👨‍💻Installing

```bash
git clone https://github.com/bariiss/hls-proxy.git
cd hls-proxy
go install
hls-proxy
```

### 📝 Usage (JS)

To use the proxy, simply supply the proxy with the url in base64 as shown below. Optionally a referer and origin can be added.

```javascript
//proxy stream
const proxyHost = "http://localhost"
const proxyPort = "1323"
const streamUrl = "https://test-streams.mux.dev/x36xhzz/x36xhzz.m3u8"

const url = `${proxyHost}:${proxyPort}/${btoa(streamUrl)}`

//proxy stream with header
const referer = "https://google.com"
const origin = "https://amazon.com"
//note that origin can be omitted
const input = `${streamUrl}|${referer}|${origin}`
const proxiedUrl = `${proxyHost}:${proxyPort}/${btoa(input)}`
```

## 🆘 Help

```bash
hls-proxy h
```

### Overview of options

```bash
--prefetch                  prefetch ts files (default: true)
--segments value            how many segments to prefetch (default: 30)
--throttle value            how much to throttle prefetch requests (requests per second) (default: 5)
--janitor-interval value    how often should the janitor clean the cache (default: 20s)
--attempts value            how many times to retry a request for a ts file (default: 3)
--clip-retention value      how long to keep ts files in cache (default: 30m0s)
--playlist-retention value  how long to keep playlists in cache (default: 5h0m0s)
--segment-cache             cache fetched segments in memory for replay (default: true)
--segment-store             persist fetched segments to disk for replay (default: false)
--segment-dir value         directory to use when segment storage is enabled (default: "./segments")
--segment-idle-enabled      purge manifests and stored segments after idle window (default: true)
--segment-idle-timeout value  inactivity window before cache/store cleanup (default: 20s)
--host value                hostname to attach to proxy url
--port value                port to attach to proxy url (default: 1323)
--log-level value           log level (default: "PRODUCTION")
--help, -h                  show help
```

## 🧑‍🏭Contributing

Contributions are always welcome. This is one of my first projets in golang, so I'm sure there room for a lot of improvement.

## 📗 Authors

[@bariiss](https://github.com/bariiss)

## 📄 Version History

- 1.4
  - Switch default logging to structured JSON with middleware that records response latency, upstream transfer size, and bytes read/written
  - Track request/response volumes inside the proxy so operators can confirm cache hits and upstream pulls
  - Flatten conditional branches across manifest, prefetch, cache, store, retry, and parsing helpers to simplify maintenance and trim redundant code paths
- 1.3
  - Introduce optional in-memory segment cache, disk persistence, and manifest-aware cleanup controls with new CLI/env settings
  - Rewrite manifest history tracking to keep replayable windows stable and purge idle playlists safely
  - Sanitize per-manifest storage layout, ensuring graceful shutdown removes persisted data
- 1.2
  - Extend configuration via environment/CLI overrides, add healthcheck endpoint/flag, and update Docker build & compose defaults
- 1.1
  - Refactor configuration to drive HTTP settings from env overrides and document fork-specific changes
- 1.0
  - Initial Release
  - See commit change or release history in the GitHub repository

## ©️ License

This project is licensed under the MIT License - see the LICENSE file for details

## 🤚 Acknowledgments

Inspiration:

- Forked from [bitknox/hls-proxy](https://github.com/bitknox/hls-proxy) for continued development and maintenance.
- [HLS-Proxy](https://github.com/warren-bank/HLS-Proxy) by [@warren-bank](https://github.com/warren-bank)
