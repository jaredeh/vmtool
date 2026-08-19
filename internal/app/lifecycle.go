package app

import (
	"context"

	"github.com/jaredeh/vmtool/pkg/vmtool"
)

func (s *Service) GetVM(_ context.Context, m Manager, name string) (*vmtool.VMInfo, error) {
	info, err := m.Info(name)
	if err != nil {
		return nil, wrap("get", err)
	}
	return info, nil
}

func (s *Service) ListVMs(_ context.Context, m Manager) ([]vmtool.VMInfo, error) {
	vms, err := m.List()
	if err != nil {
		return nil, wrap("list", err)
	}
	if vms == nil {
		vms = []vmtool.VMInfo{}
	}
	return vms, nil
}

func (s *Service) DeleteVM(_ context.Context, m Manager, name string, noclone bool) error {
	return wrap("delete", m.Delete(name, noclone))
}

func after(op string, m Manager, name string, err error) (*vmtool.VMInfo, error) {
	if err != nil {
		return nil, wrap(op, err)
	}
	info, ierr := m.Info(name)
	if ierr != nil {
		return nil, wrap(op, ierr)
	}
	return info, nil
}

func (s *Service) StartVM(_ context.Context, m Manager, name string) (*vmtool.VMInfo, error) {
	return after("start", m, name, m.Start(name))
}

func (s *Service) StopVM(_ context.Context, m Manager, name string) (*vmtool.VMInfo, error) {
	return after("stop", m, name, m.Stop(name))
}

func (s *Service) ResumeVM(_ context.Context, m Manager, name string) (*vmtool.VMInfo, error) {
	return after("resume", m, name, m.Resume(name))
}

func (s *Service) PoweroffVM(_ context.Context, m Manager, name string) (*vmtool.VMInfo, error) {
	return after("poweroff", m, name, m.Destroy(name))
}

func (s *Service) RebootVM(_ context.Context, m Manager, name string) (*vmtool.VMInfo, error) {
	return after("reboot", m, name, m.Reboot(name))
}

func (s *Service) SetAutostart(_ context.Context, m Manager, name string, enabled bool) (*vmtool.VMInfo, error) {
	return after("autostart", m, name, m.SetAutostart(name, enabled))
}
