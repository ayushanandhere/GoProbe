# GoProbe

GoProbe is a small Go service that keeps an eye on HTTP and TCP endpoints. It runs as a single binary, polls targets on their own schedules, keeps recent health state in memory, and exposes a simple JSON API for inspection and runtime changes.

## What it does

- Monitors HTTP and TCP targets concurrently
- Supports per-target polling intervals and timeouts
- Tracks current status, response time, failure count, and rolling uptime
- Exposes a REST API to inspect targets and add or remove them at runtime
- Emits structured JSON logs with `slog`
- Shuts down cleanly on `SIGINT` and `SIGTERM`

## Requirements

- Go 1.22+

## Quick Start

Clone the repository, build the binary, and start the monitor:

```bash
git clone https://github.com/ayushanandhere/GoProbe.git
cd GoProbe
go build -o goprobe .
./goprobe
```

Use a different config file if needed:

```bash
./goprobe -config /path/to/config.yaml
```

The server listens on port `8080` by default.

If `server.auth_token` is not set, GoProbe generates an ephemeral bearer token for `POST` and `DELETE` requests at startup and logs it once. You can also set `GOPROBE_API_TOKEN` in the environment or `server.auth_token` in the config file to keep the token stable across restarts.

## Configuration

GoProbe reads a YAML config file with server settings, monitor defaults, and the initial target list.

```yaml
server:
  port: 8080
  auth_token: "change-me"

monitor:
  default_interval: 10s
  default_timeout: 5s

targets:
  - name: "Google"
    type: "http"
    endpoint: "https://www.google.com"
    interval: 15s
    timeout: 3s

  - name: "Redis Local"
    type: "tcp"
    endpoint: "localhost:6379"
    interval: 5s
```

Notes:

- `type` must be `http` or `tcp`
- `interval` and `timeout` are optional per target
- the minimum `interval` and `timeout` is `1s`
- targets added through the API are kept in memory only and are not written back to `config.yaml`
- runtime-created targets cannot point at localhost, private IP space, link-local addresses, or other special-use IP ranges

## API

### `GET /health`

Basic liveness check for the GoProbe process.

```bash
curl http://localhost:8080/health
```

Example response:

```json
{
  "status": "ok",
  "uptime": "2m14s"
}
```

### `GET /api/targets`

Returns the current state for every monitored target.

```bash
curl http://localhost:8080/api/targets
```

### `GET /api/targets/{name}`

Returns one target by name.

```bash
curl http://localhost:8080/api/targets/Google
```

### `POST /api/targets`

Adds a target at runtime and starts polling it immediately.

```bash
curl -X POST http://localhost:8080/api/targets \
  -H "Authorization: Bearer change-me" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Example",
    "type": "http",
    "endpoint": "https://example.com",
    "interval": "10s",
    "timeout": "3s"
  }'
```

### `DELETE /api/targets/{name}`

Removes a target and stops its polling loop.

```bash
curl -X DELETE http://localhost:8080/api/targets/Example
  -H "Authorization: Bearer change-me"
```

## How it works

Each target gets its own polling goroutine. Every check result is sent through a shared results channel to a single collector goroutine, which updates the in-memory status map. That keeps the write path straightforward and avoids scattering state updates across worker goroutines.

GoProbe keeps only recent status history in memory. It is meant to be a lightweight monitor, not a long-term metrics store.

## Development

Run the test suite:

```bash
go test ./...
```

Run static checks:

```bash
go vet ./...
```

## Possible Extensions

- Prometheus `/metrics`
- Docker packaging
- Health transition alerts
