package app

import (
	"context"
	"fmt"

	"github.com/jaredeh/vmtool/pkg/vmtool"
)

func (s *Service) ListNetworks(_ context.Context, m Manager) ([]string, error) {
	nets, err := m.ListNetworks()
	if err != nil {
		return nil, wrap("list_networks", err)
	}
	if nets == nil {
		nets = []string{}
	}
	return nets, nil
}

func (s *Service) ListBridges(_ context.Context, m Manager) ([]string, error) {
	b, err := m.ListBridges()
	if err != nil {
		return nil, wrap("list_bridges", err)
	}
	if b == nil {
		b = []string{}
	}
	return b, nil
}

func (s *Service) ListImages(_ context.Context, m Manager) (map[string][]string, error) {
	imgs, err := m.ListImagesByPool()
	if err != nil {
		return nil, wrap("list_images", err)
	}
	if imgs == nil {
		imgs = map[string][]string{}
	}
	return imgs, nil
}

func (s *Service) DeleteImage(_ context.Context, m Manager, name, pool string) error {
	if pool == "" {
		pool = "default"
	}
	return wrap("delete_image", m.DeleteImage(name, pool))
}

func (s *Service) ListPools(_ context.Context, m Manager) ([]vmtool.PoolInfo, error) {
	pools, err := m.ListPools()
	if err != nil {
		return nil, wrap("list_pools", err)
	}
	if pools == nil {
		pools = []vmtool.PoolInfo{}
	}
	return pools, nil
}

func (s *Service) CreatePool(_ context.Context, m Manager, name, path string) error {
	if name == "" || path == "" {
		return invalid("create_pool", fmt.Errorf("name and path are required"))
	}
	return wrap("create_pool", m.CreatePool(name, path))
}

func (s *Service) ListPlaybooks() ([]string, error) {
	pbs, err := vmtool.ListPlaybooks(s.playbookDir())
	if err != nil {
		return nil, wrap("list_playbooks", err)
	}
	if pbs == nil {
		pbs = []string{}
	}
	return pbs, nil
}
