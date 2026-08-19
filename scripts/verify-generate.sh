#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
go generate ./internal/api/...
if ! git diff --exit-code -- internal/api; then
  echo "ERROR: generated code is stale. Run: go generate ./internal/api/..."
  exit 1
fi
