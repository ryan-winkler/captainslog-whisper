// Package config provides configuration management for VoxScribe.
// All configuration is via environment variables — no config files with secrets.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds the application configuration.
type Config struct {
	// Server
	Port    int    // CAPTAINSLOG_PORT (default: 8090)
	Host    string // CAPTAINSLOG_HOST (default: 0.0.0.0)

	// Backend
	WhisperURL string // CAPTAINSLOG_WHISPER_URL (default: http://127.0.0.1:5000)
	OllamaURL  string // CAPTAINSLOG_OLLAMA_URL (default: http://127.0.0.1:11434)

	// Security
	AuthToken string // CAPTAINSLOG_AUTH_TOKEN (optional — if set, requires Bearer token)

	// Vault integration
	VaultDir string // CAPTAINSLOG_VAULT_DIR (optional — if set, autosaves transcriptions)

	// Features
	EnableOllama bool // CAPTAINSLOG_ENABLE_OLLAMA (default: false)
	EnableTLS    bool // CAPTAINSLOG_ENABLE_TLS (default: false — auto-generates self-signed cert)
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	return &Config{
		Port:         envInt("CAPTAINSLOG_PORT", 8090),
		Host:         envStr("CAPTAINSLOG_HOST", "0.0.0.0"),
		WhisperURL:   envStr("CAPTAINSLOG_WHISPER_URL", "http://127.0.0.1:5000"),
		OllamaURL:    envStr("CAPTAINSLOG_OLLAMA_URL", "http://127.0.0.1:11434"),
		AuthToken:    envStr("CAPTAINSLOG_AUTH_TOKEN", ""),
		VaultDir:     envStr("CAPTAINSLOG_VAULT_DIR", ""),
		EnableOllama: envBool("CAPTAINSLOG_ENABLE_OLLAMA", false),
		EnableTLS:    envBool("CAPTAINSLOG_ENABLE_TLS", false),
	}
}

// ListenAddr returns the formatted listen address.
func (c *Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}
