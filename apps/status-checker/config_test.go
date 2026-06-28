package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "targets.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadConfigValid(t *testing.T) {
	p := writeTemp(t, `
targets:
  - name: go-hello-world
    url: https://go-hello-world.newb.no
    health_path: /health
`)
	cfg, err := loadConfig(p)
	if err != nil {
		t.Fatalf("uventet feil: %v", err)
	}
	if len(cfg.Targets) != 1 || cfg.Targets[0].Name != "go-hello-world" {
		t.Fatalf("feil parse: %+v", cfg.Targets)
	}
	if cfg.Targets[0].HealthPath != "/health" {
		t.Errorf("health_path = %q", cfg.Targets[0].HealthPath)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	if _, err := loadConfig("/nope/targets.yaml"); err == nil {
		t.Fatal("forventet feil for manglende fil")
	}
}

func TestLoadConfigEmptyTargets(t *testing.T) {
	p := writeTemp(t, "targets: []\n")
	cfg, err := loadConfig(p)
	if err != nil {
		t.Fatalf("tom liste skal ikke feile: %v", err)
	}
	if len(cfg.Targets) != 0 {
		t.Errorf("forventet 0 targets, fikk %d", len(cfg.Targets))
	}
}
