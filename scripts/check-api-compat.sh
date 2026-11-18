#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
GO_BIN=${GO:-go}
APIDIFF_VERSION=${APIDIFF_VERSION:-v0.0.0-20261113113407-e3970619a1a4}
ALLOWLIST="$ROOT_DIR/compat/api-incompatibilities.txt"

if [[ ! -f "$ALLOWLIST" ]]; then
	echo "API compatibility allowlist not found: $ALLOWLIST" >&2
	exit 1
fi

MODULES=(
	".|github.com/go-sphere/httpx"
	"ginx|github.com/go-sphere/httpx/ginx"
	"fiberx|github.com/go-sphere/httpx/fiberx"
	"echox|github.com/go-sphere/httpx/echox"
	"hertzx|github.com/go-sphere/httpx/hertzx"
	"stdx|github.com/go-sphere/httpx/stdx"
)

resolve_baseline_version() {
	local module_dir=$1
	if [[ -n "${API_BASELINE:-}" ]]; then
		printf '%s\n' "$API_BASELINE"
		return
	fi
	local pattern='v[0-9]*'
	if [[ "$module_dir" != "." ]]; then
		pattern="$module_dir/v[0-9]*"
	fi
	local tag
	tag=$(git -C "$ROOT_DIR" describe --tags --abbrev=0 --match "$pattern")
	if git -C "$ROOT_DIR" tag --points-at HEAD | grep -Fxq "$tag"; then
		tag=$(git -C "$ROOT_DIR" describe --tags --abbrev=0 --match "$pattern" HEAD^)
	fi
	if [[ "$module_dir" != "." ]]; then
		tag=${tag#"$module_dir/"}
	fi
	printf '%s\n' "$tag"
}

work_dir=$(mktemp -d)
trap 'chmod -R u+w "$work_dir" 2>/dev/null; rm -rf "$work_dir"' EXIT

apidiff_bin="$work_dir/apidiff"
GOBIN="$work_dir" GOWORK=off "$GO_BIN" install "golang.org/x/exp/cmd/apidiff@$APIDIFF_VERSION"

new_work="$work_dir/go.work"
(
	cd "$work_dir"
	GOWORK=off "$GO_BIN" work init \
		"$ROOT_DIR" \
		"$ROOT_DIR/ginx" \
		"$ROOT_DIR/fiberx" \
		"$ROOT_DIR/echox" \
		"$ROOT_DIR/hertzx" \
		"$ROOT_DIR/stdx"
) >/dev/null

for entry in "${MODULES[@]}"; do
	IFS='|' read -r module_dir module_path <<<"$entry"
	baseline_version=$(resolve_baseline_version "$module_dir")
	temp_dir=$(mktemp -d "$work_dir/module.XXXXXX")
	baseline_dir="$temp_dir/baseline"
	old_api="$temp_dir/old.api"
	new_api="$temp_dir/new.api"
	changes_file="$temp_dir/changes.txt"
	mkdir -p "$baseline_dir"

	(
		cd "$baseline_dir"
		GOWORK=off "$GO_BIN" mod init httpx-api-baseline >/dev/null
		GOWORK=off "$GO_BIN" get "$module_path@$baseline_version" >/dev/null
		GOWORK=off "$apidiff_bin" -m -w "$old_api" "$module_path"
	)

	(
		cd "$ROOT_DIR/$module_dir"
		GOWORK="$new_work" "$apidiff_bin" -m -w "$new_api" "$module_path"
	)

	if [[ ! -s "$old_api" || ! -s "$new_api" ]]; then
		echo "Failed to export API snapshot for $module_path" >&2
		exit 1
	fi

	if ! GOWORK=off "$apidiff_bin" -m -incompatible "$old_api" "$new_api" >"$changes_file"; then
		echo "apidiff failed comparing $module_path relative to $baseline_version" >&2
		exit 1
	fi

	unexpected_file="$temp_dir/unexpected.txt"
	: >"$unexpected_file"
	while IFS= read -r change; do
		[[ -z "$change" ]] && continue
		if ! grep -Fqx -- "$change" "$ALLOWLIST"; then
			printf '%s\n' "$change" >>"$unexpected_file"
		fi
	done <"$changes_file"

	if [[ -s "$unexpected_file" ]]; then
		echo "Incompatible API changes in $module_path relative to $baseline_version:" >&2
		cat "$unexpected_file" >&2
		exit 1
	fi

	echo "API compatibility check passed for $module_path ($baseline_version)"
done
