#!/usr/bin/env bash
set -euo pipefail

dir="${1:?asset directory required}"
version="${2:?version required}"
prefix="event-workflow-platform-$version"
expected=(
  "benchmark-evidence.tar.gz"
  "$prefix-darwin-amd64.tar.gz"
  "$prefix-darwin-arm64.tar.gz"
  "$prefix-linux-amd64.tar.gz"
  "$prefix-linux-arm64.tar.gz"
  "$prefix-windows-amd64.zip"
  "$prefix-windows-arm64.zip"
)

mapfile -t actual < <(find "$dir" -maxdepth 1 -type f \( -name '*.tar.gz' -o -name '*.zip' \) -print | sed 's#^.*/##' | sort)
mapfile -t wanted < <(printf '%s\n' "${expected[@]}" | sort)
[[ "${actual[*]}" == "${wanted[*]}" ]] || { echo "release archive set mismatch" >&2; printf 'actual: %s\n' "${actual[@]}" >&2; exit 1; }
[[ -f "$dir/checksums.txt" ]]
[[ "$(wc -l < "$dir/checksums.txt")" -eq 7 ]]
(cd "$dir" && sha256sum -c checksums.txt)
for file in "${expected[@]}"; do grep -Eq "(^|[[:space:]])(\./)?${file//./\.}$" "$dir/checksums.txt"; done
echo 'release archive set and checksums verified'
