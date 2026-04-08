package vmtool

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteInventory writes or overwrites an Ansible inventory file with the
// given VM's IP and SSH credentials.
func WriteInventory(path, ip, sshUser, sshPass string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating inventory directory: %w", err)
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
