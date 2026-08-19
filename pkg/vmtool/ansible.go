package vmtool

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func extraVarArgs(extraVars map[string]string) []string {
	if len(extraVars) == 0 {
		return nil
	}
	keys := make([]string, 0, len(extraVars))
	for k := range extraVars {
		if k == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+extraVars[k])
	}
	return out
}

// ListPlaybooks returns the names of .yml files in the given directory.
func ListPlaybooks(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading playbook directory: %w", err)
	}
	var playbooks []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasSuffix(n, ".yml") || strings.HasSuffix(n, ".yaml") {
			playbooks = append(playbooks, e.Name())
		}
	}
	return playbooks, nil
}

// RunPlaybook executes an ansible-playbook against the given inventory file.
// extraVars are passed as repeated --extra-vars key=value.
// Returns the combined stdout/stderr output and any error.
func RunPlaybook(inventoryPath, playbookPath string, extraVars map[string]string) (string, error) {
	args := []string{"--inventory", inventoryPath, playbookPath}
	for _, kv := range extraVarArgs(extraVars) {
		args = append(args, "--extra-vars", kv)
	}
	cmd := exec.Command("ansible-playbook", args...)
	cmd.Env = ansibleEnv()
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
	cmd.Env = ansibleEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("running command: %w", err)
	}
	return string(out), nil
}

func ansibleEnv() []string {
	return append(os.Environ(),
		"ANSIBLE_HOST_KEY_CHECKING=False",
		"ANSIBLE_SSH_ARGS=-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR",
	)
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
func EnsureInventory(path, name, ip string, auth Auth) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return true, WriteInventory(path, name, ip, auth)
	}
	if strings.Contains(string(data), "ansible_host: "+ip) {
		return false, nil
	}
	return true, WriteInventory(path, name, ip, auth)
}
