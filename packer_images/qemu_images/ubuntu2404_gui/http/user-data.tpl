#cloud-config
autoinstall:
  version: 1
  identity:
    hostname: ${PKR_VAR_distro}
    username: ${PKR_VAR_username}
    password: "${PKR_VAR_hashedpassword}"
  ssh:
    install-server: true
    allow-pw: true
  # network:
  #   network:
  #     version: 2
  #     ethernets:
  #       all-en:
  #         match:
  #           name: en*
  #         dhcp4: true
  packages:
    - qemu-guest-agent
  storage:
    layout:
      name: direct
  late-commands:
    - echo '${PKR_VAR_username} ALL=(ALL) NOPASSWD:ALL' > /target/etc/sudoers.d/${PKR_VAR_username}
