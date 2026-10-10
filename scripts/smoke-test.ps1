param(
  [string]$BaseUrl = 'http://127.0.0.1:8080',
  [string]$SinkUrl = 'http://127.0.0.1:18081'
)
$ErrorActionPreference = 'Stop'
$seed = [guid]::NewGuid().ToString('N')
$occurredAt = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
$body = @{
  tenant_code  = 'acme'
  source       = 'smoke-test'
  event_type   = 'order.timeout'
  event_id     = "evt-$seed"
  business_key = "order-$seed"
  occurred_at  = $occurredAt
  payload      = @{order_id=$seed;amount=6999}
} | ConvertTo-Json -Depth 10

$before = Invoke-RestMethod "$SinkUrl/received"
$first = Invoke-RestMethod -Method Post -Uri "$BaseUrl/api/v1/events" -ContentType 'application/json' -Body $body
$reused = Invoke-RestMethod -Method Post -Uri "$BaseUrl/api/v1/events" -ContentType 'application/json' -Body $body
if ($first.event_id -ne $reused.event_id) { throw 'Transport idempotency returned a different database event' }

$conflictBody = $body | ConvertFrom-Json
$conflictBody.payload.amount = 7000
try {
  Invoke-RestMethod -Method Post -Uri "$BaseUrl/api/v1/events" -ContentType 'application/json' -Body ($conflictBody | ConvertTo-Json -Depth 10)
  throw 'Expected an idempotency conflict for a changed payload'
} catch {
  if ([int]$_.Exception.Response.StatusCode -ne 409) { throw }
}

$dedupBody = $body | ConvertFrom-Json
$dedupBody.event_id = "evt-$seed-duplicate"
$dedup = Invoke-RestMethod -Method Post -Uri "$BaseUrl/api/v1/events" -ContentType 'application/json' -Body ($dedupBody | ConvertTo-Json -Depth 10)
if ($dedup.status -ne 'dedup_skipped') { throw "Expected dedup_skipped, got $($dedup.status)" }

$deadline = (Get-Date).AddSeconds(30)
do {
  Start-Sleep -Seconds 1
  $received = Invoke-RestMethod "$SinkUrl/received"
} while ($received.count -le $before.count -and (Get-Date) -lt $deadline)
if ($received.count -ne ($before.count + 1)) { throw "Expected exactly one new webhook, before=$($before.count) after=$($received.count)" }

$runs = Invoke-RestMethod "$BaseUrl/api/v1/workflow-runs?tenant_id=1&limit=10"
$tasks = Invoke-RestMethod "$BaseUrl/api/v1/workflow-tasks?tenant_id=1&limit=10"
$outbox = Invoke-RestMethod "$BaseUrl/api/v1/outbox-events?tenant_id=1&limit=10"
if (-not ($runs.items | Where-Object { $_.event_id -eq $first.event_id -and $_.status -eq 'success' })) { throw 'Workflow run did not succeed' }
if (-not ($tasks.items | Where-Object { $_.status -eq 'success' })) { throw 'No successful task found' }
if (-not ($outbox.items | Where-Object { $_.status -eq 'published' })) { throw 'No published outbox row found' }

[ordered]@{
  first_event = $first
  idempotent_reuse = $reused
  business_duplicate = $dedup
  webhook = $received.records[-1]
} | ConvertTo-Json -Depth 20
