package vmtool

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteInventory writes or overwrites an Ansible inventory file with the
// given VM's IP and SSH credentials. If the parent directory does not exist,
// the write is silently skipped.
func WriteInventory(path, ip, sshUser, sshPass string) error {
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}
	content := fmt.Sprintf(`all:
  hosts:
    %s:
      ansible_become: true
      ansible_ssh_user: %s
      ansible_ssh_pass: %s
`, ip, sshUser, sshPass)
	return os.WriteFile(path, []byte(content), 0o644)
}
