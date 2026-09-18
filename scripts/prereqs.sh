#!/usr/bin/env bash
# Install vmtool's host prerequisites by running ansible/playbooks/setup_host.yml.
# Ansible itself is the one thing this cannot bootstrap, so it is checked first.
#
# Extra arguments are passed straight through to ansible-playbook:
#   scripts/prereqs.sh -e vmtool_install_go=true
#   scripts/prereqs.sh --check --diff
set -euo pipefail
cd "$(dirname "$0")/.."

playbook="ansible/playbooks/setup_host.yml"

if ! command -v ansible-playbook >/dev/null 2>&1; then
  printf >&2 '%s\n' \
    "ERROR: ansible-playbook not found; install ansible first." \
    "" \
    "  Ubuntu / Debian:  sudo apt install ansible" \
    "  Omarchy / Arch:   sudo pacman -S --needed ansible"
  exit 1
fi

if [[ ! -f $playbook ]]; then
  echo "ERROR: $playbook not found (run this from a vmtool checkout)." >&2
  exit 1
fi

# The playbook needs root. Ask for the sudo password unless sudo is passwordless
# or the caller already passed a --ask-become-pass/-K of their own.
become=()
if ! sudo -n true 2>/dev/null && [[ ! " $* " == *" -K "* && ! " $* " == *" --ask-become-pass "* ]]; then
  become=(--ask-become-pass)
fi

echo "Running $playbook"
exec ansible-playbook "$playbook" "${become[@]}" "$@"
