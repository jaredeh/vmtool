# vmtool

Library and CLI/TUI for creating VMs on a Linux machine using KVM/QEMU and libvirt.

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

```bash
go build -o vmtool ./cmd/vmtool
```

## Usage

```bash
# Interactive TUI (run from project root)
./vmtool i

# CLI
./vmtool create <name> <image>
./vmtool list
./vmtool delete <name>
```
