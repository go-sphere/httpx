#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
APIDIFF_VERSION=${APIDIFF_VERSION:-v0.0.0-20260218203240-3dfff04db8fa}

# The baseline is the latest released root tag unless API_BASELINE names one;
# adapters are tagged together with the root, so it applies to every module.
API_BASELINE=${API_BASELINE:-$(git -C "$ROOT_DIR" describe --tags --abbrev=0 --match 'v[0-9]*')}

MODULES=(
	".|github.com/go-sphere/httpx|$API_BASELINE"
	"ginx|github.com/go-sphere/httpx/ginx|$API_BASELINE"
	"fiberx|github.com/go-sphere/httpx/fiberx|$API_BASELINE"
	"echox|github.com/go-sphere/httpx/echox|$API_BASELINE"
	"hertzx|github.com/go-sphere/httpx/hertzx|$API_BASELINE"
	"stdx|github.com/go-sphere/httpx/stdx|$API_BASELINE"
)

work_dir=$(mktemp -d)
trap 'chmod -R u+w "$work_dir" 2>/dev/null; rm -rf "$work_dir"' EXIT

for entry in "${MODULES[@]}"; do
	IFS='|' read -r module_dir module_path baseline_version <<<"$entry"
	temp_dir=$(mktemp -d "$work_dir/module.XXXXXX")
	baseline_dir="$temp_dir/baseline"
	old_api="$temp_dir/old.api"
	new_api="$temp_dir/new.api"
	mkdir -p "$baseline_dir"

	(
		cd "$baseline_dir"
		go mod init httpx-api-baseline >/dev/null
		go get "$module_path@$baseline_version" >/dev/null
		go mod download all
		go run "golang.org/x/exp/cmd/apidiff@$APIDIFF_VERSION" \
			-m -w "$old_api" "$module_path"
	)

	(
		cd "$ROOT_DIR/$module_dir"
		go run "golang.org/x/exp/cmd/apidiff@$APIDIFF_VERSION" \
			-m -w "$new_api" "$module_path"
	)

	changes=$(
		go run "golang.org/x/exp/cmd/apidiff@$APIDIFF_VERSION" \
			-m -incompatible "$old_api" "$new_api"
	)
	if [[ -n "$changes" ]]; then
		echo "Incompatible API changes in $module_path relative to $baseline_version:" >&2
		printf '%s\n' "$changes" >&2
		exit 1
	fi

	echo "API compatibility check passed for $module_path"
done
