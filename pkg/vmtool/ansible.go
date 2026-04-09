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
// Returns the combined stdout/stderr output and any error.
func RunPlaybook(inventoryPath, playbookPath string) (string, error) {
	cmd := exec.Command("ansible-playbook",
		"--inventory", inventoryPath,
		playbookPath,
	)
	cmd.Env = append(os.Environ(), "ANSIBLE_HOST_KEY_CHECKING=False")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("running playbook %s: %w", filepath.Base(playbookPath), err)
	}
	return string(out), nil
}

// RunCommand runs an ad-hoc ansible command on all hosts in the inventory.
// Returns the combined output and any error.
func RunCommand(inventoryPath, command string) (string, error) {
	cmd := exec.Command("ansible", "all",
		"--inventory", inventoryPath,
		"--become",
		"--module-name", "shell",
		"--args", command,
	)
	cmd.Env = append(os.Environ(), "ANSIBLE_HOST_KEY_CHECKING=False")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("running command: %w", err)
	}
	return string(out), nil
}

// GrowDisk expands the root partition and filesystem to fill the disk.
// Assumes standard Ubuntu direct layout: vda1=ESP, vda2=root.
func GrowDisk(inventoryPath string) error {
	if _, err := RunCommand(inventoryPath, "growpart /dev/vda 2 && resize2fs /dev/vda2"); err != nil {
		return fmt.Errorf("growing disk: %w", err)
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
