package app

import (
	"fmt"
	"path/filepath"
	"strings"
)

// withDeviceID copies extra and sets deviceid to the VM name unless already set.
func withDeviceID(vmName string, extra map[string]string) map[string]string {
	out := make(map[string]string, len(extra)+1)
	for k, v := range extra {
		out[k] = v
	}
	if _, ok := out["deviceid"]; !ok && vmName != "" {
		out["deviceid"] = vmName
	}
	return out
}

func (s *Service) resolvePlaybook(name string) (string, error) {
	if name == "" {
		return "", invalid("playbook", fmt.Errorf("playbook is required"))
	}
	for _, el := range strings.Split(filepath.ToSlash(name), "/") {
		if el == ".." {
			return "", invalid("playbook", fmt.Errorf("playbook path must not contain .."))
		}
	}
	if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
		return "", invalid("playbook", fmt.Errorf("playbook must end in .yml or .yaml"))
	}
	if filepath.IsAbs(name) || strings.ContainsRune(name, filepath.Separator) {
		return name, nil
	}
	base := filepath.Clean(s.playbookDir())
	resolved := filepath.Clean(filepath.Join(base, name))
	if resolved != base && !strings.HasPrefix(resolved, base+string(filepath.Separator)) {
		return "", invalid("playbook", fmt.Errorf("playbook escapes PlaybookDir"))
	}
	return resolved, nil
}
