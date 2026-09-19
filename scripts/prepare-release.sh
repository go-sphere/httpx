#!/usr/bin/env bash
set -euo pipefail

version=${1:?Usage: prepare-release.sh vX.Y.Z}
if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([+-][a-zA-Z0-9.-]+)?$ ]]; then
  echo "Invalid release version: $version" >&2
  exit 1
fi
repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
go_bin=${GO:-go}

# The root tag was pushed moments ago, so every cached answer for it is
# suspect: a proxy can still be holding a negative or pre-re-push response
# (goproxy.cn 404s a tag that exists on GitHub), and a version that was
# deleted and pushed again under the same name leaves a copy in the local
# module cache that `go` would reuse without going to the network. Resolve the
# root module straight from GitHub, from a cache entry cleared first. Set
# RELEASE_GOPROXY to a proxy list if GitHub is not reachable from here.
export GOPROXY="${RELEASE_GOPROXY:-direct}"
export GOWORK=off
mod_cache=$("$go_bin" env GOMODCACHE)
mod_download=$mod_cache/cache/download/github.com/go-sphere/httpx/@v
rm -f "$mod_download/$version.info" "$mod_download/$version.mod" \
      "$mod_download/$version.zip" "$mod_download/$version.ziphash"
if [[ -d "$mod_cache/github.com/go-sphere/httpx@$version" ]]; then
  chmod -R u+w "$mod_cache/github.com/go-sphere/httpx@$version"
  rm -rf "$mod_cache/github.com/go-sphere/httpx@$version"
fi

# Check availability before mutating any module. The root must be published
# first; an adapter tag cannot bootstrap its own missing root dependency.
"$go_bin" mod download "github.com/go-sphere/httpx@$version"
for adapter in ginx fiberx echox hertzx stdx; do
  (
    cd "$repo_dir/$adapter"
    "$go_bin" get "github.com/go-sphere/httpx@$version"
    "$go_bin" mod tidy
  )
done
printf 'Dependencies updated to %s. Review and commit go.mod/go.sum, then run make release-check TAG=%s.\n' "$version" "$version"
