#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
APIDIFF_VERSION=${APIDIFF_VERSION:-v0.0.0-20260218203240-3dfff04db8fa}

MODULES=(
	".|github.com/go-sphere/httpx|v0.0.3"
	"ginx|github.com/go-sphere/httpx/ginx|v0.0.3"
	"fiberx|github.com/go-sphere/httpx/fiberx|v0.0.3"
	"echox|github.com/go-sphere/httpx/echox|v0.0.3"
	"hertzx|github.com/go-sphere/httpx/hertzx|v0.0.3"
)

for entry in "${MODULES[@]}"; do
	IFS='|' read -r module_dir module_path baseline_version <<<"$entry"
	temp_dir=$(mktemp -d)
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
