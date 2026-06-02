packer {
  required_plugins {
    qemu = {
      version = ">= 1.0.0"
      source  = "github.com/hashicorp/qemu"
    }
  }
}

source "qemu" "ubuntu" {
  iso_url          = "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"
  iso_checksum     = "53fdde898feed8b027d94baa9cfe8229867f330a1d9c49dc7d84465ee7f229f7"
  disk_image       = true
  cd_files         = ["./http/user-data", "./http/meta-data"]
  cd_label         = "cidata"
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
  http_directory   = "http"
  shutdown_command = "echo '${var.password}' | sudo -S shutdown -P now"
  ssh_username     = var.username
  ssh_password     = var.password
  ssh_timeout      = "60m"
}

build {
  sources = ["source.qemu.ubuntu"]

  provisioner "shell" {
    inline = [
      "sudo rm -f /etc/netplan/50-cloud-init.yaml",
      "sudo rm -f /etc/ssh/sshd_config.d/60-cloudimg-settings.conf",
      "sudo ssh-keygen -A",
      "sudo touch /etc/cloud/cloud-init.disabled",
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
