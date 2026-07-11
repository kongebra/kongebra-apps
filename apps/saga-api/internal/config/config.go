// Package config reads saga-api configuration from environment variables.
package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port           string
	DatabaseURL    string // required, no default
	OllamaURL      string
	OllamaCloudURL string
	OllamaAPIKey   string // no default; empty = Ollama Cloud disabled
	YtdlpPath      string
	WorkDir        string // writable dir for yt-dlp temp files (emptyDir in k8s)
	ChunkTimeout   time.Duration
	// SAGACloudConcurrency caps concurrent cloud-tier jobs. Local is always 1
	// (single GPU); cloud can fan out since Ollama Cloud runs the model.
	SAGACloudConcurrency int
}

func Load() Config {
	return Config{
		Port:                 getenv("PORT", "8080"),
		DatabaseURL:          os.Getenv("DATABASE_URL"),
		OllamaURL:            getenv("OLLAMA_URL", "http://100.125.242.93:11434"),
		OllamaCloudURL:       getenv("OLLAMA_CLOUD_URL", "https://ollama.com"),
		OllamaAPIKey:         os.Getenv("OLLAMA_API_KEY"),
		YtdlpPath:            getenv("YTDLP_PATH", "yt-dlp"),
		WorkDir:              getenv("WORK_DIR", os.TempDir()),
		ChunkTimeout:         getduration("CHUNK_TIMEOUT", 15*time.Minute),
		SAGACloudConcurrency: getint("SAGA_CLOUD_CONCURRENCY", 3),
	}
}

func getint(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
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
