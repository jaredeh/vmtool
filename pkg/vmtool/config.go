package vmtool

// NetworkType defines how a VM connects to the network.
type NetworkType string

const (
	// NetworkNAT uses the default libvirt NAT network.
	NetworkNAT NetworkType = "nat"
	// NetworkBridge connects the VM to a host bridge interface.
	NetworkBridge NetworkType = "bridge"
	// NetworkDirect uses macvtap for direct host NIC attachment.
	NetworkDirect NetworkType = "direct"
)

// NetworkConfig describes the VM's network attachment.
type NetworkConfig struct {
	Type   NetworkType
	Source string // network name (nat), bridge name (bridge), or host interface (direct)
}

// VMConfig holds the parameters for defining a VM.
type VMConfig struct {
	Name      string
	VCPUs     uint
	MemoryMiB uint
	DiskPath  string
	Network   NetworkConfig
	SSHUser   string
	SSHPass   string
}

// DefaultConfig returns a VMConfig with sensible defaults.
func DefaultConfig(name, diskPath string) VMConfig {
	return VMConfig{
		Name:      name,
		VCPUs:     2,
		MemoryMiB: 2048,
		DiskPath:  diskPath,
		Network: NetworkConfig{
			Type:   NetworkNAT,
			Source: "default",
		},
		SSHUser: "packer",
		SSHPass: "packer",
	}
}

// VMState represents the running state of a VM.
type VMState string

const (
	StateRunning    VMState = "running"
	StateShutoff    VMState = "shutoff"
	StatePaused     VMState = "paused"
	StateCrashed    VMState = "crashed"
	StateUndefined  VMState = "undefined"
	StateUnknown    VMState = "unknown"
)

// VMInfo holds runtime information about a VM.
type VMInfo struct {
	Name      string
	State     VMState
	VCPUs     uint
	MemoryMiB uint
	IP        string
}
