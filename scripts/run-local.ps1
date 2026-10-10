Write-Host "Starting local dependencies..."
docker compose -f deployments/docker-compose.yaml up -d

Write-Host "Waiting for MySQL to accept connections..."
Start-Sleep -Seconds 12

Write-Host "Running migration..."
$env:MYSQL_DSN = "event_app:$($env:MYSQL_PASSWORD ?? 'event-app-local-only')@tcp(127.0.0.1:3306)/event_workflow?parseTime=true&charset=utf8mb4&loc=UTC"
$env:REDIS_ADDR = "127.0.0.1:6379"
go run ./cmd/migrate

Write-Host "Start services in separate terminals:"
Write-Host "  go run ./cmd/http-server"
Write-Host "  go run ./cmd/worker"
Write-Host "  go run ./cmd/webhook-sink"
