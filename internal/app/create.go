package app

import (
	"context"
	"fmt"

	"github.com/jaredeh/vmtool/pkg/vmtool"
)

func (s *Service) CreateVM(_ context.Context, m Manager, in CreateVMInput) (*vmtool.VMInfo, error) {
	if in.Name == "" || in.Image == "" {
		return nil, invalid("create", fmt.Errorf("name and image are required"))
	}
	if in.NetType != "" && in.NetType != "nat" && in.NetType != "bridge" && in.NetType != "direct" {
		return nil, invalid("create", fmt.Errorf("invalid net_type %q", in.NetType))
	}

	diskPath, err := m.ImagePath(in.Image)
	if err != nil {
		return nil, wrap("create", err)
	}
	cfg := vmtool.DefaultConfig(in.Name, diskPath)
	if in.VCPUs > 0 {
		cfg.VCPUs = in.VCPUs
	}
	if in.Memory > 0 {
		cfg.Memory = in.Memory
	}
	cfg.DiskSizeGB = in.DiskSizeGB
	if in.Pool != "" {
		cfg.Pool = in.Pool
	}
	if in.NetType != "" {
		cfg.Network.Type = vmtool.NetworkType(in.NetType)
	}
	if in.NetSource != "" {
		cfg.Network.Source = in.NetSource
	}
	if in.MacvtapMode != "" {
		cfg.Network.MacvtapMode = vmtool.MacvtapMode(in.MacvtapMode)
	}
	if in.SSHUser != "" {
		cfg.SSHUser = in.SSHUser
	}
	if in.SSHPass != "" {
		cfg.SSHPass = in.SSHPass
	}
	cfg.Noclone = in.Noclone
	cfg.ExtraDiskSizeGB = in.ExtraDiskSizeGB
	cfg.ExtraDiskPool = in.ExtraDiskPool

	if !in.Noclone {
		emit(in, "clone", "start", "cloning image", "")
		cloned, err := m.CloneImage(cfg.DiskPath, cfg.Name, cfg.Pool)
		if err != nil {
			emit(in, "clone", "error", err.Error(), "")
			return nil, wrap("create", err)
		}
		cfg.DiskPath = cloned
		emit(in, "clone", "done", fmt.Sprintf("cloned → %s.qcow2", cfg.Name), "")
	}

	if cfg.DiskSizeGB > 0 {
		emit(in, "resize", "start", fmt.Sprintf("resizing disk to %dGB", cfg.DiskSizeGB), "")
		if err := m.ResizeVolume(cfg.DiskPath, uint64(cfg.DiskSizeGB)*1024*1024*1024); err != nil {
			emit(in, "resize", "error", err.Error(), "")
			return nil, wrap("create", err)
		}
		emit(in, "resize", "done", fmt.Sprintf("disk resized to %dGB", cfg.DiskSizeGB), "")
	}

	if cfg.ExtraDiskSizeGB > 0 {
		emit(in, "extra_disk", "start", fmt.Sprintf("creating extra disk %dGB", cfg.ExtraDiskSizeGB), "")
		pool := cfg.ExtraDiskPool
		if pool == "" {
			pool = cfg.Pool
		}
		path, err := m.CreateVolume(pool, cfg.Name+"-2.qcow2", cfg.ExtraDiskSizeGB)
		if err != nil {
			emit(in, "extra_disk", "error", err.Error(), "")
			return nil, wrap("create", err)
		}
		cfg.ExtraDisks = []vmtool.DiskDevice{{Path: path, Target: "vdb"}}
		emit(in, "extra_disk", "done", fmt.Sprintf("extra disk %s → vdb", path), "")
	}

	emit(in, "start", "start", "defining and starting", "")
	if err := m.Define(cfg); err != nil {
		emit(in, "start", "error", err.Error(), "")
		return nil, wrap("create", err)
	}
	if err := m.Start(cfg.Name); err != nil {
		emit(in, "start", "error", err.Error(), "")
		return nil, wrap("create", err)
	}
	emit(in, "start", "done", "started", "")

	emit(in, "wait_ip", "start", "waiting for IP", "")
	ip, ipErr := m.WaitForIP(cfg.Name, s.ipTimeout())
	info, infoErr := m.Info(cfg.Name)
	if infoErr != nil {
		return nil, wrap("create", infoErr)
	}
	if ipErr != nil {
		emit(in, "wait_ip", "error", ipErr.Error(), "")
		if in.DiskSizeGB > 0 || in.Playbook != "" {
			return info, internal("create", fmt.Errorf("no IP: %w", ipErr))
		}
		return info, nil
	}
	emit(in, "wait_ip", "done", "IP "+ip, "")

	auth := vmtool.ResolveAuth(ip, cfg.Name, vmtool.Auth{User: cfg.SSHUser, Password: cfg.SSHPass})
	if s.InventoryPath != "" {
		emit(in, "write_inventory", "start", "writing inventory", "")
		if err := vmtool.WriteInventory(s.InventoryPath, cfg.Name, ip, auth); err != nil {
			emit(in, "write_inventory", "error", err.Error(), "")
			return info, wrap("create", err)
		}
		emit(in, "write_inventory", "done", "inventory written", "")
	}

	if in.DiskSizeGB > 0 {
		emit(in, "grow_disk", "start", "growing filesystem", "")
		invPath, cleanup, err := vmtool.TempInventory(cfg.Name, ip, auth)
		if err != nil {
			emit(in, "grow_disk", "error", err.Error(), "")
			return info, wrap("create", err)
		}
		err = vmtool.GrowDisk(invPath)
		cleanup()
		if err != nil {
			emit(in, "grow_disk", "error", err.Error(), "")
			return info, wrap("create", err)
		}
		emit(in, "grow_disk", "done", "filesystem grown", "")
	}

	if in.Playbook != "" {
		pb, err := s.resolvePlaybook(in.Playbook)
		if err != nil {
			return info, err
		}
		emit(in, "playbook", "start", "running "+in.Playbook, "")
		invPath, cleanup, err := vmtool.TempInventory(cfg.Name, ip, auth)
		if err != nil {
			emit(in, "playbook", "error", err.Error(), "")
			return info, wrap("create", err)
		}
		out, err := vmtool.RunPlaybook(invPath, pb, withDeviceID(cfg.Name, in.ExtraVars))
		cleanup()
		if err != nil {
			emit(in, "playbook", "error", err.Error(), out)
			return info, &Error{Kind: KindInternal, Op: "playbook", Err: err, Output: out}
		}
		emit(in, "playbook", "done", in.Playbook+" done", out)

		emit(in, "reboot", "start", "rebooting", "")
		if err := m.Reboot(cfg.Name); err != nil {
			emit(in, "reboot", "error", err.Error(), "")
			return info, wrap("create", err)
		}
		emit(in, "reboot", "done", "rebooted", "")
	}

	info, err = m.Info(cfg.Name)
	if err != nil {
		return nil, wrap("create", err)
	}
	return info, nil
}
