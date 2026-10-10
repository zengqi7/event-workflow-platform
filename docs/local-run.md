# Local Development

## Requirements

- Go 1.26+
- Docker Compose v2 for integration dependencies
- PowerShell 7 for repository scripts

All Go build caches used by `scripts/test.ps1` are stored under `E:\Cache\codex-project-build`.

## Run the complete stack

```powershell
Copy-Item .env.example .env
docker compose up --build -d
.\scripts\smoke-test.ps1
```

Inspect services with `docker compose ps` and logs with `docker compose logs -f api worker`.

## Run Go processes on the host

Start only MySQL and Redis:

```powershell
docker compose up -d mysql redis
$env:MYSQL_DSN='event_app:<MYSQL_PASSWORD>@tcp(127.0.0.1:<MYSQL_PORT>)/event_workflow?parseTime=true&charset=utf8mb4&loc=UTC'
$env:REDIS_ADDR='127.0.0.1:6379'
go run ./cmd/migrate
```

Then run `cmd/http-server`, `cmd/worker`, and optionally `cmd/webhook-sink` in separate terminals. Host defaults use MySQL `127.0.0.1:3306`, Redis `127.0.0.1:6379`, API `127.0.0.1:8080`, and sink `:18081`.

Read endpoints require a tenant selector, for example:

```powershell
Invoke-RestMethod 'http://127.0.0.1:8080/api/v1/workflow-runs?tenant_id=1'
```

## Verification

```powershell
.\scripts\test.ps1
$env:INTEGRATION_MYSQL_DSN='root:password@tcp(127.0.0.1:3306)/event_workflow?parseTime=true&charset=utf8mb4&loc=UTC'
$env:INTEGRATION_REDIS_ADDR='127.0.0.1:6379'
go test -race -tags=integration ./integration
```
