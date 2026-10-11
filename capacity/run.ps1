param(
    [ValidateSet(1,4,8,16)][int]$WorkerConcurrency = 4,
    [int]$Rate = 20,
    [string]$Duration = '30s',
    [ValidateRange(1,10)][int]$Repetitions = 3,
    [ValidateSet('none','backlog','crash')][string]$RecoveryMode = 'none',
    [string]$OutputRoot = 'results/capacity'
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$corpus = Join-Path $root 'data/capacity/corpus.json'
if (-not (Test-Path -LiteralPath $corpus)) { throw 'prepare the capacity corpus first' }
$k6 = 'grafana/k6:1.3.0@sha256:3ddc8b1a33a2c3d8edc6e99b6a762ae36cba08788463458f5e6a7703e14eb77d'

function Get-FreeTcpPort {
    $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
    $listener.Start()
    try { return ([Net.IPEndPoint]$listener.LocalEndpoint).Port } finally { $listener.Stop() }
}

for ($attempt = 1; $attempt -le $Repetitions; $attempt++) {
    $runId = "event-w${WorkerConcurrency}-r${Rate}-a${attempt}-$(Get-Date -Format 'yyyyMMddHHmmss')"
    $project = ($runId -replace '[^a-z0-9-]', '').ToLowerInvariant()
    $result = Join-Path $root "$OutputRoot/$runId"
    New-Item -ItemType Directory -Force -Path $result | Out-Null
    $envFile = Join-Path $result 'compose.env'
    $mysqlRoot = [Guid]::NewGuid().ToString('N')
    $mysqlApp = [Guid]::NewGuid().ToString('N')
    $httpPort = Get-FreeTcpPort
    @("MYSQL_ROOT_PASSWORD=$mysqlRoot", "MYSQL_PASSWORD=$mysqlApp", "WORKER_CONCURRENCY=$WorkerConcurrency", 'MYSQL_PORT=0', 'REDIS_PORT=0', "HTTP_PORT=$httpPort", 'WEBHOOK_SINK_PORT=0', 'WORKER_METRICS_PORT=0') | Set-Content -LiteralPath $envFile -Encoding ascii
    $compose = @('compose','-p',$project,'-f',(Join-Path $root 'compose.yaml'),'-f',(Join-Path $root 'capacity/compose.capacity.yaml'),'--env-file',$envFile)
    try {
        & docker @compose up -d --build --wait
        if ($LASTEXITCODE -ne 0) { throw 'capacity stack failed to start' }
        $keyOutput = & docker @compose run --rm --no-deps --entrypoint /usr/local/bin/key -e "MYSQL_DSN=root:$mysqlRoot@tcp(mysql:3306)/event_workflow?parseTime=true&charset=utf8mb4&loc=UTC" api -tenant 1 -subject capacity -scopes events:write,state:read -ttl 2h
        $tokenLine = $keyOutput | Where-Object { $_ -like 'token=*' } | Select-Object -First 1
        if (-not $tokenLine) { throw 'capacity credential creation failed' }
        $token = $tokenLine.Substring(6)
        $corpusDir = (Split-Path -Parent $corpus).Replace('\','/')
        $resultDir = $result.Replace('\','/')
        $k6Env = Join-Path $result 'k6.env'
        @("API_TOKEN=$token", "RUN_ID=$runId", "BASE_URL=http://host.docker.internal:$httpPort", "RATE=$Rate", "DURATION=$Duration") | Set-Content -LiteralPath $k6Env -Encoding ascii
        $scriptDir = (Join-Path $root 'capacity').Replace('\','/')
        if ($RecoveryMode -eq 'backlog') { & docker @compose stop worker | Out-Null }
        $k6Name = "$project-k6"
        $k6Args = @('run','-d','--name',$k6Name,'--add-host=host.docker.internal:host-gateway','-v',"${corpusDir}:/corpus:ro",'-v',"${resultDir}:/results",'-v',"${scriptDir}:/scripts:ro",'--env-file',$k6Env,$k6,'run','/scripts/event-load.js')
        & docker @k6Args | Out-Null
        if ($LASTEXITCODE -ne 0) { throw 'k6 container failed to start' }
        $samples = Join-Path $result 'resource-samples.jsonl'
        $sample = 0
        $crashedAt = $null
        while ((& docker inspect -f '{{.State.Running}}' $k6Name 2>$null) -eq 'true') {
            $sample++
            $timestamp = (Get-Date).ToUniversalTime().ToString('o')
            foreach ($line in (& docker stats --no-stream --format '{{json .}}' (& docker @compose ps -q))) {
                @{ timestamp=$timestamp; sample=$sample; container=($line | ConvertFrom-Json) } | ConvertTo-Json -Compress | Add-Content -LiteralPath $samples -Encoding utf8
            }
            if ($RecoveryMode -eq 'crash' -and -not $crashedAt -and $sample -ge 3) {
                & docker @compose kill worker | Out-Null
                $crashedAt = Get-Date
            }
            Start-Sleep -Seconds 1
        }
        $k6Exit = [int](& docker wait $k6Name)
        & docker logs $k6Name | Set-Content -LiteralPath (Join-Path $result 'k6.log') -Encoding utf8
        & docker rm $k6Name | Out-Null
        if ($k6Exit -ne 0) { throw 'k6 load failed' }
        $recoveryStarted = $null
        if ($RecoveryMode -eq 'backlog') {
            $recoveryStarted = Get-Date
            & docker @compose start worker | Out-Null
        }
        if ($RecoveryMode -eq 'crash') {
            Start-Sleep -Seconds 62
            $recoveryStarted = Get-Date
            & docker @compose start worker | Out-Null
        }
        $deadline = (Get-Date).AddMinutes(10)
        $queueSamples = Join-Path $result 'queue-samples.jsonl'
        do {
            $counts = & docker @compose exec -T mysql mysql -N -uroot "-p$mysqlRoot" event_workflow -e "SELECT SUM(status IN ('success','failed')),COUNT(*) FROM workflow_runs WHERE tenant_id=1;"
            $parts = "$counts" -split '\s+'
            $timestamp = (Get-Date).ToUniversalTime().ToString('o')
            @{ timestamp=$timestamp; terminal=if($parts.Count -ge 1){[int]$parts[0]}else{$null}; total=if($parts.Count -ge 2){[int]$parts[1]}else{$null} } | ConvertTo-Json -Compress | Add-Content -LiteralPath $queueSamples -Encoding utf8
            foreach ($line in (& docker stats --no-stream --format '{{json .}}' (& docker @compose ps -q))) {
                @{ timestamp=$timestamp; phase='drain'; container=($line | ConvertFrom-Json) } | ConvertTo-Json -Compress | Add-Content -LiteralPath $samples -Encoding utf8
            }
            if ($parts.Count -ge 2 -and [int]$parts[0] -eq [int]$parts[1] -and [int]$parts[1] -gt 0) { break }
            Start-Sleep -Seconds 1
        } while ((Get-Date) -lt $deadline)
        if ((Get-Date) -ge $deadline) { throw 'queue did not drain before the deadline' }
        $latencies = & docker @compose exec -T mysql mysql -N -uroot "-p$mysqlRoot" event_workflow -e "SELECT TIMESTAMPDIFF(MICROSECOND,e.created_at,r.finished_at)/1000 FROM workflow_runs r JOIN events e ON e.id=r.event_id WHERE r.tenant_id=1 AND r.finished_at IS NOT NULL ORDER BY 1;"
        $latencies | Set-Content -LiteralPath (Join-Path $result 'completion-latency-ms.txt') -Encoding ascii
        $completion = & docker @compose exec -T mysql mysql -N -uroot "-p$mysqlRoot" event_workflow -e "SELECT COUNT(*),TIMESTAMPDIFF(MICROSECOND,MIN(e.created_at),MAX(r.finished_at))/1000000 FROM workflow_runs r JOIN events e ON e.id=r.event_id WHERE r.tenant_id=1 AND r.finished_at IS NOT NULL;"
        $completionParts = "$completion" -split '\s+'
        $drainedAt = Get-Date
        $completedCount = [int]$completionParts[0]; $processingSeconds = [double]$completionParts[1]
        @{ run_id=$runId; worker_concurrency=$WorkerConcurrency; rate=$Rate; duration=$Duration; http_port=$httpPort; completed_count=$completedCount; processing_window_seconds=$processingSeconds; completion_throughput_rps=if($processingSeconds -gt 0){$completedCount/$processingSeconds}else{$null}; recovery_mode=$RecoveryMode; crashed_at=if($crashedAt){$crashedAt.ToUniversalTime().ToString('o')}else{$null}; recovery_started_at=if($recoveryStarted){$recoveryStarted.ToUniversalTime().ToString('o')}else{$null}; queue_drain_seconds=if($recoveryStarted){[Math]::Round(($drainedAt-$recoveryStarted).TotalSeconds,3)}else{$null}; completed_at=$drainedAt.ToUniversalTime().ToString('o') } | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $result 'run.json') -Encoding utf8
    } finally {
        & docker @compose down -v --remove-orphans | Out-Null
        Remove-Item -LiteralPath $envFile -Force -ErrorAction SilentlyContinue
        Remove-Item -LiteralPath (Join-Path $result 'k6.env') -Force -ErrorAction SilentlyContinue
    }
}
