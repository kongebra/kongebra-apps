package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("PORT", "")
	t.Setenv("LITELLM_URL", "")
	t.Setenv("LITELLM_API_KEY", "")
	cfg := Load()
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want 8080", cfg.Port)
	}
	if cfg.LiteLLMURL != "http://litellm.litellm.svc:4000/v1" {
		t.Errorf("LiteLLMURL = %q", cfg.LiteLLMURL)
	}
	if cfg.ChunkTimeout != 15*time.Minute {
		t.Errorf("ChunkTimeout = %v", cfg.ChunkTimeout)
	}
	if cfg.YtdlpPath != "yt-dlp" {
		t.Errorf("YtdlpPath = %q", cfg.YtdlpPath)
	}
	if cfg.LiteLLMAPIKey != "" {
		t.Errorf("LiteLLMAPIKey = %q, want empty by default", cfg.LiteLLMAPIKey)
	}
}

func TestLoadLiteLLMOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("LITELLM_URL", "http://example.test/v1")
	t.Setenv("LITELLM_API_KEY", "sk-123")
	cfg := Load()
	if cfg.LiteLLMURL != "http://example.test/v1" {
		t.Errorf("LiteLLMURL = %q", cfg.LiteLLMURL)
	}
	if cfg.LiteLLMAPIKey != "sk-123" {
		t.Errorf("LiteLLMAPIKey = %q", cfg.LiteLLMAPIKey)
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
