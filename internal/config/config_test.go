package config

import (
	"os"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	// Ensure no env vars interfere
	for _, key := range []string{
		"CAPTAINSLOG_PORT", "CAPTAINSLOG_HOST", "CAPTAINSLOG_WHISPER_URL",
		"CAPTAINSLOG_LLM_URL", "CAPTAINSLOG_OLLAMA_URL", "CAPTAINSLOG_AUTH_TOKEN",
		"CAPTAINSLOG_VAULT_DIR", "CAPTAINSLOG_ENABLE_LLM", "CAPTAINSLOG_ENABLE_OLLAMA",
		"CAPTAINSLOG_ENABLE_TLS",
	} {
		os.Unsetenv(key)
	}

	cfg := Load()

	if cfg.Port != 8090 {
		t.Errorf("Port = %d, want 8090", cfg.Port)
	}
	if cfg.Host != "0.0.0.0" {
		t.Errorf("Host = %q, want %q", cfg.Host, "0.0.0.0")
	}
	if cfg.WhisperURL != "http://127.0.0.1:5000" {
		t.Errorf("WhisperURL = %q, want default", cfg.WhisperURL)
	}
	if cfg.LLMURL != "http://127.0.0.1:11434" {
		t.Errorf("LLMURL = %q, want default", cfg.LLMURL)
	}
	if cfg.AuthToken != "" {
		t.Errorf("AuthToken = %q, want empty", cfg.AuthToken)
	}
	if cfg.VaultDir != "" {
		t.Errorf("VaultDir = %q, want empty", cfg.VaultDir)
	}
	if cfg.EnableLLM {
		t.Error("EnableLLM should be false by default")
	}
	if cfg.EnableTLS {
		t.Error("EnableTLS should be false by default")
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("CAPTAINSLOG_PORT", "9999")
	t.Setenv("CAPTAINSLOG_HOST", "127.0.0.1")
	t.Setenv("CAPTAINSLOG_WHISPER_URL", "http://whisper:5000")
	t.Setenv("CAPTAINSLOG_AUTH_TOKEN", "secret123")
	t.Setenv("CAPTAINSLOG_VAULT_DIR", "/tmp/vault")
	t.Setenv("CAPTAINSLOG_ENABLE_LLM", "true")
	t.Setenv("CAPTAINSLOG_ENABLE_TLS", "true")

	cfg := Load()

	if cfg.Port != 9999 {
		t.Errorf("Port = %d, want 9999", cfg.Port)
	}
	if cfg.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want 127.0.0.1", cfg.Host)
	}
	if cfg.WhisperURL != "http://whisper:5000" {
		t.Errorf("WhisperURL = %q", cfg.WhisperURL)
	}
	if cfg.AuthToken != "secret123" {
		t.Errorf("AuthToken = %q", cfg.AuthToken)
	}
	if cfg.VaultDir != "/tmp/vault" {
		t.Errorf("VaultDir = %q", cfg.VaultDir)
	}
	if !cfg.EnableLLM {
		t.Error("EnableLLM should be true")
	}
	if !cfg.EnableTLS {
		t.Error("EnableTLS should be true")
	}
}

// Test backward compatibility: CAPTAINSLOG_OLLAMA_URL still works
func TestLoadLegacyOllamaEnv(t *testing.T) {
	t.Setenv("CAPTAINSLOG_OLLAMA_URL", "http://custom-ollama:11434")
	t.Setenv("CAPTAINSLOG_ENABLE_OLLAMA", "true")
	// Clear the new env vars to ensure fallback works
	os.Unsetenv("CAPTAINSLOG_LLM_URL")
	os.Unsetenv("CAPTAINSLOG_ENABLE_LLM")

	cfg := Load()

	if cfg.LLMURL != "http://custom-ollama:11434" {
		t.Errorf("LLMURL = %q, want legacy OLLAMA_URL value", cfg.LLMURL)
	}
	if !cfg.EnableLLM {
		t.Error("EnableLLM should be true via legacy ENABLE_OLLAMA")
	}
}

func TestListenAddr(t *testing.T) {
	cfg := &Config{Host: "0.0.0.0", Port: 8090}
	if addr := cfg.ListenAddr(); addr != "0.0.0.0:8090" {
		t.Errorf("ListenAddr() = %q, want 0.0.0.0:8090", addr)
	}
}

func TestEnvIntInvalid(t *testing.T) {
	t.Setenv("CAPTAINSLOG_PORT", "not-a-number")
	cfg := Load()
	if cfg.Port != 8090 {
		t.Errorf("Port = %d, want fallback 8090 on invalid input", cfg.Port)
	}
}

func TestEnvBoolInvalid(t *testing.T) {
	t.Setenv("CAPTAINSLOG_ENABLE_LLM", "not-a-bool")
	cfg := Load()
	if cfg.EnableLLM {
		t.Error("EnableLLM should fallback to false on invalid input")
	}
}
