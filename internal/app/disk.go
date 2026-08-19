package app

import (
	"context"
	"fmt"

	"github.com/jaredeh/vmtool/pkg/vmtool"
)

func (s *Service) MigrateDisk(_ context.Context, m Manager, name, pool string) (*vmtool.VMInfo, error) {
	if pool == "" {
		return nil, invalid("migrate_disk", fmt.Errorf("pool is required"))
	}
	if err := m.MigrateDisk(name, pool); err != nil {
		return nil, wrap("migrate_disk", err)
	}
	info, err := m.Info(name)
	if err != nil {
		return nil, wrap("migrate_disk", err)
	}
	return info, nil
}

func (s *Service) ResizeDisk(_ context.Context, m Manager, name string, sizeGB uint, auth vmtool.Auth) (*ResizeResult, error) {
	if sizeGB == 0 {
		return nil, invalid("resize_disk", fmt.Errorf("size must be > 0"))
	}
	if err := m.ResizeDisk(name, sizeGB); err != nil {
		return nil, wrap("resize_disk", err)
	}
	info, err := m.Info(name)
	if err != nil {
		return nil, wrap("resize_disk", err)
	}
	res := &ResizeResult{VM: *info}
	if info.State != vmtool.StateRunning || info.IP == "" {
		return res, nil
	}
	resolved := vmtool.ResolveAuth(info.IP, name, auth)
	invPath, cleanup, err := vmtool.TempInventory(name, info.IP, resolved)
	if err != nil {
		res.GrowError = err.Error()
		return res, nil
	}
	defer cleanup()
	if err := vmtool.GrowDisk(invPath); err != nil {
		res.GrowError = err.Error()
		return res, nil
	}
	res.FilesystemGrown = true
	return res, nil
}

func (s *Service) AddDisk(_ context.Context, m Manager, name string, sizeGB uint, pool string) (*AddDiskResult, error) {
	if sizeGB == 0 {
		return nil, invalid("add_disk", fmt.Errorf("size must be > 0"))
	}
	disk, err := m.AddDisk(name, sizeGB, pool)
	if err != nil {
		return nil, wrap("add_disk", err)
	}
	info, err := m.Info(name)
	if err != nil {
		return nil, wrap("add_disk", err)
	}
	return &AddDiskResult{VM: *info, Path: disk.Path, Target: disk.Target}, nil
}
