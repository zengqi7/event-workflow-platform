#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
mode="${1:-snapshot}"
version="$(tr -d '\r\n' < VERSION)"

[[ "$version" == "0.1.0" ]]
python -m json.tool docs/openapi.json >/dev/null
bash scripts/check-support-matrix.sh
if grep -REq 'uses:[[:space:]]+[^[:space:]]+@v[0-9]' .github/workflows; then
  echo 'workflow actions must be pinned to full commit SHAs' >&2
  exit 1
fi
grep -Fq '@sha256:' Dockerfile
grep -Fq '@sha256:' compose.yaml

if [[ "$mode" == tag ]]; then
  [[ "${GITHUB_REF:-}" == "refs/tags/v0.1.0" ]]
  [[ "${GITHUB_REF_NAME:-}" == "v0.1.0" ]]
  [[ "$(git rev-list -n 1 v0.1.0)" == "${GITHUB_SHA:-}" ]]
  git fetch --quiet origin main
  [[ "$(git rev-parse origin/main)" == "${GITHUB_SHA:-}" ]]
elif [[ "$mode" != snapshot ]]; then
  echo "mode must be tag or snapshot" >&2
  exit 1
fi

echo "release preflight passed for $mode"
