package vmtool

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"libvirt.org/go/libvirt"
)

// Manager wraps a libvirt connection and provides VM lifecycle operations.
type Manager struct {
	conn *libvirt.Connect
}

// NewManager opens a connection to the local libvirt daemon and ensures
// the default NAT network exists and is active.
func NewManager() (*Manager, error) {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return nil, fmt.Errorf("connecting to libvirt: %w", err)
	}
	mgr := &Manager{conn: conn}
	if err := mgr.EnsureDefaultNetwork(); err != nil {
		mgr.Close()
		return nil, fmt.Errorf("ensuring default network: %w", err)
	}
	return mgr, nil
}

// Close releases the libvirt connection.
func (m *Manager) Close() error {
	_, err := m.conn.Close()
	return err
}

// domainXML builds a full libvirt domain XML from a VMConfig.
func domainXML(cfg VMConfig) string {
	return fmt.Sprintf(`<domain type='kvm'>
  <name>%s</name>
  <memory unit='MiB'>%d</memory>
  <vcpu>%d</vcpu>
  <os>
    <type arch='x86_64' machine='q35'>hvm</type>
    <boot dev='hd'/>
  </os>
  <features>
    <acpi/>
    <apic/>
  </features>
  <cpu mode='host-passthrough' check='none'/>
  <devices>
    <disk type='file' device='disk'>
      <driver name='qemu' type='qcow2'/>
      <source file='%s'/>
      <target dev='vda' bus='virtio'/>
    </disk>
%s
    <serial type='pty'>
      <target port='0'/>
    </serial>
    <console type='pty'>
      <target type='serial' port='0'/>
    </console>
    <graphics type='spice' autoport='yes'>
      <listen type='address'/>
      <image compression='off'/>
    </graphics>
    <video>
      <model type='qxl' ram='65536' vram='65536' vgamem='16384' heads='1' primary='yes'/>
    </video>
    <channel type='unix'>
      <target type='virtio' name='org.qemu.guest_agent.0'/>
    </channel>
    <memballoon model='virtio'/>
    <rng model='virtio'>
      <backend model='random'>/dev/urandom</backend>
    </rng>
  </devices>
</domain>`, cfg.Name, cfg.MemoryMiB, cfg.VCPUs, cfg.DiskPath, networkXML(cfg.Network))
}

// Define registers a VM with libvirt without starting it.
func (m *Manager) Define(cfg VMConfig) error {
	xml := domainXML(cfg)
	dom, err := m.conn.DomainDefineXML(xml)
	if err != nil {
		return fmt.Errorf("defining domain: %w", err)
	}
	if err := dom.Free(); err != nil {
		return err
	}
	return nil
}

// Start boots a previously defined VM.
func (m *Manager) Start(name string) error {
	dom, err := m.conn.LookupDomainByName(name)
	if err != nil {
		return fmt.Errorf("looking up domain %q: %w", name, err)
	}
	defer dom.Free()
	return dom.Create()
}

// Stop requests a graceful ACPI shutdown of the VM.
func (m *Manager) Stop(name string) error {
	dom, err := m.conn.LookupDomainByName(name)
	if err != nil {
		return fmt.Errorf("looking up domain %q: %w", name, err)
	}
	defer dom.Free()
	return dom.Shutdown()
}

// Destroy forces an immediate power-off of the VM.
func (m *Manager) Destroy(name string) error {
	dom, err := m.conn.LookupDomainByName(name)
	if err != nil {
		return fmt.Errorf("looking up domain %q: %w", name, err)
	}
	defer dom.Free()
	return dom.Destroy()
}

// Undefine removes the VM definition from libvirt.
func (m *Manager) Undefine(name string) error {
	dom, err := m.conn.LookupDomainByName(name)
	if err != nil {
		return fmt.Errorf("looking up domain %q: %w", name, err)
	}
	defer dom.Free()
	return dom.Undefine()
}

// CloneImage clones basePath into targetPool under newName.qcow2 and returns the new volume path.
func (m *Manager) CloneImage(basePath, newName, targetPool string) (string, error) {
	srcVol, err := m.conn.LookupStorageVolByPath(basePath)
	if err != nil {
		return "", fmt.Errorf("looking up base image at %q: %w", basePath, err)
	}
	defer srcVol.Free()

	srcInfo, err := srcVol.GetInfo()
	if err != nil {
		return "", fmt.Errorf("getting base image info: %w", err)
	}

	pool, err := m.conn.LookupStoragePoolByName(targetPool)
	if err != nil {
		return "", fmt.Errorf("looking up pool %q: %w", targetPool, err)
	}
	defer pool.Free()

	volName := newName + ".qcow2"
	volXML := fmt.Sprintf(`<volume>
  <name>%s</name>
  <capacity>%d</capacity>
  <target>
    <format type='qcow2'/>
  </target>
</volume>`, volName, srcInfo.Capacity)

	newVol, err := pool.StorageVolCreateXMLFrom(volXML, srcVol, 0)
	if err != nil {
		return "", fmt.Errorf("cloning volume: %w", err)
	}
	defer newVol.Free()

	path, err := newVol.GetPath()
	if err != nil {
		return "", fmt.Errorf("getting new volume path: %w", err)
	}
	return path, nil
}

// Create clones the base image, optionally resizes it, defines, and starts a VM.
// cfg.DiskPath should be the path to the base image (from ImagePath).
// It will be replaced with the path to the cloned volume.
func (m *Manager) Create(cfg VMConfig) error {
	if !cfg.Noclone {
		clonedPath, err := m.CloneImage(cfg.DiskPath, cfg.Name, cfg.Pool)
		if err != nil {
			return fmt.Errorf("cloning image: %w", err)
		}
		cfg.DiskPath = clonedPath
	}

	if cfg.DiskSizeGB > 0 {
		if err := m.ResizeVolume(cfg.DiskPath, uint64(cfg.DiskSizeGB)*1024*1024*1024); err != nil {
			return fmt.Errorf("resizing disk: %w", err)
		}
	}

	if err := m.Define(cfg); err != nil {
		return err
	}
	return m.Start(cfg.Name)
}

// ResizeVolume resizes a volume (by path) to the given size in bytes.
func (m *Manager) ResizeVolume(volPath string, sizeBytes uint64) error {
	vol, err := m.conn.LookupStorageVolByPath(volPath)
	if err != nil {
		return fmt.Errorf("looking up volume at %q: %w", volPath, err)
	}
	defer vol.Free()
	return vol.Resize(sizeBytes, 0)
}

// Delete stops (force), undefines a VM, and removes its cloned disk volume.
func (m *Manager) Delete(name string, noclone bool) error {
	dom, err := m.conn.LookupDomainByName(name)
	if err != nil {
		return fmt.Errorf("looking up domain %q: %w", name, err)
	}
	defer dom.Free()

	var diskPath string
	if !noclone {
		if xmlDesc, err := dom.GetXMLDesc(0); err == nil {
			diskPath = parseDiskSourcePath(xmlDesc)
		}
	}

	state, _, err := dom.GetState()
	if err != nil {
		return fmt.Errorf("getting state: %w", err)
	}
	if state == libvirt.DOMAIN_RUNNING || state == libvirt.DOMAIN_PAUSED {
		if err := dom.Destroy(); err != nil {
			return fmt.Errorf("destroying domain: %w", err)
		}
	}

	if err := dom.Undefine(); err != nil {
		return fmt.Errorf("undefining domain: %w", err)
	}

	if noclone || diskPath == "" {
		return nil
	}

	vol, err := m.conn.LookupStorageVolByPath(diskPath)
	if err != nil {
		return nil // no matching volume, nothing to clean up
	}
	defer vol.Free()

	if err := vol.Delete(0); err != nil {
		return fmt.Errorf("deleting volume at %q: %w", diskPath, err)
	}
	return nil
}

func domainState(state libvirt.DomainState) VMState {
	switch state {
	case libvirt.DOMAIN_RUNNING:
		return StateRunning
	case libvirt.DOMAIN_SHUTOFF:
		return StateShutoff
	case libvirt.DOMAIN_PAUSED:
		return StatePaused
	case libvirt.DOMAIN_CRASHED:
		return StateCrashed
	default:
		return StateUnknown
	}
}

// Info returns runtime information about a single VM.
func (m *Manager) Info(name string) (*VMInfo, error) {
	dom, err := m.conn.LookupDomainByName(name)
	if err != nil {
		return nil, fmt.Errorf("looking up domain %q: %w", name, err)
	}
	defer dom.Free()

	info, err := dom.GetInfo()
	if err != nil {
		return nil, fmt.Errorf("getting domain info: %w", err)
	}

	state, _, err := dom.GetState()
	if err != nil {
		return nil, fmt.Errorf("getting state: %w", err)
	}

	autostart, _ := dom.GetAutostart()

	vi := &VMInfo{
		Name:      name,
		State:     domainState(state),
		VCPUs:     uint(info.NrVirtCpu),
		MemoryMiB: uint(info.MaxMem / 1024),
		Autostart: autostart,
	}

	if state == libvirt.DOMAIN_RUNNING {
		vi.IP, _ = m.getIP(dom)
	}

	return vi, nil
}

// List returns info for all VMs managed by libvirt.
func (m *Manager) List() ([]VMInfo, error) {
	domains, err := m.conn.ListAllDomains(libvirt.CONNECT_LIST_DOMAINS_ACTIVE | libvirt.CONNECT_LIST_DOMAINS_INACTIVE)
	if err != nil {
		return nil, fmt.Errorf("listing domains: %w", err)
	}

	var vms []VMInfo
	for _, dom := range domains {
		name, err := dom.GetName()
		if err != nil {
			dom.Free()
			continue
		}
		info, err := dom.GetInfo()
		if err != nil {
			dom.Free()
			continue
		}
		state, _, err := dom.GetState()
		if err != nil {
			dom.Free()
			continue
		}

		autostart, _ := dom.GetAutostart()
		vi := VMInfo{
			Name:      name,
			State:     domainState(state),
			VCPUs:     uint(info.NrVirtCpu),
			MemoryMiB: uint(info.MaxMem / 1024),
			Autostart: autostart,
		}
		if state == libvirt.DOMAIN_RUNNING {
			vi.IP, _ = m.getIP(&dom)
		}
		vms = append(vms, vi)
		dom.Free()
	}
	return vms, nil
}

// getIP attempts to retrieve the IP address from a running domain.
// It tries DHCP leases first (works for libvirt-managed networks), then
// falls back to ARP (works for bridge/direct-attached interfaces).
func (m *Manager) getIP(dom *libvirt.Domain) (string, error) {
	sources := []libvirt.DomainInterfaceAddressesSource{
		libvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_LEASE,
		libvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_AGENT,
		libvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_ARP,
	}
	for _, src := range sources {
		ifaces, err := dom.ListAllInterfaceAddresses(src)
		if err != nil {
			continue
		}
		for _, iface := range ifaces {
			if iface.Name == "lo" {
				continue
			}
			for _, addr := range iface.Addrs {
				if addr.Type == libvirt.IP_ADDR_TYPE_IPV4 && !net.ParseIP(addr.Addr).IsLoopback() {
					return addr.Addr, nil
				}
			}
		}
	}
	return "", fmt.Errorf("no IPv4 address found")
}

// WaitForIP polls until the VM has an IP address or the timeout is reached.
func (m *Manager) WaitForIP(name string, timeout time.Duration) (string, error) {
	dom, err := m.conn.LookupDomainByName(name)
	if err != nil {
		return "", fmt.Errorf("looking up domain %q: %w", name, err)
	}
	defer dom.Free()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ip, err := m.getIP(dom)
		if err == nil && net.ParseIP(ip) != nil {
			return ip, nil
		}
		time.Sleep(time.Second)
	}
	return "", fmt.Errorf("timed out waiting for IP on %q", name)
}

// EnsureDefaultNetwork creates and starts the default NAT network if it doesn't exist.
func (m *Manager) EnsureDefaultNetwork() error {
	net, err := m.conn.LookupNetworkByName("default")
	if err == nil {
		// Network exists, make sure it's active
		active, err := net.IsActive()
		if err != nil {
			net.Free()
			return fmt.Errorf("checking network status: %w", err)
		}
		if !active {
			if err := net.Create(); err != nil {
				net.Free()
				return fmt.Errorf("starting default network: %w", err)
			}
		}
		net.Free()
		return nil
	}

	// Create the default network
	netXML := `<network>
  <name>default</name>
  <forward mode='nat'>
    <nat>
      <port start='1024' end='65535'/>
    </nat>
  </forward>
  <bridge name='virbr0' stp='on' delay='0'/>
  <ip address='192.168.122.1' netmask='255.255.255.0'>
    <dhcp>
      <range start='192.168.122.2' end='192.168.122.254'/>
    </dhcp>
  </ip>
</network>`

	net, err = m.conn.NetworkDefineXML(netXML)
	if err != nil {
		return fmt.Errorf("defining default network: %w", err)
	}
	defer net.Free()

	if err := net.SetAutostart(true); err != nil {
		return fmt.Errorf("setting network autostart: %w", err)
	}
	if err := net.Create(); err != nil {
		return fmt.Errorf("starting default network: %w", err)
	}
	return nil
}

// ListBridges returns host bridge interfaces, excluding libvirt-managed ones (virbr*).
func (m *Manager) ListBridges() ([]string, error) {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return nil, fmt.Errorf("reading /sys/class/net: %w", err)
	}
	var bridges []string
	for _, e := range entries {
		name := e.Name()
		// A directory with a "bridge" subdirectory is a bridge device
		if _, err := os.Stat(filepath.Join("/sys/class/net", name, "bridge")); err != nil {
			continue
		}
		// Skip libvirt-managed bridges — those should be used via NAT mode
		if strings.HasPrefix(name, "virbr") {
			continue
		}
		bridges = append(bridges, name)
	}
	return bridges, nil
}

// ListNetworks returns the names of available libvirt networks.
func (m *Manager) ListNetworks() ([]string, error) {
	nets, err := m.conn.ListAllNetworks(libvirt.CONNECT_LIST_NETWORKS_ACTIVE)
	if err != nil {
		return nil, fmt.Errorf("listing networks: %w", err)
	}
	var names []string
	for _, n := range nets {
		name, err := n.GetName()
		if err == nil {
			names = append(names, name)
		}
		n.Free()
	}
	return names, nil
}

// GetAutostart returns whether the VM is set to start automatically on host boot.
func (m *Manager) GetAutostart(name string) (bool, error) {
	dom, err := m.conn.LookupDomainByName(name)
	if err != nil {
		return false, fmt.Errorf("looking up domain %q: %w", name, err)
	}
	defer dom.Free()
	return dom.GetAutostart()
}

// SetAutostart enables or disables automatic start of the VM on host boot.
func (m *Manager) SetAutostart(name string, autostart bool) error {
	dom, err := m.conn.LookupDomainByName(name)
	if err != nil {
		return fmt.Errorf("looking up domain %q: %w", name, err)
	}
	defer dom.Free()
	return dom.SetAutostart(autostart)
}

// Reboot requests a reboot of the VM.
func (m *Manager) Reboot(name string) error {
	dom, err := m.conn.LookupDomainByName(name)
	if err != nil {
		return fmt.Errorf("looking up domain %q: %w", name, err)
	}
	defer dom.Free()
	return dom.Reboot(0)
}

// parsePoolPath extracts the <path> value from a storage pool XML descriptor.
func parsePoolPath(xml string) string {
	si := strings.Index(xml, "<path>")
	if si == -1 {
		return ""
	}
	si += len("<path>")
	ei := strings.Index(xml[si:], "</path>")
	if ei == -1 {
		return ""
	}
	return xml[si : si+ei]
}

// parseDiskSourcePath extracts the disk source file path from a domain XML descriptor.
func parseDiskSourcePath(xml string) string {
	const marker = "<source file='"
	si := strings.Index(xml, marker)
	if si == -1 {
		return ""
	}
	si += len(marker)
	ei := strings.Index(xml[si:], "'")
	if ei == -1 {
		return ""
	}
	return xml[si : si+ei]
}

// DefaultPoolPath returns the filesystem path of the "default" storage pool.
func (m *Manager) DefaultPoolPath() (string, error) {
	pool, err := m.conn.LookupStoragePoolByName("default")
	if err != nil {
		return "", fmt.Errorf("looking up default pool: %w", err)
	}
	defer pool.Free()

	xml, err := pool.GetXMLDesc(0)
	if err != nil {
		return "", fmt.Errorf("getting pool XML: %w", err)
	}

	path := parsePoolPath(xml)
	if path == "" {
		return "", fmt.Errorf("could not parse path from pool XML")
	}
	return path, nil
}

// ListImages returns the names of .qcow2 volumes across all storage pools.
func (m *Manager) ListImages() ([]string, error) {
	pools, err := m.conn.ListAllStoragePools(0)
	if err != nil {
		return nil, fmt.Errorf("listing storage pools: %w", err)
	}
	var images []string
	for _, pool := range pools {
		vols, err := pool.ListAllStorageVolumes(0)
		if err != nil {
			pool.Free()
			continue
		}
		for _, vol := range vols {
			name, err := vol.GetName()
			vol.Free()
			if err != nil {
				continue
			}
			if strings.HasSuffix(name, ".qcow2") {
				images = append(images, name)
			}
		}
		pool.Free()
	}
	return images, nil
}

// ImagePath searches all storage pools for a volume by name and returns its path.
func (m *Manager) ImagePath(name string) (string, error) {
	pools, err := m.conn.ListAllStoragePools(0)
	if err != nil {
		return "", fmt.Errorf("listing storage pools: %w", err)
	}
	for _, pool := range pools {
		vol, err := pool.LookupStorageVolByName(name)
		if err != nil {
			pool.Free()
			continue
		}
		path, pathErr := vol.GetPath()
		vol.Free()
		pool.Free()
		if pathErr != nil {
			return "", fmt.Errorf("getting volume path: %w", pathErr)
		}
		return path, nil
	}
	return "", fmt.Errorf("image %q not found in any storage pool", name)
}

// PoolInfo holds metadata about a storage pool.
type PoolInfo struct {
	Name   string
	Path   string
	Active bool
}

// ListPools returns metadata for all storage pools.
func (m *Manager) ListPools() ([]PoolInfo, error) {
	pools, err := m.conn.ListAllStoragePools(0)
	if err != nil {
		return nil, fmt.Errorf("listing storage pools: %w", err)
	}
	var result []PoolInfo
	for _, pool := range pools {
		name, _ := pool.GetName()
		active, _ := pool.IsActive()
		path := ""
		if xmlDesc, err := pool.GetXMLDesc(0); err == nil {
			path = parsePoolPath(xmlDesc)
		}
		result = append(result, PoolInfo{Name: name, Path: path, Active: active})
		pool.Free()
	}
	return result, nil
}

// CreatePool defines a new directory-type storage pool at path, starts it, and sets autostart.
func (m *Manager) CreatePool(name, path string) error {
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("creating pool directory: %w", err)
	}
	poolXML := fmt.Sprintf(`<pool type='dir'>
  <name>%s</name>
  <target>
    <path>%s</path>
  </target>
</pool>`, name, path)

	pool, err := m.conn.StoragePoolDefineXML(poolXML, 0)
	if err != nil {
		return fmt.Errorf("defining pool: %w", err)
	}
	defer pool.Free()

	if err := pool.Create(0); err != nil {
		return fmt.Errorf("starting pool: %w", err)
	}
	if err := pool.SetAutostart(true); err != nil {
		return fmt.Errorf("setting pool autostart: %w", err)
	}
	return nil
}

// MigrateDisk copies a VM's disk to targetPool, redefines the domain with the new path,
// and restarts the VM if it was running. The VM is force-stopped during the copy.
func (m *Manager) MigrateDisk(vmName, targetPool string) error {
	dom, err := m.conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("looking up domain %q: %w", vmName, err)
	}
	defer dom.Free()

	state, _, err := dom.GetState()
	if err != nil {
		return fmt.Errorf("getting VM state: %w", err)
	}
	wasRunning := state == libvirt.DOMAIN_RUNNING || state == libvirt.DOMAIN_PAUSED

	xmlDesc, err := dom.GetXMLDesc(0)
	if err != nil {
		return fmt.Errorf("getting domain XML: %w", err)
	}
	oldPath := parseDiskSourcePath(xmlDesc)
	if oldPath == "" {
		return fmt.Errorf("could not find disk source path in domain XML")
	}

	if wasRunning {
		if err := dom.Destroy(); err != nil {
			return fmt.Errorf("stopping VM: %w", err)
		}
	}

	oldVol, err := m.conn.LookupStorageVolByPath(oldPath)
	if err != nil {
		return fmt.Errorf("looking up volume at %q: %w", oldPath, err)
	}
	defer oldVol.Free()

	pool, err := m.conn.LookupStoragePoolByName(targetPool)
	if err != nil {
		return fmt.Errorf("looking up pool %q: %w", targetPool, err)
	}
	defer pool.Free()

	volInfo, err := oldVol.GetInfo()
	if err != nil {
		return fmt.Errorf("getting volume info: %w", err)
	}
	volName, err := oldVol.GetName()
	if err != nil {
		return fmt.Errorf("getting volume name: %w", err)
	}

	volXML := fmt.Sprintf(`<volume>
  <name>%s</name>
  <capacity>%d</capacity>
  <target>
    <format type='qcow2'/>
  </target>
</volume>`, volName, volInfo.Capacity)

	newVol, err := pool.StorageVolCreateXMLFrom(volXML, oldVol, 0)
	if err != nil {
		return fmt.Errorf("copying volume to pool %q: %w", targetPool, err)
	}
	defer newVol.Free()

	newPath, err := newVol.GetPath()
	if err != nil {
		return fmt.Errorf("getting new volume path: %w", err)
	}

	newXML := strings.Replace(xmlDesc, oldPath, newPath, 1)
	if _, err := m.conn.DomainDefineXML(newXML); err != nil {
		return fmt.Errorf("redefining domain: %w", err)
	}

	if err := oldVol.Delete(0); err != nil {
		return fmt.Errorf("deleting old volume: %w", err)
	}

	if wasRunning {
		dom2, err := m.conn.LookupDomainByName(vmName)
		if err != nil {
			return fmt.Errorf("looking up domain after redefine: %w", err)
		}
		defer dom2.Free()
		if err := dom2.Create(); err != nil {
			return fmt.Errorf("restarting VM: %w", err)
		}
	}

	return nil
}
