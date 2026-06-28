package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Target er en tjeneste checker poller (kun domene-URL, aldri in-cluster service-DNS).
type Target struct {
	Name       string `yaml:"name"`
	URL        string `yaml:"url"`
	HealthPath string `yaml:"health_path"`
}

type Config struct {
	Targets []Target `yaml:"targets"`
}

// loadConfig leser og parser targets.yaml (montert ConfigMap). Fail-fast ved feil.
func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("les config %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}
	return &cfg, nil
}
