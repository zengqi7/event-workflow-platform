#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

[[ "$(tr -d '\r\n' < VERSION)" == "0.1.0" ]]
grep -Eq '^go 1\.27\.2$' go.mod
grep -Fq 'FROM golang:1.27.2-alpine' Dockerfile
grep -Fq 'image: mysql:8.4' compose.yaml
grep -Fq 'image: redis:7.4-alpine' compose.yaml
grep -Fq '| Go | 1.27.x for source builds | 1.27.2 |' docs/support-matrix.md
grep -Fq '| MySQL | 8.4 LTS | 8.4.11,' docs/support-matrix.md
grep -Fq '| Redis | 7.4 | 7.4.11,' docs/support-matrix.md

echo 'support matrix metadata is consistent'
