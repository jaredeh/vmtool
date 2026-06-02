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

// CloneImage creates a new volume in the default pool by cloning a base image.
// Returns the path to the new volume.
func (m *Manager) CloneImage(baseImage, newName string) (string, error) {
	pool, err := m.conn.LookupStoragePoolByName("default")
	if err != nil {
		return "", fmt.Errorf("looking up default pool: %w", err)
	}
	defer pool.Free()

	srcVol, err := pool.LookupStorageVolByName(baseImage)
	if err != nil {
		return "", fmt.Errorf("looking up base image %q: %w", baseImage, err)
	}
	defer srcVol.Free()

	srcInfo, err := srcVol.GetInfo()
	if err != nil {
		return "", fmt.Errorf("getting base image info: %w", err)
	}

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
		clonedPath, err := m.CloneImage(
			filepath.Base(cfg.DiskPath),
			cfg.Name,
		)
		if err != nil {
			return fmt.Errorf("cloning image: %w", err)
		}
		cfg.DiskPath = clonedPath
	}

	if cfg.DiskSizeGB > 0 {
		if err := m.ResizeVolume(cfg.Name+".qcow2", uint64(cfg.DiskSizeGB)*1024*1024*1024); err != nil {
			return fmt.Errorf("resizing disk: %w", err)
		}
	}

	if err := m.Define(cfg); err != nil {
		return err
	}
	return m.Start(cfg.Name)
}

// ResizeVolume resizes a volume in the default pool to the given size in bytes.
func (m *Manager) ResizeVolume(volName string, sizeBytes uint64) error {
	pool, err := m.conn.LookupStoragePoolByName("default")
	if err != nil {
		return fmt.Errorf("looking up default pool: %w", err)
	}
	defer pool.Free()

	vol, err := pool.LookupStorageVolByName(volName)
	if err != nil {
		return fmt.Errorf("looking up volume %q: %w", volName, err)
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

	// If noclone we don't want to clean up the disk
	if noclone {
		return nil
	}

	// Clean up the cloned volume
	volName := name + ".qcow2"
	pool, err := m.conn.LookupStoragePoolByName("default")
	if err != nil {
		return nil // VM removed, pool lookup is best-effort
	}
	defer pool.Free()

	vol, err := pool.LookupStorageVolByName(volName)
	if err != nil {
		return nil // no matching volume, nothing to clean up
	}
	defer vol.Free()

	if err := vol.Delete(0); err != nil {
		return fmt.Errorf("deleting volume %q: %w", volName, err)
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

	// Parse <target><path>/var/lib/libvirt/images</path></target>
	start := strings.Index(xml, "<target>")
	if start == -1 {
		return "", fmt.Errorf("no <target> in pool XML")
	}
	pathStart := strings.Index(xml[start:], "<path>")
	if pathStart == -1 {
		return "", fmt.Errorf("no <path> in pool XML")
	}
	pathStart += start + len("<path>")
	pathEnd := strings.Index(xml[pathStart:], "</path>")
	if pathEnd == -1 {
		return "", fmt.Errorf("no </path> in pool XML")
	}
	return xml[pathStart : pathStart+pathEnd], nil
}

// ListImages returns the names of .qcow2 volumes in the default storage pool
// using the libvirt storage pool API (no direct filesystem access needed).
func (m *Manager) ListImages() ([]string, error) {
	pool, err := m.conn.LookupStoragePoolByName("default")
	if err != nil {
		return nil, fmt.Errorf("looking up default pool: %w", err)
	}
	defer pool.Free()

	vols, err := pool.ListAllStorageVolumes(0)
	if err != nil {
		return nil, fmt.Errorf("listing pool volumes: %w", err)
	}

	var images []string
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
	return images, nil
}

// ImagePath returns the full filesystem path for a volume in the default pool
// using the libvirt storage volume API.
func (m *Manager) ImagePath(name string) (string, error) {
	pool, err := m.conn.LookupStoragePoolByName("default")
	if err != nil {
		return "", fmt.Errorf("looking up default pool: %w", err)
	}
	defer pool.Free()

	vol, err := pool.LookupStorageVolByName(name)
	if err != nil {
		return "", fmt.Errorf("looking up volume %q: %w", name, err)
	}
	defer vol.Free()

	path, err := vol.GetPath()
	if err != nil {
		return "", fmt.Errorf("getting volume path: %w", err)
	}
	return path, nil
}
