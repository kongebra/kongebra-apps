// Package config reads saga-api configuration from environment variables.
package config

import (
	"os"
	"time"
)

type Config struct {
	Port         string
	DatabaseURL  string // required, no default
	OllamaURL    string
	YtdlpPath    string
	WorkDir      string // writable dir for yt-dlp temp files (emptyDir in k8s)
	ChunkTimeout time.Duration
}

func Load() Config {
	return Config{
		Port:         getenv("PORT", "8080"),
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		OllamaURL:    getenv("OLLAMA_URL", "http://100.125.242.93:11434"),
		YtdlpPath:    getenv("YTDLP_PATH", "yt-dlp"),
		WorkDir:      getenv("WORK_DIR", os.TempDir()),
		ChunkTimeout: getduration("CHUNK_TIMEOUT", 15*time.Minute),
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getduration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
