package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("PORT", "")
	t.Setenv("OLLAMA_URL", "")
	cfg := Load()
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want 8080", cfg.Port)
	}
	if cfg.OllamaURL != "http://100.125.242.93:11434" {
		t.Errorf("OllamaURL = %q", cfg.OllamaURL)
	}
	if cfg.ChunkTimeout != 15*time.Minute {
		t.Errorf("ChunkTimeout = %v", cfg.ChunkTimeout)
	}
	if cfg.YtdlpPath != "yt-dlp" {
		t.Errorf("YtdlpPath = %q", cfg.YtdlpPath)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("PORT", "9999")
	t.Setenv("CHUNK_TIMEOUT", "5m")
	cfg := Load()
	if cfg.Port != "9999" || cfg.ChunkTimeout != 5*time.Minute {
		t.Errorf("overrides not applied: %+v", cfg)
	}
}
