$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot

function Assert-Contains([string]$Path, [string]$Value) {
    $content = Get-Content -LiteralPath (Join-Path $root $Path) -Raw -Encoding UTF8
    if (-not $content.Contains($Value)) {
        throw "$Path does not contain expected value: $Value"
    }
}

$version = (Get-Content -LiteralPath (Join-Path $root 'VERSION') -Raw -Encoding UTF8).Trim()
if ($version -notmatch '^\d+\.\d+\.\d+$') { throw "invalid VERSION: $version" }
Assert-Contains 'docs/support-matrix.md' ('release `v' + $version + '`')

Assert-Contains 'go.mod' "go 1.27.2"
Assert-Contains 'Dockerfile' 'FROM golang:1.27.2-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS build'
Assert-Contains 'compose.yaml' 'mysql:8.4@sha256:6ea90827b1100f8f2ae306a539f86d2c264a26ed435a2a9f75551dd5c3aeb242'
Assert-Contains 'compose.yaml' 'redis:7.4-alpine@sha256:858f009f9709ce576febc734aa78b8f6d624b82571f9ddb6bda4377c833b3499'
Assert-Contains 'docs/support-matrix.md' '| Go | 1.27.x for source builds | 1.27.2 |'
Assert-Contains 'docs/support-matrix.md' '| MySQL | 8.4 LTS | 8.4.11,'
Assert-Contains 'docs/support-matrix.md' '| Redis | 7.4 | 7.4.11,'

Write-Output 'support matrix metadata is consistent'
