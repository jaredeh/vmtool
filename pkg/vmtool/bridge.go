package vmtool

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BridgeConfig holds the parameters for creating a host bridge.
type BridgeConfig struct {
	Name     string
	NIC      string
	DHCP     bool
	IP       string // CIDR notation, e.g. "192.168.1.50/24"
	Gateway  string
	DNS      string
	SudoPass string
}

// NICInfo holds the current network configuration of a host interface.
type NICInfo struct {
	IP      string // CIDR notation
	Gateway string
	DNS     string
	DHCP    bool
}

// ListPhysicalNICs returns physical (non-virtual) network interfaces.
func ListPhysicalNICs() ([]string, error) {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return nil, fmt.Errorf("reading /sys/class/net: %w", err)
	}
	var nics []string
	for _, e := range entries {
		name := e.Name()
		if name == "lo" {
			continue
		}
		// Skip virtual interfaces
		if strings.HasPrefix(name, "virbr") ||
			strings.HasPrefix(name, "veth") ||
			strings.HasPrefix(name, "docker") ||
			strings.HasPrefix(name, "br-") {
			continue
		}
		// Skip bridges
		if _, err := os.Stat(filepath.Join("/sys/class/net", name, "bridge")); err == nil {
			continue
		}
		// Only include interfaces that have a physical device backing
		devicePath := filepath.Join("/sys/class/net", name, "device")
		if _, err := os.Stat(devicePath); err != nil {
			continue
		}
		nics = append(nics, name)
	}
	return nics, nil
}

// GetNICInfo reads the current IP configuration of a network interface.
func GetNICInfo(nicName string) NICInfo {
	info := NICInfo{DHCP: true} // default to DHCP

	iface, err := net.InterfaceByName(nicName)
	if err != nil {
		return info
	}

	addrs, err := iface.Addrs()
	if err == nil {
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				ones, _ := ipnet.Mask.Size()
				info.IP = fmt.Sprintf("%s/%d", ipnet.IP.String(), ones)
				break
			}
		}
	}

	// Read default gateway from /proc/net/route
	info.Gateway = readGateway(nicName)

	// Read DNS from /etc/resolv.conf
	info.DNS = readDNS()

	return info
}

// readGateway reads the default gateway for a NIC from /proc/net/route.
func readGateway(nicName string) string {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		if fields[0] != nicName || fields[1] != "00000000" {
			continue
		}
		// Gateway is in hex, little-endian
		gw := fields[2]
		if len(gw) != 8 {
			continue
		}
		var octets [4]uint8
		for i := 0; i < 4; i++ {
			var b uint8
			fmt.Sscanf(gw[i*2:i*2+2], "%02x", &b)
			octets[i] = b
		}
		// /proc/net/route is in host byte order (little-endian on x86)
		return fmt.Sprintf("%d.%d.%d.%d", octets[3], octets[2], octets[1], octets[0])
	}
	return ""
}

// readDNS reads the first nameserver from /etc/resolv.conf.
func readDNS() string {
	f, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "nameserver ") {
			ns := strings.TrimPrefix(line, "nameserver ")
			ns = strings.TrimSpace(ns)
			if ns != "127.0.0.53" { // skip systemd-resolved stub
				return ns
			}
		}
	}
	return ""
}

// CreateBridge creates a Linux bridge via netplan configuration and applies it.
func CreateBridge(cfg BridgeConfig) error {
	var addresses, gateway, dns, dhcp string

	if cfg.DHCP {
		dhcp = "      dhcp4: true"
	} else {
		dhcp = "      dhcp4: false"
		if cfg.IP != "" {
			addresses = fmt.Sprintf("      addresses: [%s]", cfg.IP)
		}
		if cfg.Gateway != "" {
			gateway = cfg.Gateway
		}
		if cfg.DNS != "" {
			dns = fmt.Sprintf("      nameservers:\n        addresses: [%s]", cfg.DNS)
		}
	}

	var lines []string
	lines = append(lines, "network:", "  version: 2")

	// Enslave the physical NIC (no IP config on it)
	lines = append(lines, "  ethernets:", fmt.Sprintf("    %s:", cfg.NIC), "      dhcp4: false")

	// Bridge definition
	lines = append(lines, "  bridges:", fmt.Sprintf("    %s:", cfg.Name))
	lines = append(lines, fmt.Sprintf("      interfaces: [%s]", cfg.NIC))
	lines = append(lines, dhcp)
	if addresses != "" {
		lines = append(lines, addresses)
	}
	if gateway != "" {
		lines = append(lines, "      routes:", fmt.Sprintf("        - to: default\n          via: %s", gateway))
	}
	if dns != "" {
		lines = append(lines, dns)
	}

	content := strings.Join(lines, "\n") + "\n"

	filename := fmt.Sprintf("/etc/netplan/99-vmtool-%s.yaml", cfg.Name)

	// Write netplan config
	if err := sudoWriteFile(cfg.SudoPass, filename, content); err != nil {
		return fmt.Errorf("writing netplan config: %w", err)
	}

	// Apply
	if err := sudoRun(cfg.SudoPass, "netplan", "apply"); err != nil {
		return fmt.Errorf("netplan apply: %w", err)
	}
	return nil
}

// sudoRun executes a command with sudo, piping the password via stdin.
func sudoRun(password string, args ...string) error {
	cmd := exec.Command("sudo", append([]string{"-S"}, args...)...)
	cmd.Stdin = strings.NewReader(password + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// sudoWriteFile writes content to a file via a temp file + sudo mv + sudo chmod.
func sudoWriteFile(password, path, content string) error {
	tmp, err := os.CreateTemp("", "vmtool-*.yaml")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	tmp.Close()

	if err := sudoRun(password, "mv", tmpPath, path); err != nil {
		return err
	}
	return sudoRun(password, "chmod", "600", path)
}
