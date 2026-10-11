# Local Development

## Requirements

- Go version from go.mod (currently 1.27.2)
- Docker Compose v2 for integration dependencies
- PowerShell 7 for repository scripts

All Go build caches used by `scripts/test.ps1` are stored under `E:\Cache\codex-project-build`.

## Run the complete stack

```powershell
Copy-Item .env.example .env
docker compose up --build -d
$keyOutput = docker compose run --rm --no-deps --entrypoint key api -tenant 1 -subject local-demo -scopes events:write,state:read -ttl 24h
$env:EVENT_API_TOKEN = ($keyOutput | Where-Object { $_ -like 'token=*' }) -replace '^token=', ''
.\scripts\smoke-test.ps1
```

Inspect services with `docker compose ps` and logs with `docker compose logs -f api worker`.

## Run Go processes on the host

Start only MySQL and Redis:

```powershell
docker compose up -d mysql redis
$env:MYSQL_DSN='root:<MYSQL_ROOT_PASSWORD>@tcp(127.0.0.1:<MYSQL_PORT>)/event_workflow?parseTime=true&charset=utf8mb4&loc=UTC'
$env:REDIS_ADDR='127.0.0.1:6379'
go run ./cmd/migrate
$env:MYSQL_DSN='event_app:<MYSQL_PASSWORD>@tcp(127.0.0.1:<MYSQL_PORT>)/event_workflow?parseTime=true&charset=utf8mb4&loc=UTC'
```

Then run `cmd/http-server`, `cmd/worker`, and optionally `cmd/webhook-sink` in separate terminals. Host defaults use MySQL `127.0.0.1:3306`, Redis `127.0.0.1:6379`, API `127.0.0.1:8080`, and sink `:18081`.

Read endpoints derive tenant from the key, for example:

```powershell
Invoke-RestMethod 'http://127.0.0.1:8080/api/v1/workflow-runs' -Headers @{ Authorization="Bearer $env:EVENT_API_TOKEN" }
```

## Verification

```powershell
.\scripts\test.ps1
$env:INTEGRATION_MYSQL_DSN='root:password@tcp(127.0.0.1:3306)/event_workflow?parseTime=true&charset=utf8mb4&loc=UTC'
$env:INTEGRATION_REDIS_ADDR='127.0.0.1:6379'
go test -race -tags=integration ./integration
```

## Linux/macOS source entry points

Set MYSQL_DSN and REDIS_ADDR using your secret/config store, then run `go run ./cmd/migrate`, `go run ./cmd/http-server` and `go run ./cmd/worker` in separate supervised terminals. Issue a key with `go run ./cmd/key -tenant 1 -subject local-demo -scopes events:write,state:read -ttl 24h`; send `Authorization: Bearer <token>` with curl. Set WEBHOOK_ALLOWED_HOSTS and operator secret mounts before processing; only explicit development mode permits the local sink. Docker Compose provides the same documented API on all three host systems. Cross-platform archives and recovery steps are in release-and-operations.md.
