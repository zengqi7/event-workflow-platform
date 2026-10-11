$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot

function Assert-Contains([string]$Path, [string]$Value) {
    $content = Get-Content -LiteralPath (Join-Path $root $Path) -Raw -Encoding UTF8
    if (-not $content.Contains($Value)) {
        throw "$Path does not contain expected value: $Value"
    }
}

$version = (Get-Content -LiteralPath (Join-Path $root 'VERSION') -Raw -Encoding UTF8).Trim()
if ($version -ne '0.1.0') { throw "VERSION must be 0.1.0, got $version" }

Assert-Contains 'go.mod' "go 1.27.2"
Assert-Contains 'Dockerfile' 'FROM golang:1.27.2-alpine'
Assert-Contains 'compose.yaml' 'image: mysql:8.4'
Assert-Contains 'compose.yaml' 'image: redis:7.4-alpine'
Assert-Contains 'docs/support-matrix.md' '| Go | 1.27.x for source builds | 1.27.2 |'
Assert-Contains 'docs/support-matrix.md' '| MySQL | 8.4 LTS | 8.4.11,'
Assert-Contains 'docs/support-matrix.md' '| Redis | 7.4 | 7.4.11,'

Write-Output 'support matrix metadata is consistent'
