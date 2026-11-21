#!/usr/bin/env sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT INT TERM

if ! command -v qemu-aarch64-static >/dev/null 2>&1; then
    echo "qemu-aarch64-static is required (install qemu-user-static)" >&2
    exit 1
fi

cd "$repo_root"

GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
    go test -c -o "$tmp_dir/compiler-arm64.test" ./core/compiler
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
    go test -c -o "$tmp_dir/jjs-arm64.test" .

qemu-aarch64-static "$tmp_dir/compiler-arm64.test" -test.v
qemu-aarch64-static "$tmp_dir/jjs-arm64.test" -test.v
