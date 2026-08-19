# vmtool

Library and CLI/TUI for creating VMs on a Linux machine using KVM/QEMU and libvirt.

The HTTP API is **spec-first**: edit `spec/openapi/vmtool.yaml`, then
`go generate ./internal/api/...`. See [`AGENTS.md`](AGENTS.md) and
[`spec/README.md`](spec/README.md).

## Prerequisites

```bash
sudo apt install libvirt-dev pkg-config qemu-kvm libvirt-daemon-system ansible
```

- **libvirt-dev** — C headers for the Go libvirt bindings
- **pkg-config** — required by cgo to find libvirt
- **qemu-kvm** — KVM/QEMU hypervisor
- **libvirt-daemon-system** — libvirt daemon and default network
- **ansible** — used for VM provisioning and playbook execution

## Build

Go 1.24+.

```bash
go build -o vmtool ./cmd/vmtool
go generate ./internal/api/...          # after editing spec/openapi/vmtool.yaml
scripts/verify-generate.sh             # fail if generated code is stale
```

## Usage

```bash
# Interactive TUI (run from project root)
./vmtool i

# CLI
./vmtool create <name> <image>
./vmtool create web ubuntu.qcow2 --net-type direct --net-source eth0 --macvtap-mode bridge
./vmtool create scratch ubuntu.qcow2 --noclone          # boot the image in place
./vmtool create web ubuntu.qcow2 --extra-disk-size 50   # second empty disk as vdb
./vmtool add-disk web 50                                # attach a new empty disk (vdb, vdc, …)
./vmtool reboot web                                     # ACPI reboot, stays defined
./vmtool list
./vmtool delete <name>
./vmtool delete scratch --noclone                       # undefine only, keep the disk
./vmtool server                                         # REST on 127.0.0.1:9473; GET / is Swagger UI
# Interactive SSH from a client: GET ws://127.0.0.1:9473/vms/<name>/console
```
