
#cloud-config
hostname: ${PKR_VAR_distro}

# Set up the user
users:
  - name: ${PKR_VAR_username}
    passwd: "${PKR_VAR_hashedpassword}"
    lock_passwd: false
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    groups: [sudo, adm]

# Enable password authentication for the user
ssh_pwauth: true

# Install necessary packages
packages:
  - qemu-guest-agent

# Handle files and network configuration
write_files:
  - path: /etc/netplan/99-wildcard.yaml
    permissions: '0600'
    content: |
      network:
        version: 2
        ethernets:
          all-en:
            match:
              name: "en*"
            dhcp4: true

# Finalize configuration
runcmd:
  - rm -f /etc/netplan/50-cloud-init.yaml
  - netplan apply
  - rm -f /etc/ssh/sshd_config.d/60-cloudimg-settings.conf
  - ssh-keygen -A
  - touch /etc/cloud/cloud-init.disabled
