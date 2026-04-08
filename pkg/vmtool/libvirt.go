package vmtool

import (
	"fmt"
	"net"
	"strings"
	"time"

	"libvirt.org/go/libvirt"
)

// Manager wraps a libvirt connection and provides VM lifecycle operations.
type Manager struct {
	conn *libvirt.Connect
}

// NewManager opens a connection to the local libvirt daemon.
func NewManager() (*Manager, error) {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return nil, fmt.Errorf("connecting to libvirt: %w", err)
	}
	return &Manager{conn: conn}, nil
}

// Close releases the libvirt connection.
func (m *Manager) Close() error {
	_, err := m.conn.Close()
	return err
}

// domainXML builds a full libvirt domain XML from a VMConfig.
// Uses machine type "pc" (i440fx) to match packer's QEMU builder default,
// ensuring the guest NIC gets the same PCI address / interface name as
// during the packer build.
func domainXML(cfg VMConfig) string {
	return fmt.Sprintf(`<domain type='kvm'>
  <name>%s</name>
  <memory unit='MiB'>%d</memory>
  <vcpu>%d</vcpu>
  <os>
    <type arch='x86_64' machine='pc'>hvm</type>
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

// Create defines and starts a VM in one step.
func (m *Manager) Create(cfg VMConfig) error {
	if err := m.Define(cfg); err != nil {
		return err
	}
	return m.Start(cfg.Name)
}

// Delete stops (force) and undefines a VM.
func (m *Manager) Delete(name string) error {
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
	return dom.Undefine()
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

	vi := &VMInfo{
		Name:      name,
		State:     domainState(state),
		VCPUs:     uint(info.NrVirtCpu),
		MemoryMiB: uint(info.MaxMem / 1024),
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

		vi := VMInfo{
			Name:      name,
			State:     domainState(state),
			VCPUs:     uint(info.NrVirtCpu),
			MemoryMiB: uint(info.MaxMem / 1024),
		}
		if state == libvirt.DOMAIN_RUNNING {
			vi.IP, _ = m.getIP(&dom)
		}
		vms = append(vms, vi)
		dom.Free()
	}
	return vms, nil
}

// getIP attempts to retrieve the IP address from a running domain's DHCP leases.
func (m *Manager) getIP(dom *libvirt.Domain) (string, error) {
	ifaces, err := dom.ListAllInterfaceAddresses(libvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_LEASE)
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		for _, addr := range iface.Addrs {
			if addr.Type == libvirt.IP_ADDR_TYPE_IPV4 {
				return addr.Addr, nil
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
