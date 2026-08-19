package app

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"

	"github.com/jaredeh/vmtool/pkg/vmtool"
)

func (s *Service) RunCommand(_ context.Context, m Manager, name string, argv []string) (*CmdResult, error) {
	if len(argv) == 0 {
		return nil, invalid("cmd", fmt.Errorf("command is required"))
	}
	info, err := m.Info(name)
	if err != nil {
		return nil, wrap("cmd", err)
	}
	if info.IP == "" {
		return nil, conflict("cmd", fmt.Errorf("VM %q has no IP address", name))
	}
	c, err := vmtool.RemoteCmd(info.IP, info.Name, argv)
	if err != nil {
		return nil, wrap("cmd", err)
	}
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	c.Stdin = nil
	err = c.Run()
	res := &CmdResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err == nil {
		return res, nil
	}
	if e, ok := err.(*exec.ExitError); ok {
		res.ExitCode = e.ExitCode()
		return res, nil
	}
	return res, badGateway("cmd", err)
}

func (s *Service) RunPlaybook(_ context.Context, m Manager, name, playbook string, auth vmtool.Auth, extraVars map[string]string) (*PlaybookResult, error) {
	pb, err := s.resolvePlaybook(playbook)
	if err != nil {
		return nil, err
	}
	info, err := m.Info(name)
	if err != nil {
		return nil, wrap("playbook", err)
	}
	if info.IP == "" {
		return nil, conflict("playbook", fmt.Errorf("VM %q has no IP address", name))
	}
	resolved := vmtool.ResolveAuth(info.IP, name, auth)
	invPath, cleanup, err := vmtool.TempInventory(name, info.IP, resolved)
	if err != nil {
		return nil, wrap("playbook", err)
	}
	defer cleanup()
	out, err := vmtool.RunPlaybook(invPath, pb, withDeviceID(name, extraVars))
	res := &PlaybookResult{Output: out}
	if err != nil {
		return res, &Error{Kind: KindInternal, Op: "playbook", Err: err, Output: out}
	}
	return res, nil
}
