$ErrorActionPreference = 'Stop'
$cacheRoot = if ($env:PROJECT_CACHE_ROOT) {
    $env:PROJECT_CACHE_ROOT
} elseif (Test-Path -LiteralPath 'E:\') {
    'E:\Cache\codex-project-build'
} else {
    Join-Path $env:TEMP 'codex-project-build'
}
$env:GOCACHE = Join-Path $cacheRoot 'go-build'
$env:GOMODCACHE = Join-Path $cacheRoot 'mod'
$env:GOTMPDIR = Join-Path $cacheRoot 'tmp'
$env:GOTOOLCHAIN = 'auto'
New-Item -ItemType Directory -Force -Path $env:GOCACHE,$env:GOMODCACHE,$env:GOTMPDIR | Out-Null
Push-Location (Split-Path -Parent $PSScriptRoot)
try {
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go tests failed' }
    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go vet failed' }
} finally { Pop-Location }
