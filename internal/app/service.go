package app

import (
	"path/filepath"
	"time"

	"github.com/jaredeh/vmtool/pkg/vmtool"
)

// Manager is the libvirt surface Service needs.
// *vmtool.Manager satisfies this. There is no Create method:
// CreateVM calls CloneImage / ResizeVolume / Define / Start itself.
type Manager interface {
	Start(name string) error
	Stop(name string) error
	Resume(name string) error
	Destroy(name string) error
	Delete(name string, noclone bool) error
	Info(name string) (*vmtool.VMInfo, error)
	List() ([]vmtool.VMInfo, error)
	WaitForIP(name string, timeout time.Duration) (string, error)
	SetAutostart(name string, enabled bool) error
	Reboot(name string) error
	ResizeDisk(name string, sizeGB uint) error
	MigrateDisk(name, pool string) error
	CreateVolume(pool, name string, sizeGB uint) (string, error)
	AddDisk(name string, sizeGB uint, pool string) (*vmtool.DiskDevice, error)
	ImagePath(name string) (string, error)
	ListNetworks() ([]string, error)
	ListBridges() ([]string, error)
	ListImagesByPool() (map[string][]string, error)
	DeleteImage(name, pool string) error
	ListPools() ([]vmtool.PoolInfo, error)
	CreatePool(name, path string) error
	CloneImage(basePath, newName, targetPool string) (string, error)
	ResizeVolume(volPath string, sizeBytes uint64) error
	Define(cfg vmtool.VMConfig) error
}

type Service struct {
	PlaybookDir   string
	InventoryPath string
	IPTimeout     time.Duration
}

func (s *Service) playbookDir() string {
	if s.PlaybookDir == "" {
		return filepath.Join("ansible", "playbooks")
	}
	return s.PlaybookDir
}

func (s *Service) ipTimeout() time.Duration {
	if s.IPTimeout <= 0 {
		return 120 * time.Second
	}
	return s.IPTimeout
}

type ProgressFunc func(ProgressEvent)

type ProgressEvent struct {
	Stage  string
	Status string
	Detail string
	Output string
}

type CreateVMInput struct {
	Name        string
	Image       string
	VCPUs       uint
	Memory      uint // GiB
	DiskSizeGB  uint
	Pool        string
	NetType     string
	NetSource   string
	MacvtapMode string
	SSHUser     string
	SSHPass     string
	Playbook        string
	Noclone         bool
	ExtraDiskSizeGB uint
	ExtraDiskPool   string
	ExtraVars       map[string]string
	OnProgress      ProgressFunc
}

type CmdResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

type PlaybookResult struct {
	Output string
}

type ResizeResult struct {
	VM              vmtool.VMInfo
	FilesystemGrown bool
	GrowError       string
}

type AddDiskResult struct {
	VM     vmtool.VMInfo
	Path   string
	Target string
}

func emit(in CreateVMInput, stage, status, detail, output string) {
	if in.OnProgress != nil {
		in.OnProgress(ProgressEvent{Stage: stage, Status: status, Detail: detail, Output: output})
	}
}
