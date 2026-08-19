package app

import (
	"path/filepath"
	"testing"
)

func TestResolvePlaybook(t *testing.T) {
	s := &Service{PlaybookDir: "/repo/ansible/playbooks"}

	got, err := s.resolvePlaybook("setup_bot.yml")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join("/repo/ansible/playbooks", "setup_bot.yml") {
		t.Fatalf("bare: %s", got)
	}

	got, err = s.resolvePlaybook("ansible/playbooks/setup_bot.yml")
	if err != nil {
		t.Fatal(err)
	}
	if got != "ansible/playbooks/setup_bot.yml" {
		t.Fatalf("relative with slash: %s", got)
	}

	if _, err := s.resolvePlaybook("../etc/passwd.yml"); err == nil {
		t.Fatal("expected .. reject")
	}
	if _, err := s.resolvePlaybook("foo..yml"); err != nil {
		t.Fatalf("foo..yml should be allowed: %v", err)
	}
	if _, err := s.resolvePlaybook(""); err == nil {
		t.Fatal("empty should fail")
	}
	if _, err := s.resolvePlaybook("noext"); err == nil {
		t.Fatal("extension required")
	}
}

func TestWithDeviceID(t *testing.T) {
	got := withDeviceID("web", nil)
	if got["deviceid"] != "web" {
		t.Fatalf("default: %v", got)
	}
	got = withDeviceID("web", map[string]string{"deviceid": "lab", "foo": "bar"})
	if got["deviceid"] != "lab" || got["foo"] != "bar" {
		t.Fatalf("override: %v", got)
	}
	got = withDeviceID("web", map[string]string{"foo": "bar"})
	if got["deviceid"] != "web" || got["foo"] != "bar" {
		t.Fatalf("merge: %v", got)
	}
}
