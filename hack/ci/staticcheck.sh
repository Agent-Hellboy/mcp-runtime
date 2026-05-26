#!/usr/bin/env bash
set -euo pipefail

gopath="$(go env GOPATH)"
bin="${gopath}/bin/staticcheck"

if [[ ! -x "${bin}" ]]; then
  echo "staticcheck not found at ${bin}. Install with:" >&2
  echo "  GOTOOLCHAIN=go1.24.11 go install honnef.co/go/tools/cmd/staticcheck@latest" >&2
  exit 1
fi

exec "${bin}" "$@"
