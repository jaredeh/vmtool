package server

import (
	"context"
	"os/exec"

	"github.com/jaredeh/vmtool/internal/api"
	"github.com/jaredeh/vmtool/internal/app"
	"github.com/jaredeh/vmtool/pkg/vmtool"
)

type handlers struct {
	svc    *app.Service
	listen string
	// consoleCmd, if set, replaces libvirt lookup + SSHCmd (tests).
	consoleCmd func(name string) (*exec.Cmd, error)
}

func (h *handlers) withManager(fn func(m *vmtool.Manager) (any, error)) (any, error) {
	m, err := vmtool.NewManager()
	if err != nil {
		return nil, err
	}
	defer m.Close()
	return fn(m)
}

func toAPI(info *vmtool.VMInfo) api.VMInfo {
	if info == nil {
		return api.VMInfo{}
	}
	return api.VMInfo{
		Name:      info.Name,
		State:     api.VMInfoState(info.State),
		Vcpus:     int(info.VCPUs),
		Memory:    int(info.Memory),
		Ip:        info.IP,
		Autostart: info.Autostart,
	}
}

func apiErr(err error) api.Error {
	out := api.Error{Error: err.Error()}
	var ae *app.Error
	if app.AsError(err, &ae) {
		if ae.Output != "" {
			out.Output = &ae.Output
		}
	}
	return out
}

func apiErrVM(err error, info *vmtool.VMInfo) api.Error {
	e := apiErr(err)
	if info != nil {
		v := toAPI(info)
		e.Vm = &v
	}
	return e
}

func kindOf(err error) app.Kind {
	var ae *app.Error
	if app.AsError(err, &ae) {
		return ae.Kind
	}
	if vmtool.IsNotFound(err) {
		return app.KindNotFound
	}
	return app.KindInternal
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefBool(p *bool) bool {
	return p != nil && *p
}

func derefInt(p *int) uint {
	if p == nil || *p < 0 {
		return 0
	}
	return uint(*p)
}

func (h *handlers) ListVMs(ctx context.Context, _ api.ListVMsRequestObject) (api.ListVMsResponseObject, error) {
	v, err := h.withManager(func(m *vmtool.Manager) (any, error) {
		return h.svc.ListVMs(ctx, m)
	})
	if err != nil {
		return api.ListVMs500JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse(apiErr(err))}, nil
	}
	vms := v.([]vmtool.VMInfo)
	out := make([]api.VMInfo, len(vms))
	for i := range vms {
		out[i] = toAPI(&vms[i])
	}
	return api.ListVMs200JSONResponse(out), nil
}

func (h *handlers) GetVM(ctx context.Context, req api.GetVMRequestObject) (api.GetVMResponseObject, error) {
	v, err := h.withManager(func(m *vmtool.Manager) (any, error) {
		return h.svc.GetVM(ctx, m, req.Name)
	})
	if err != nil {
		if kindOf(err) == app.KindNotFound {
			return api.GetVM404JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse(apiErr(err))}, nil
		}
		return api.GetVM500JSONResponse(apiErr(err)), nil
	}
	return api.GetVM200JSONResponse(toAPI(v.(*vmtool.VMInfo))), nil
}

func (h *handlers) CreateVM(ctx context.Context, req api.CreateVMRequestObject) (api.CreateVMResponseObject, error) {
	if req.Body == nil {
		return api.CreateVM400JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse{Error: "request body required"}}, nil
	}
	b := req.Body
	in := app.CreateVMInput{
		Name:  b.Name,
		Image: b.Image,
	}
	in.VCPUs = derefInt(b.Vcpus)
	in.Memory = derefInt(b.Memory)
	in.DiskSizeGB = derefInt(b.DiskSize)
	in.Pool = derefStr(b.Pool)
	if b.NetType != nil {
		in.NetType = string(*b.NetType)
	}
	in.NetSource = derefStr(b.NetSource)
	if b.MacvtapMode != nil {
		in.MacvtapMode = string(*b.MacvtapMode)
	}
	in.SSHUser = derefStr(b.SshUser)
	in.SSHPass = derefStr(b.SshPass)
	in.Playbook = derefStr(b.Playbook)
	in.Noclone = derefBool(b.Noclone)
	if b.ExtraDisk != nil {
		if b.ExtraDisk.Size < 1 {
			return api.CreateVM400JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse{Error: "extra_disk.size must be > 0"}}, nil
		}
		in.ExtraDiskSizeGB = uint(b.ExtraDisk.Size)
		in.ExtraDiskPool = derefStr(b.ExtraDisk.Pool)
	}
	if b.ExtraVars != nil {
		in.ExtraVars = map[string]string(*b.ExtraVars)
	}

	v, err := h.withManager(func(m *vmtool.Manager) (any, error) {
		info, e := h.svc.CreateVM(ctx, m, in)
		if e != nil {
			return info, e
		}
		return info, nil
	})
	info, _ := v.(*vmtool.VMInfo)
	if err != nil {
		switch kindOf(err) {
		case app.KindInvalid:
			return api.CreateVM400JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse(apiErr(err))}, nil
		case app.KindNotFound:
			return api.CreateVM404JSONResponse(apiErr(err)), nil
		default:
			return api.CreateVM500JSONResponse(apiErrVM(err, info)), nil
		}
	}
	return api.CreateVM201JSONResponse(toAPI(info)), nil
}

func (h *handlers) DeleteVM(ctx context.Context, req api.DeleteVMRequestObject) (api.DeleteVMResponseObject, error) {
	noclone := derefBool(req.Params.Noclone)
	_, err := h.withManager(func(m *vmtool.Manager) (any, error) {
		return nil, h.svc.DeleteVM(ctx, m, req.Name, noclone)
	})
	if err != nil {
		if kindOf(err) == app.KindNotFound {
			return api.DeleteVM404JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse(apiErr(err))}, nil
		}
		return api.DeleteVM500JSONResponse(apiErr(err)), nil
	}
	return api.DeleteVM204Response{}, nil
}

func (h *handlers) StartVM(ctx context.Context, req api.StartVMRequestObject) (api.StartVMResponseObject, error) {
	v, err := h.withManager(func(m *vmtool.Manager) (any, error) {
		return h.svc.StartVM(ctx, m, req.Name)
	})
	if err != nil {
		if kindOf(err) == app.KindNotFound {
			return api.StartVM404JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse(apiErr(err))}, nil
		}
		return api.StartVM500JSONResponse(apiErr(err)), nil
	}
	return api.StartVM200JSONResponse(toAPI(v.(*vmtool.VMInfo))), nil
}

func (h *handlers) StopVM(ctx context.Context, req api.StopVMRequestObject) (api.StopVMResponseObject, error) {
	v, err := h.withManager(func(m *vmtool.Manager) (any, error) {
		return h.svc.StopVM(ctx, m, req.Name)
	})
	if err != nil {
		if kindOf(err) == app.KindNotFound {
			return api.StopVM404JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse(apiErr(err))}, nil
		}
		return api.StopVM500JSONResponse(apiErr(err)), nil
	}
	return api.StopVM200JSONResponse(toAPI(v.(*vmtool.VMInfo))), nil
}

func (h *handlers) ResumeVM(ctx context.Context, req api.ResumeVMRequestObject) (api.ResumeVMResponseObject, error) {
	v, err := h.withManager(func(m *vmtool.Manager) (any, error) {
		return h.svc.ResumeVM(ctx, m, req.Name)
	})
	if err != nil {
		if kindOf(err) == app.KindNotFound {
			return api.ResumeVM404JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse(apiErr(err))}, nil
		}
		return api.ResumeVM500JSONResponse(apiErr(err)), nil
	}
	return api.ResumeVM200JSONResponse(toAPI(v.(*vmtool.VMInfo))), nil
}

func (h *handlers) PoweroffVM(ctx context.Context, req api.PoweroffVMRequestObject) (api.PoweroffVMResponseObject, error) {
	v, err := h.withManager(func(m *vmtool.Manager) (any, error) {
		return h.svc.PoweroffVM(ctx, m, req.Name)
	})
	if err != nil {
		if kindOf(err) == app.KindNotFound {
			return api.PoweroffVM404JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse(apiErr(err))}, nil
		}
		return api.PoweroffVM500JSONResponse(apiErr(err)), nil
	}
	return api.PoweroffVM200JSONResponse(toAPI(v.(*vmtool.VMInfo))), nil
}

func (h *handlers) RebootVM(ctx context.Context, req api.RebootVMRequestObject) (api.RebootVMResponseObject, error) {
	v, err := h.withManager(func(m *vmtool.Manager) (any, error) {
		return h.svc.RebootVM(ctx, m, req.Name)
	})
	if err != nil {
		if kindOf(err) == app.KindNotFound {
			return api.RebootVM404JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse(apiErr(err))}, nil
		}
		return api.RebootVM500JSONResponse(apiErr(err)), nil
	}
	return api.RebootVM200JSONResponse(toAPI(v.(*vmtool.VMInfo))), nil
}

func (h *handlers) SetAutostart(ctx context.Context, req api.SetAutostartRequestObject) (api.SetAutostartResponseObject, error) {
	if req.Body == nil {
		return api.SetAutostart400JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse{Error: "request body required"}}, nil
	}
	v, err := h.withManager(func(m *vmtool.Manager) (any, error) {
		return h.svc.SetAutostart(ctx, m, req.Name, req.Body.Enabled)
	})
	if err != nil {
		if kindOf(err) == app.KindNotFound {
			return api.SetAutostart404JSONResponse(apiErr(err)), nil
		}
		return api.SetAutostart500JSONResponse(apiErr(err)), nil
	}
	return api.SetAutostart200JSONResponse(toAPI(v.(*vmtool.VMInfo))), nil
}

func (h *handlers) RunVMCommand(ctx context.Context, req api.RunVMCommandRequestObject) (api.RunVMCommandResponseObject, error) {
	if req.Body == nil || len(req.Body.Command) == 0 {
		return api.RunVMCommand400JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse{Error: "command is required"}}, nil
	}
	v, err := h.withManager(func(m *vmtool.Manager) (any, error) {
		return h.svc.RunCommand(ctx, m, req.Name, req.Body.Command)
	})
	if err != nil {
		switch kindOf(err) {
		case app.KindInvalid:
			return api.RunVMCommand400JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse(apiErr(err))}, nil
		case app.KindNotFound:
			return api.RunVMCommand404JSONResponse(apiErr(err)), nil
		case app.KindConflict:
			return api.RunVMCommand409JSONResponse(apiErr(err)), nil
		case app.KindBadGateway:
			return api.RunVMCommand502JSONResponse(apiErr(err)), nil
		default:
			return api.RunVMCommand500JSONResponse(apiErr(err)), nil
		}
	}
	res := v.(*app.CmdResult)
	return api.RunVMCommand200JSONResponse{ExitCode: res.ExitCode, Stdout: res.Stdout, Stderr: res.Stderr}, nil
}

func (h *handlers) RunPlaybook(ctx context.Context, req api.RunPlaybookRequestObject) (api.RunPlaybookResponseObject, error) {
	if req.Body == nil || req.Body.Playbook == "" {
		return api.RunPlaybook400JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse{Error: "playbook is required"}}, nil
	}
	v, err := h.withManager(func(m *vmtool.Manager) (any, error) {
		var extra map[string]string
		if req.Body.ExtraVars != nil {
			extra = map[string]string(*req.Body.ExtraVars)
		}
		return h.svc.RunPlaybook(ctx, m, req.Name, req.Body.Playbook, vmtool.DefaultAuth(), extra)
	})
	res, _ := v.(*app.PlaybookResult)
	out := ""
	if res != nil {
		out = res.Output
	}
	if err != nil {
		switch kindOf(err) {
		case app.KindInvalid:
			return api.RunPlaybook400JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse(apiErr(err))}, nil
		case app.KindNotFound:
			return api.RunPlaybook404JSONResponse(apiErr(err)), nil
		case app.KindConflict:
			return api.RunPlaybook409JSONResponse(apiErr(err)), nil
		default:
			msg := err.Error()
			return api.RunPlaybook500JSONResponse{Output: out, Error: &msg}, nil
		}
	}
	return api.RunPlaybook200JSONResponse{Output: out}, nil
}

func (h *handlers) MigrateDisk(ctx context.Context, req api.MigrateDiskRequestObject) (api.MigrateDiskResponseObject, error) {
	if req.Body == nil || req.Body.Pool == "" {
		return api.MigrateDisk400JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse{Error: "pool is required"}}, nil
	}
	v, err := h.withManager(func(m *vmtool.Manager) (any, error) {
		return h.svc.MigrateDisk(ctx, m, req.Name, req.Body.Pool)
	})
	if err != nil {
		if kindOf(err) == app.KindNotFound {
			return api.MigrateDisk404JSONResponse(apiErr(err)), nil
		}
		if kindOf(err) == app.KindInvalid {
			return api.MigrateDisk400JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse(apiErr(err))}, nil
		}
		return api.MigrateDisk500JSONResponse(apiErr(err)), nil
	}
	return api.MigrateDisk200JSONResponse(toAPI(v.(*vmtool.VMInfo))), nil
}

func (h *handlers) ResizeDisk(ctx context.Context, req api.ResizeDiskRequestObject) (api.ResizeDiskResponseObject, error) {
	if req.Body == nil || req.Body.Size < 1 {
		return api.ResizeDisk400JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse{Error: "size must be > 0"}}, nil
	}
	v, err := h.withManager(func(m *vmtool.Manager) (any, error) {
		return h.svc.ResizeDisk(ctx, m, req.Name, uint(req.Body.Size), vmtool.DefaultAuth())
	})
	if err != nil {
		if kindOf(err) == app.KindNotFound {
			return api.ResizeDisk404JSONResponse(apiErr(err)), nil
		}
		if kindOf(err) == app.KindInvalid {
			return api.ResizeDisk400JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse(apiErr(err))}, nil
		}
		return api.ResizeDisk500JSONResponse(apiErr(err)), nil
	}
	res := v.(*app.ResizeResult)
	out := api.ResizeDiskResult{Vm: toAPI(&res.VM), FilesystemGrown: res.FilesystemGrown}
	if res.GrowError != "" {
		out.GrowError = &res.GrowError
	}
	return api.ResizeDisk200JSONResponse(out), nil
}

func (h *handlers) AddDisk(ctx context.Context, req api.AddDiskRequestObject) (api.AddDiskResponseObject, error) {
	if req.Body == nil || req.Body.Size < 1 {
		return api.AddDisk400JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse{Error: "size must be > 0"}}, nil
	}
	v, err := h.withManager(func(m *vmtool.Manager) (any, error) {
		return h.svc.AddDisk(ctx, m, req.Name, uint(req.Body.Size), derefStr(req.Body.Pool))
	})
	if err != nil {
		if kindOf(err) == app.KindNotFound {
			return api.AddDisk404JSONResponse(apiErr(err)), nil
		}
		if kindOf(err) == app.KindInvalid {
			return api.AddDisk400JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse(apiErr(err))}, nil
		}
		return api.AddDisk500JSONResponse(apiErr(err)), nil
	}
	res := v.(*app.AddDiskResult)
	return api.AddDisk200JSONResponse{Vm: toAPI(&res.VM), Path: res.Path, Target: res.Target}, nil
}

func (h *handlers) ListNetworks(ctx context.Context, _ api.ListNetworksRequestObject) (api.ListNetworksResponseObject, error) {
	v, err := h.withManager(func(m *vmtool.Manager) (any, error) {
		return h.svc.ListNetworks(ctx, m)
	})
	if err != nil {
		return api.ListNetworks500JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse(apiErr(err))}, nil
	}
	return api.ListNetworks200JSONResponse(v.([]string)), nil
}

func (h *handlers) ListBridges(ctx context.Context, _ api.ListBridgesRequestObject) (api.ListBridgesResponseObject, error) {
	v, err := h.withManager(func(m *vmtool.Manager) (any, error) {
		return h.svc.ListBridges(ctx, m)
	})
	if err != nil {
		return api.ListBridges500JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse(apiErr(err))}, nil
	}
	return api.ListBridges200JSONResponse(v.([]string)), nil
}

func (h *handlers) ListImages(ctx context.Context, _ api.ListImagesRequestObject) (api.ListImagesResponseObject, error) {
	v, err := h.withManager(func(m *vmtool.Manager) (any, error) {
		return h.svc.ListImages(ctx, m)
	})
	if err != nil {
		return api.ListImages500JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse(apiErr(err))}, nil
	}
	return api.ListImages200JSONResponse(v.(map[string][]string)), nil
}

func (h *handlers) DeleteImage(ctx context.Context, req api.DeleteImageRequestObject) (api.DeleteImageResponseObject, error) {
	pool := "default"
	if req.Params.Pool != nil && *req.Params.Pool != "" {
		pool = *req.Params.Pool
	}
	_, err := h.withManager(func(m *vmtool.Manager) (any, error) {
		return nil, h.svc.DeleteImage(ctx, m, req.Name, pool)
	})
	if err != nil {
		if kindOf(err) == app.KindNotFound {
			return api.DeleteImage404JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse(apiErr(err))}, nil
		}
		return api.DeleteImage500JSONResponse(apiErr(err)), nil
	}
	return api.DeleteImage204Response{}, nil
}

func (h *handlers) ListPools(ctx context.Context, _ api.ListPoolsRequestObject) (api.ListPoolsResponseObject, error) {
	v, err := h.withManager(func(m *vmtool.Manager) (any, error) {
		return h.svc.ListPools(ctx, m)
	})
	if err != nil {
		return api.ListPools500JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse(apiErr(err))}, nil
	}
	pools := v.([]vmtool.PoolInfo)
	out := make([]api.PoolInfo, len(pools))
	for i, p := range pools {
		out[i] = api.PoolInfo{Name: p.Name, Path: p.Path, Active: p.Active}
	}
	return api.ListPools200JSONResponse(out), nil
}

func (h *handlers) CreatePool(ctx context.Context, req api.CreatePoolRequestObject) (api.CreatePoolResponseObject, error) {
	if req.Body == nil || req.Body.Name == "" || req.Body.Path == "" {
		return api.CreatePool400JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse{Error: "name and path are required"}}, nil
	}
	_, err := h.withManager(func(m *vmtool.Manager) (any, error) {
		return nil, h.svc.CreatePool(ctx, m, req.Body.Name, req.Body.Path)
	})
	if err != nil {
		if kindOf(err) == app.KindInvalid {
			return api.CreatePool400JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse(apiErr(err))}, nil
		}
		return api.CreatePool500JSONResponse(apiErr(err)), nil
	}
	return api.CreatePool201JSONResponse{Name: req.Body.Name, Path: req.Body.Path}, nil
}

func (h *handlers) ListPlaybooks(_ context.Context, _ api.ListPlaybooksRequestObject) (api.ListPlaybooksResponseObject, error) {
	pbs, err := h.svc.ListPlaybooks()
	if err != nil {
		return api.ListPlaybooks500JSONResponse{ErrorJSONResponse: api.ErrorJSONResponse(apiErr(err))}, nil
	}
	return api.ListPlaybooks200JSONResponse(pbs), nil
}
