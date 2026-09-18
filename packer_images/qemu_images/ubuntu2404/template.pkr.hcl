packer {
  required_plugins {
    qemu = {
      version = ">= 1.0.0"
      source  = "github.com/hashicorp/qemu"
    }
  }
}

source "qemu" "ubuntu" {
  iso_url          = "https://releases.ubuntu.com/24.04.4/ubuntu-24.04.4-live-server-amd64.iso"
  iso_checksum     = "sha256:e907d92eeec9df64163a7e454cbc8d7755e8ddc7ed42f99dbc80c40f1a138433"
  output_directory = var.output_directory
  vm_name          = var.distro
  format           = "qcow2"
  disk_size        = "10G"
  memory           = 4096
  cpus             = 4
  accelerator      = "kvm"
  machine_type     = "q35"
  net_device       = "virtio-net-pci"
  disk_interface   = "virtio"
  headless         = true
  boot_wait        = "3s"
  boot_command = [
    "e<wait>",
    "<down><down><down><end>",
    " autoinstall ds=\"nocloud-net;s=http://{{.HTTPIP}}:{{.HTTPPort}}/\" ",
    "<f10>"
  ]
  http_directory   = "http"
  shutdown_command = "echo '${var.password}' | sudo -S shutdown -P now"
  ssh_username     = var.username
  ssh_password     = var.password
  ssh_timeout      = "60m"
}

build {
  sources = ["source.qemu.ubuntu"]

  provisioner "file" {
    source      = "files/99-wildcard.yaml"
    destination = "/tmp/99-wildcard.yaml"
  }

  # The wildcard netplan is what makes the image portable: packer attaches the
  # NIC straight to pcie.0 (ens3), libvirt puts it behind a pcie-root-port
  # (enp1s0), so a config naming one interface is dead on the other host.
  #
  # Deleting 50-cloud-init.yaml is not enough — cloud-init rewrites it on every
  # boot from subiquity's 90-installer-network.cfg, which hardcodes ens3. Drop
  # that file and turn cloud-init's network layer off so 99-wildcard.yaml is the
  # only thing that ever configures the guest NIC.
  provisioner "shell" {
    inline = [
      "sudo rm -f /etc/netplan/50-cloud-init.yaml",
      "sudo rm -f /etc/cloud/cloud.cfg.d/90-installer-network.cfg",
      "printf 'network: {config: disabled}\\n' | sudo tee /etc/cloud/cloud.cfg.d/99-disable-network-config.cfg",
      "sudo cp /tmp/99-wildcard.yaml /etc/netplan/99-wildcard.yaml",
      "sudo chmod 600 /etc/netplan/99-wildcard.yaml",
    ]
  }
}

variable "output_directory" {
  description = "The directory where the output files will be stored."
  type        = string
}

variable "distro" {
  description = "Name of the VM distro, usually short name like ubuntu2404."
  type        = string
}

variable "username" {
  description = "Initial username for the VM."
  type        = string
}

variable "password" {
  description = "Password for the initial user."
  type        = string
}
