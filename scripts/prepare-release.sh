#!/usr/bin/env bash
set -euo pipefail

version=${1:?Usage: prepare-release.sh vX.Y.Z}
if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([+-][a-zA-Z0-9.-]+)?$ ]]; then
  echo "Invalid release version: $version" >&2
  exit 1
fi
repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
go_bin=${GO:-go}
# Check availability before mutating any module. The root must be published
# first; an adapter tag cannot bootstrap its own missing root dependency.
GOWORK=off "$go_bin" mod download "github.com/go-sphere/httpx@$version"
for adapter in ginx fiberx echox hertzx stdx; do
  (
    cd "$repo_dir/$adapter"
    GOWORK=off "$go_bin" get "github.com/go-sphere/httpx@$version"
    GOWORK=off "$go_bin" mod tidy
  )
done
printf 'Dependencies updated to %s. Review and commit go.mod/go.sum, then run make release-check TAG=%s.\n' "$version" "$version"
