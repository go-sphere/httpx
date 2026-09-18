#!/usr/bin/env bash
set -euo pipefail

version=${1:?Usage: check-release.sh vX.Y.Z}
repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
go_bin=${GO:-go}
# Read go.mod itself, without workspace or dependency graph overrides.
for adapter in ginx fiberx echox hertzx stdx; do
  required=$(awk '$1 == "github.com/go-sphere/httpx" { print $2 }' "$repo_dir/$adapter/go.mod")
  if [[ "$required" != "$version" ]]; then
    printf '%s requires httpx %s, expected %s. Publish the root version, then run make prepare-release TAG=%s.\n' "$adapter" "$required" "$version" "$version" >&2
    exit 1
  fi
done
for adapter in ginx fiberx echox hertzx stdx; do
  (
    cd "$repo_dir/$adapter"
    GOWORK=off "$go_bin" mod tidy -diff
    GOWORK=off "$go_bin" test -mod=readonly -count=1 ./...
  )
done
