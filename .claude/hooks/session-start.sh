#!/bin/bash
# mew SessionStart hook (Claude Code on the web): warm the Go module and build
# caches so `make check` / go vet / go test run without a first-use network
# stall. Idempotent and non-interactive. Runs only in the remote environment.
set -euo pipefail

if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
  exit 0
fi

cd "${CLAUDE_PROJECT_DIR:-.}"

if ! command -v go >/dev/null 2>&1; then
  echo "session-start: go toolchain not found on PATH; skipping warmup" >&2
  exit 0
fi

echo "session-start: downloading Go modules..." >&2
go mod download

echo "session-start: warming build cache..." >&2
go build ./... >/dev/null

echo "session-start: ready ($(go version | awk '{print $3}'))" >&2
