package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("PORT", "")
	t.Setenv("OLLAMA_URL", "")
	t.Setenv("OLLAMA_CLOUD_URL", "")
	t.Setenv("OLLAMA_API_KEY", "")
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
	if cfg.OllamaCloudURL != "https://ollama.com" {
		t.Errorf("OllamaCloudURL = %q", cfg.OllamaCloudURL)
	}
	if cfg.OllamaAPIKey != "" {
		t.Errorf("OllamaAPIKey = %q, want empty (cloud disabled by default)", cfg.OllamaAPIKey)
	}
}

func TestLoadCloudOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("OLLAMA_CLOUD_URL", "https://example.test")
	t.Setenv("OLLAMA_API_KEY", "sk-123")
	cfg := Load()
	if cfg.OllamaCloudURL != "https://example.test" {
		t.Errorf("OllamaCloudURL = %q", cfg.OllamaCloudURL)
	}
	if cfg.OllamaAPIKey != "sk-123" {
		t.Errorf("OllamaAPIKey = %q", cfg.OllamaAPIKey)
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
