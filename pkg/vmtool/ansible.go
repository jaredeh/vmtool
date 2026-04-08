package vmtool

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ListPlaybooks returns the names of .yml files in the given directory.
func ListPlaybooks(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading playbook directory: %w", err)
	}
	var playbooks []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yml") {
			playbooks = append(playbooks, e.Name())
		}
	}
	return playbooks, nil
}

// RunPlaybook executes an ansible-playbook against the given inventory file.
func RunPlaybook(inventoryPath, playbookPath string) error {
	cmd := exec.Command("ansible-playbook",
		"--inventory", inventoryPath,
		playbookPath,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "ANSIBLE_HOST_KEY_CHECKING=False")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("running playbook %s: %w", filepath.Base(playbookPath), err)
	}
	return nil
}

// EnsureInventory checks if the inventory file has the correct IP for the VM,
// and overwrites it if not.
func EnsureInventory(path, ip, sshUser, sshPass string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		// File doesn't exist, write it
		return true, WriteInventory(path, ip, sshUser, sshPass)
	}
	// Check if the IP is already in the file
	if strings.Contains(string(data), ip+":") {
		return false, nil
	}
	return true, WriteInventory(path, ip, sshUser, sshPass)
}
