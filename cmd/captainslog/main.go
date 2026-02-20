// Captain's Log — Local speech-to-text UI and OpenAI-compatible API proxy.
//
// "Captain's log, stardate 103452.7..."
//
// Captain's Log provides a beautiful browser-based recording interface backed
// by a Whisper-compatible transcription server. It supports the OpenAI
// audio transcription API format, optional Obsidian vault autosave with
// stardates, speaker diarization, and Ollama integration for post-processing.
package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ryan-winkler/captainslog-whisper/internal/config"
	"github.com/ryan-winkler/captainslog-whisper/internal/proxy"
	"github.com/ryan-winkler/captainslog-whisper/internal/stardate"
	localtls "github.com/ryan-winkler/captainslog-whisper/internal/tls"
	"github.com/ryan-winkler/captainslog-whisper/internal/vault"
)

//go:embed all:web
var webFS embed.FS

// runtimeSettings holds settings changeable via the Preferences UI at runtime.
// Persisted to configDir/settings.json on every update.
type runtimeSettings struct {
	mu          sync.RWMutex
	VaultDir    string `json:"vault_dir"`
	DownloadDir string `json:"download_dir"`
	Language    string `json:"language"`
	Model       string `json:"model"`
	AutoSave    bool   `json:"auto_save"`
	AutoCopy    bool   `json:"auto_copy"`
	Prompt      string `json:"prompt"`
	VadFilter   bool   `json:"vad_filter"`
	Diarize     bool   `json:"diarize"`
	ShowStardates bool `json:"show_stardates"`
	DateFormat  string `json:"date_format"`
	FileTitle   string `json:"file_title"`
	WhisperURL  string `json:"whisper_url"`
	OllamaURL   string `json:"ollama_url"`
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg := config.Load()

	// Config directory for persistent settings (portable via symlink/rclone)
	configDir := envOrDefault("CAPTAINSLOG_CONFIG_DIR",
		filepath.Join(os.Getenv("HOME"), ".config", "captainslog"))
	os.MkdirAll(configDir, 0755)
	configFile := filepath.Join(configDir, "settings.json")

	settings := &runtimeSettings{
		VaultDir:      cfg.VaultDir,
		DownloadDir:   envOrDefault("CAPTAINSLOG_DOWNLOAD_DIR", ""),
		Language:      envOrDefault("CAPTAINSLOG_LANGUAGE", "en"),
		Model:         envOrDefault("CAPTAINSLOG_MODEL", "large-v3"),
		AutoSave:      cfg.VaultDir != "",
		AutoCopy:      true,
		Prompt:        envOrDefault("CAPTAINSLOG_PROMPT", ""),
		VadFilter:     false,
		Diarize:       false,
		ShowStardates: true,
		DateFormat:    envOrDefault("CAPTAINSLOG_DATE_FORMAT", "2006-01-02"),
		FileTitle:     envOrDefault("CAPTAINSLOG_FILE_TITLE", "Dictation"),
		WhisperURL:    cfg.WhisperURL,
		OllamaURL:     cfg.OllamaURL,
	}

	// Load persisted settings from file (env vars override)
	if data, err := os.ReadFile(configFile); err == nil {
		var saved runtimeSettings
		if json.Unmarshal(data, &saved) == nil {
			// Only apply file values for fields not overridden by env vars
			if os.Getenv("CAPTAINSLOG_LANGUAGE") == "" && saved.Language != "" {
				settings.Language = saved.Language
			}
			if os.Getenv("CAPTAINSLOG_MODEL") == "" && saved.Model != "" {
				settings.Model = saved.Model
			}
			settings.AutoSave = saved.AutoSave
			settings.AutoCopy = saved.AutoCopy
			settings.Prompt = saved.Prompt
			settings.VadFilter = saved.VadFilter
			settings.Diarize = saved.Diarize
			settings.ShowStardates = saved.ShowStardates
			if saved.DateFormat != "" {
				settings.DateFormat = saved.DateFormat
			}
			if saved.FileTitle != "" {
				settings.FileTitle = saved.FileTitle
			}
			if saved.VaultDir != "" && os.Getenv("CAPTAINSLOG_VAULT_DIR") == "" {
				settings.VaultDir = saved.VaultDir
			}
			if saved.DownloadDir != "" {
				settings.DownloadDir = saved.DownloadDir
			}
			logger.Info("loaded settings from file", "path", configFile)
		}
	}

	whisperProxy := proxy.New(cfg.WhisperURL, logger)

	mux := http.NewServeMux()

	// --- Auth middleware ---
	withAuth := func(next http.HandlerFunc) http.HandlerFunc {
		if cfg.AuthToken == "" {
			return next
		}
		return func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get("Authorization")
			if token != "Bearer "+cfg.AuthToken {
				http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}

	// --- Security headers ---
	secure := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("Permissions-Policy", "microphone=(self)")
			next.ServeHTTP(w, r)
		})
	}

	// --- OpenAI-compatible API ---
	mux.HandleFunc("/v1/audio/transcriptions", withAuth(whisperProxy.Transcribe))
	mux.HandleFunc("/v1/audio/translations", withAuth(whisperProxy.Translate))

	// --- Vault save ---
	mux.HandleFunc("/api/vault/save", withAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Text     string `json:"text"`
			Language string `json:"language"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error": "invalid request body"}`, http.StatusBadRequest)
			return
		}
		settings.mu.RLock()
		dir := settings.VaultDir
		dateFmt := settings.DateFormat
		title := settings.FileTitle
		settings.mu.RUnlock()
		saver := vault.New(dir, dateFmt, title, logger)
		if saver == nil {
			http.Error(w, `{"error": "vault directory not configured — set it in Preferences"}`, http.StatusNotImplemented)
			return
		}
		file, err := saver.Save(req.Text, req.Language)
		if err != nil {
			http.Error(w, `{"error": "save failed"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"file": file, "status": "saved"})
	}))

	// --- Stardate API ---
	mux.HandleFunc("/api/stardate", func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"stardate":  stardate.Now(),
			"formatted": stardate.Format(now),
			"earth":     now.Format(time.RFC3339),
		})
	})

	// --- Settings API ---
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			settings.mu.RLock()
			json.NewEncoder(w).Encode(settings)
			settings.mu.RUnlock()
		case http.MethodPut:
			var update runtimeSettings
			if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
				http.Error(w, `{"error": "invalid request body"}`, http.StatusBadRequest)
				return
			}
			settings.mu.Lock()
			if update.VaultDir != "" {
				settings.VaultDir = update.VaultDir
			}
			if update.DownloadDir != "" {
				settings.DownloadDir = update.DownloadDir
			}
			if update.Language != "" {
				settings.Language = update.Language
			}
			if update.Model != "" {
				settings.Model = update.Model
			}
			settings.AutoSave = update.AutoSave
			settings.AutoCopy = update.AutoCopy
			settings.Prompt = update.Prompt
			settings.VadFilter = update.VadFilter
			settings.Diarize = update.Diarize
			settings.ShowStardates = update.ShowStardates
			if update.DateFormat != "" {
				settings.DateFormat = update.DateFormat
			}
			if update.FileTitle != "" {
				settings.FileTitle = update.FileTitle
			}
			if update.WhisperURL != "" {
				settings.WhisperURL = update.WhisperURL
				whisperProxy = proxy.New(update.WhisperURL, logger)
			}
			if update.OllamaURL != "" {
				settings.OllamaURL = update.OllamaURL
			}
			settings.mu.Unlock()

			// Persist to file
			go func() {
				settings.mu.RLock()
				data, err := json.MarshalIndent(settings, "", "  ")
				settings.mu.RUnlock()
				if err == nil {
					if writeErr := os.WriteFile(configFile, data, 0644); writeErr != nil {
						logger.Error("failed to persist settings", "error", writeErr)
					} else {
						logger.Info("settings persisted", "path", configFile)
					}
				}
			}()

			logger.Info("settings updated", "vault_dir", settings.VaultDir, "language", settings.Language)
			json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
		default:
			http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
		}
	})

	// --- Health ---
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		status := map[string]any{
			"status":    "ok",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"stardate":  stardate.Now(),
			"version":   "0.1.0",
			"whisper":   "unknown",
			"ollama":    "disabled",
			"vault":     settings.VaultDir != "",
			"tls":       cfg.EnableTLS,
		}
		if err := whisperProxy.Health(); err != nil {
			status["whisper"] = "unreachable"
		} else {
			status["whisper"] = "connected"
		}
		if cfg.EnableOllama {
			status["ollama"] = "enabled"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	})

	// --- Model discovery (dynamic from backends) ---
	mux.HandleFunc("/api/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		result := map[string]any{
			"whisper": []map[string]string{},
			"ollama":  []map[string]string{},
		}

		// Query whisper-fastapi for available models
		settings.mu.RLock()
		whisperURL := settings.WhisperURL
		ollamaURL := settings.OllamaURL
		settings.mu.RUnlock()

		client := &http.Client{Timeout: 3 * time.Second}

		// whisper-fastapi exposes GET /v1/models (some versions)
		if resp, err := client.Get(whisperURL + "/v1/models"); err == nil {
			defer resp.Body.Close()
			var data struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if json.NewDecoder(resp.Body).Decode(&data) == nil && len(data.Data) > 0 {
				models := make([]map[string]string, len(data.Data))
				for i, m := range data.Data {
					models[i] = map[string]string{"id": m.ID, "name": m.ID}
				}
				result["whisper"] = models
			}
		}

		// Fallback: provide known model list if backend doesn't support /v1/models
		if len(result["whisper"].([]map[string]string)) == 0 {
			result["whisper"] = []map[string]string{
				{"id": "large-v3", "name": "large-v3 (best accuracy)"},
				{"id": "large-v2", "name": "large-v2"},
				{"id": "medium", "name": "medium (balanced)"},
				{"id": "small", "name": "small (fast)"},
				{"id": "base", "name": "base (faster)"},
				{"id": "tiny", "name": "tiny (instant)"},
			}
		}

		// Query Ollama for available models
		if cfg.EnableOllama {
			if resp, err := client.Get(ollamaURL + "/api/tags"); err == nil {
				defer resp.Body.Close()
				var data struct {
					Models []struct {
						Name string `json:"name"`
					} `json:"models"`
				}
				if json.NewDecoder(resp.Body).Decode(&data) == nil {
					models := make([]map[string]string, len(data.Models))
					for i, m := range data.Models {
						models[i] = map[string]string{"id": m.Name, "name": m.Name}
					}
					result["ollama"] = models
				}
			}
		}

		json.NewEncoder(w).Encode(result)
	})

	// --- Config ---
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"vault_enabled":  settings.VaultDir != "",
			"ollama_enabled": cfg.EnableOllama,
			"auth_required":  cfg.AuthToken != "",
			"tls_enabled":    cfg.EnableTLS,
		})
	})

	// --- Static web UI ---
	webSub, err := fs.Sub(webFS, "web")
	if err != nil {
		logger.Error("failed to load embedded web files", "error", err)
		os.Exit(1)
	}
	mux.Handle("/", http.FileServer(http.FS(webSub)))

	// --- Start ---
	server := &http.Server{
		Addr:         cfg.ListenAddr(),
		Handler:      secure(mux),
		ReadTimeout:  120 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	proto := "http"
	if cfg.EnableTLS {
		certDir := filepath.Join(os.Getenv("HOME"), ".config", "captainslog", "tls")
		hostnames := []string{"localhost", "captainslog.local"}
		if extra := os.Getenv("CAPTAINSLOG_TLS_HOSTNAMES"); extra != "" {
			for _, h := range strings.Split(extra, ",") {
				hostnames = append(hostnames, strings.TrimSpace(h))
			}
		}
		tlsConfig, err := localtls.GenerateOrLoad(certDir, hostnames, logger)
		if err != nil {
			logger.Error("TLS setup failed, falling back to HTTP", "error", err)
		} else {
			server.TLSConfig = tlsConfig
			proto = "https"
		}
	}

	sd := stardate.Now()
	logger.Info("Captain's Log starting",
		"addr", cfg.ListenAddr(),
		"proto", proto,
		"stardate", sd,
		"whisper", cfg.WhisperURL,
		"vault", settings.VaultDir,
	)

	fmt.Fprintf(os.Stderr, "\n  🖖 Captain's Log v0.1.0\n  → Stardate %s\n  → %s://%s\n  → API: %s://%s/v1/audio/transcriptions\n\n", sd, proto, cfg.ListenAddr(), proto, cfg.ListenAddr())

	if proto == "https" {
		if err := server.ListenAndServeTLS("", ""); err != nil {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	} else {
		if err := server.ListenAndServe(); err != nil {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
