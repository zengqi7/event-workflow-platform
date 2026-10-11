#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

version="$(tr -d '\r\n' < VERSION)"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]
grep -Fq "release \`v$version\`" docs/support-matrix.md
grep -Eq '^go 1\.27\.2$' go.mod
grep -Fq 'FROM golang:1.27.2-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS build' Dockerfile
grep -Fq 'mysql:8.4@sha256:6ea90827b1100f8f2ae306a539f86d2c264a26ed435a2a9f75551dd5c3aeb242' compose.yaml
grep -Fq 'redis:7.4-alpine@sha256:858f009f9709ce576febc734aa78b8f6d624b82571f9ddb6bda4377c833b3499' compose.yaml
grep -Fq '| Go | 1.27.x for source builds | 1.27.2 |' docs/support-matrix.md
grep -Fq '| MySQL | 8.4 LTS | 8.4.11,' docs/support-matrix.md
grep -Fq '| Redis | 7.4 | 7.4.11,' docs/support-matrix.md

echo 'support matrix metadata is consistent'
