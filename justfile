# vmtool — build, generate, test.
# `go generate ./internal/api/...` remains the required codegen entrypoint.

set shell := ["bash", "-euo", "pipefail", "-c"]

root := justfile_directory()
bin := "vmtool"

default:
    @just --list

# Build ./vmtool
build:
    cd "{{ root }}" && go build -o {{ bin }} ./cmd/vmtool

# Regenerate internal/api from spec/openapi/vmtool.yaml
generate:
    cd "{{ root }}" && go generate ./internal/api/...

# Fail if generated API code is stale
verify:
    "{{ root }}/scripts/verify-generate.sh"

# Run all Go tests
test:
    cd "{{ root }}" && go test ./...

# Pre-push: stale-generate check + tests
check: verify test

# Remove the built binary
clean:
    rm -f "{{ root }}/{{ bin }}"
