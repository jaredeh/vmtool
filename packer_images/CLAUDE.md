# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Packer-based QEMU/KVM VM image builder. Builds qcow2 images and installs them into libvirt. Currently one distro: `ubuntu2404`.

## Commands

```bash
./run generate    # generate user-data from template, run packer build, install to libvirt
./run clean       # delete output for current distro
./run distclean   # delete all output
```

`./run generate` does the full pipeline:
1. `envsubst` renders `http/user-data.tpl` → `http/user-data` (cloud-init autoinstall config)
2. `packer init .` + `packer build .` in `qemu_images/ubuntu2404/`
3. Copies built qcow2 to `/var/lib/libvirt/images/ubuntu2404.qcow2` (requires sudo)
4. `virsh pool-refresh default`

## Architecture

```
run                              # main entry script, sets PKR_VAR_* env vars
qemu_images/ubuntu2404/
  template.pkr.hcl               # Packer QEMU builder config
  http/
    user-data.tpl                # cloud-init autoinstall template (envsubst vars)
    user-data                    # generated at build time (gitignored)
    meta-data                    # empty, required by cloud-init
  files/
    99-wildcard.yaml             # netplan config pushed into image
keys/                            # SSH keys added to authorized_keys (gitignored)
output/                          # packer build output, qcow2 files (gitignored)
```

## Key variables

All set in `run`, exported as `PKR_VAR_*` for Packer:

| Var | Default |
|-----|---------|
| `PKR_VAR_distro` | `ubuntu2404` |
| `PKR_VAR_username` | `packer` |
| `PKR_VAR_password` | `packer` |
| `PKR_VAR_hashedpassword` | sha512crypt of password |
| `PKR_VAR_output_directory` | `output/ubuntu2404` |

## Adding a new distro

1. Create `qemu_images/<distro>/` mirroring ubuntu2404 structure
2. Update `PKR_VAR_distro` in `run`
3. Update ISO url/checksum in `template.pkr.hcl`
4. Adjust boot commands if distro installer differs
