$ErrorActionPreference = 'Stop'
$env:GOCACHE = 'E:\Cache\codex-project-build\go-build'
$env:GOMODCACHE = 'E:\Cache\codex-project-build\mod'
$env:GOTMPDIR = 'E:\Cache\codex-project-build\tmp'
$env:GOTOOLCHAIN = 'local'
New-Item -ItemType Directory -Force -Path $env:GOCACHE,$env:GOMODCACHE,$env:GOTMPDIR | Out-Null
Push-Location (Split-Path -Parent $PSScriptRoot)
try {
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go tests failed' }
    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go vet failed' }
} finally { Pop-Location }
