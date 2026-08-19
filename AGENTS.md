# Project rules for vmtool

**vmtool** — CLI, TUI, and REST for KVM/QEMU VMs via libvirt, plus Ansible playbooks.

## Hard rules

1. **Spec-first API.** Change `spec/openapi/vmtool.yaml` first. Run `go generate ./internal/api/...` (or `scripts/verify-generate.sh`). Update `internal/app`, adapters (`internal/server`, `internal/cli`, `internal/tui`), and tests in the same change. Do not hand-edit `internal/api/api.gen.go`. Do not add a route to `Handler()` that is not in the spec unless it is listed in the exclusion register in [`spec/README.md`](spec/README.md).
2. **One workflow layer.** CLI, TUI, and REST call `internal/app`. Do not reimplement create / playbook / resize / lifecycle in a surface. `pkg/vmtool` stays primitives (libvirt, ansible, SSH, inventory).
3. **Secrets.** Never commit `.machines/`, guest passwords, or SSH private keys. Do not log `ssh_pass` or request bodies.
4. **Generated artifacts.** `internal/api/api.gen.go` is produced by oapi-codegen. Regenerate; do not patch.
5. **Loopback REST.** Default bind is `127.0.0.1:9473` with no auth. Binding off-loopback is unsupported.

## Spec-first change procedure

1. Edit `spec/openapi/vmtool.yaml`.
2. `go generate ./internal/api/...`
3. Implement or adjust `internal/app` and the three adapters.
4. `scripts/verify-generate.sh` and `go test ./...` before push.

See [`spec/README.md`](spec/README.md) and [`docs/design/spec-first.md`](docs/design/spec-first.md).
