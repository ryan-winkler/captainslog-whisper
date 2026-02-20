// Package config provides configuration management for Captain's Log.
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
	LLMURL     string // CAPTAINSLOG_LLM_URL (default: http://127.0.0.1:11434)
	StreamURL  string // CAPTAINSLOG_STREAM_URL (optional — WebSocket URL for live streaming)

	// Security
	AuthToken string // CAPTAINSLOG_AUTH_TOKEN (optional — if set, requires Bearer token)

	// Vault integration
	VaultDir string // CAPTAINSLOG_VAULT_DIR (optional — if set, autosaves transcriptions)

	// Features
	EnableLLM bool // CAPTAINSLOG_ENABLE_LLM (default: false — works with Ollama, LM Studio, etc.)
	EnableTLS bool // CAPTAINSLOG_ENABLE_TLS (default: false — auto-generates self-signed cert)

	// Observability
	AccessLog bool   // CAPTAINSLOG_ACCESS_LOG (default: false — set true for per-request JSON logs)
	LogDir    string // CAPTAINSLOG_LOG_DIR (optional — directory for log files, empty = stdout only)

	// Rate limiting
	RateLimit int    // CAPTAINSLOG_RATE_LIMIT (default: 0 — disabled, set >0 to enable for LAN/public)
	RateAllow string // CAPTAINSLOG_RATE_ALLOW (default: "127.0.0.1,::1" — comma-separated IPs/CIDRs)
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	return &Config{
		Port:         envInt("CAPTAINSLOG_PORT", 8090),
		Host:         envStr("CAPTAINSLOG_HOST", "0.0.0.0"),
		WhisperURL:   envStr("CAPTAINSLOG_WHISPER_URL", "http://127.0.0.1:5000"),
		LLMURL:       envStr("CAPTAINSLOG_LLM_URL", envStr("CAPTAINSLOG_OLLAMA_URL", "http://127.0.0.1:11434")),
		StreamURL:    envStr("CAPTAINSLOG_STREAM_URL", ""),
		AuthToken:    envStr("CAPTAINSLOG_AUTH_TOKEN", ""),
		VaultDir:     envStr("CAPTAINSLOG_VAULT_DIR", ""),
		EnableLLM:    envBool("CAPTAINSLOG_ENABLE_LLM", envBool("CAPTAINSLOG_ENABLE_OLLAMA", false)),
		EnableTLS:    envBool("CAPTAINSLOG_ENABLE_TLS", false),
		AccessLog:    envBool("CAPTAINSLOG_ACCESS_LOG", false),
		LogDir:       envStr("CAPTAINSLOG_LOG_DIR", ""),
		RateLimit:    envInt("CAPTAINSLOG_RATE_LIMIT", 0),
		RateAllow:    envStr("CAPTAINSLOG_RATE_ALLOW", "127.0.0.1,::1"),
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
