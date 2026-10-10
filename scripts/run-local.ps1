$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
    Write-Host "Starting local dependencies..."
    docker compose up -d mysql redis
    if ($LASTEXITCODE -ne 0) { throw 'Docker Compose startup failed' }

    $settings = @{}
    Get-Content .env | Where-Object { $_ -match '^[^#][^=]*=' } | ForEach-Object {
        $name, $value = $_ -split '=', 2
        $settings[$name.Trim()] = $value.Trim()
    }
    $mysqlPort = if ($settings.MYSQL_PORT) { $settings.MYSQL_PORT } else { '3306' }
    $mysqlPassword = $settings.MYSQL_PASSWORD
    if (-not $mysqlPassword) { throw 'MYSQL_PASSWORD is missing from .env' }

    Write-Host "Running migration..."
    $env:MYSQL_DSN = "event_app:$mysqlPassword@tcp(127.0.0.1:$mysqlPort)/event_workflow?parseTime=true&charset=utf8mb4&loc=UTC"
    $env:REDIS_ADDR = "127.0.0.1:$($settings.REDIS_PORT ?? '6379')"
    go run ./cmd/migrate
    if ($LASTEXITCODE -ne 0) { throw 'Database migration failed' }

    Write-Host "Start services in separate terminals:"
    Write-Host "  go run ./cmd/http-server"
    Write-Host "  go run ./cmd/worker"
    Write-Host "  go run ./cmd/webhook-sink"
} finally {
    Pop-Location
}
