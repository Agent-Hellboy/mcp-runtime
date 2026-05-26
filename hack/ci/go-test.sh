#!/usr/bin/env bash
set -euo pipefail

if [ $# -lt 1 ]; then
  echo "Usage: $0 <module-dir> [go-test-args...]" >&2
  exit 2
fi

module_dir="$1"
shift

: "${GOCACHE:=/tmp/go-build}"
: "${GOMODCACHE:=/tmp/go-mod}"
mkdir -p "$GOCACHE" "$GOMODCACHE"
export GOCACHE GOMODCACHE

if [ $# -eq 0 ]; then
  (cd "$module_dir" && go test ./...)
else
  (cd "$module_dir" && go test "$@")
fi
