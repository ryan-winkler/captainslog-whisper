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
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ryan-winkler/captainslog-whisper/internal/config"
	"github.com/ryan-winkler/captainslog-whisper/internal/httputil"
	"github.com/ryan-winkler/captainslog-whisper/internal/proxy"
	"github.com/ryan-winkler/captainslog-whisper/internal/ratelimit"
	"github.com/ryan-winkler/captainslog-whisper/internal/stardate"
	localtls "github.com/ryan-winkler/captainslog-whisper/internal/tls"
	"github.com/ryan-winkler/captainslog-whisper/internal/vault"

	"gopkg.in/natefinch/lumberjack.v2"
)

const version = "0.2.0"

//go:embed all:web
var webFS embed.FS

// runtimeSettings holds settings changeable via the Preferences UI at runtime.
// Persisted to configDir/settings.json on every update.
type runtimeSettings struct {
	mu            sync.RWMutex `json:"-"` // exclude mutex from JSON serialization
	VaultDir      string `json:"vault_dir"`
	DownloadDir   string `json:"download_dir"`
	Language      string `json:"language"`
	Model         string `json:"model"`
	AutoSave      bool   `json:"auto_save"`
	AutoCopy      bool   `json:"auto_copy"`
	Prompt        string `json:"prompt"`
	VadFilter     bool   `json:"vad_filter"`
	Diarize       bool   `json:"diarize"`
	ShowStardates bool   `json:"show_stardates"`
	DateFormat    string `json:"date_format"`
	FileTitle     string `json:"file_title"`
	WhisperURL    string `json:"whisper_url"`
	LLMURL        string `json:"llm_url"`
	LLMModel      string `json:"llm_model"`
	EnableLLM     bool   `json:"enable_llm"`
	AccessLog     bool   `json:"access_log"`
	TimeFormat    string `json:"time_format"`
	HistoryLimit  int    `json:"history_limit"`
	StreamURL     string `json:"stream_url"`
	EnableTLS     bool   `json:"enable_tls"`
	DefaultExportFormat string `json:"default_export_format"`
	// Advanced transcription parameters (feature parity with faster-whisper)
	WordTimestamps          bool    `json:"word_timestamps"`
	BeamSize                int     `json:"beam_size"`
	Temperature             float64 `json:"temperature"`
	ConditionOnPreviousText *bool   `json:"condition_on_previous_text"` // pointer to distinguish false from unset
}

func main() {
	// --version / -v flag
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("captainslog %s\n", version)
		os.Exit(0)
	}

	// --- CLI flags ---
	// Priority: CLI flag > environment variable > settings.json > default
	var (
		flagPort       = flag.Int("port", 0, "Server port (default: 8090)")
		flagHost       = flag.String("host", "", "Bind address (default: 0.0.0.0)")
		flagWhisperURL = flag.String("whisper-url", "", "Whisper server URL")
		flagLLMURL     = flag.String("llm-url", "", "LLM server URL")
		flagVault      = flag.String("vault", "", "Save directory for autosave (Obsidian, Logseq, any folder)")
		flagHistoryLimit = flag.Int("history-limit", 0, "Max history entries shown (default: 5)")
		flagEnableLLM  = flag.Bool("enable-llm", false, "Enable local LLM integration")
		flagEnableTLS  = flag.Bool("enable-tls", false, "Enable auto-TLS for HTTPS")
		flagStreamURL  = flag.String("stream-url", "", "WebSocket URL for live streaming (e.g. ws://localhost:8765)")
		flagVersion    = flag.Bool("version", false, "Print version and exit")
	)
	flag.Parse()

	if *flagVersion {
		fmt.Println("captainslog", version)
		return
	}

	// --- Logger setup ---
	// All output goes to stdout so it's visible in journalctl, docker logs, etc.
	// If CAPTAINSLOG_LOG_DIR is set, also write to a log file for persistent storage.
	var logger *slog.Logger
	logFormat := envOrDefault("CAPTAINSLOG_LOG_FORMAT", "text")
	cfg := config.Load()

	// Apply CLI flag overrides
	if *flagPort > 0 { cfg.Port = *flagPort }
	if *flagHost != "" { cfg.Host = *flagHost }
	if *flagWhisperURL != "" { cfg.WhisperURL = *flagWhisperURL }
	if *flagLLMURL != "" { cfg.LLMURL = *flagLLMURL }
	if *flagVault != "" { cfg.VaultDir = *flagVault }
	if *flagEnableLLM { cfg.EnableLLM = true }
	if *flagEnableTLS { cfg.EnableTLS = true }
	if *flagStreamURL != "" { cfg.StreamURL = *flagStreamURL }

	// Build the log writer: stdout always, optionally tee to a rotating file.
	// WHY stdout? journalctl, docker logs, and most container orchestrators
	// capture stdout — stderr is for panics/crashes only.
	var logWriter io.Writer = os.Stdout
	if cfg.LogDir != "" {
		// WHY lumberjack? It handles log rotation automatically — max size,
		// retention, and compression. Without it, log files grow unbounded
		// and eventually fill the disk. lumberjack is MIT-licensed and the
		// de facto standard for Go log rotation (4k+ GitHub stars).
		rotator := &lumberjack.Logger{
			Filename:   filepath.Join(cfg.LogDir, "captainslog.log"),
			MaxSize:    100, // MB — rotate after 100MB
			MaxBackups: 3,   // keep 3 old files
			MaxAge:     28,  // days — delete files older than 28 days
			Compress:   true, // gzip old files to save disk space
		}
		// MultiWriter sends every log line to both stdout and the rotating file.
		// This ensures journalctl/docker logs always work, while also
		// persisting logs for later analysis or shipping to Grafana/Loki.
		logWriter = io.MultiWriter(os.Stdout, rotator)
	}

	if logFormat == "json" {
		// JSON format: structured logs for Grafana/Loki/ELK ingestion
		logger = slog.New(slog.NewJSONHandler(logWriter, &slog.HandlerOptions{Level: slog.LevelInfo}))
	} else {
		// Text format: human-readable for terminal/journalctl viewing
		logger = slog.New(slog.NewTextHandler(logWriter, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}

	// Validate config
	for _, u := range []string{cfg.WhisperURL, cfg.LLMURL} {
		if u != "" && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			logger.Warn("invalid URL in config — must start with http:// or https://", "url", u)
		}
	}
	if cfg.VaultDir != "" {
		cfg.VaultDir = filepath.Clean(cfg.VaultDir)
	}

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
		LLMURL:        cfg.LLMURL,
		LLMModel:      "llama3.2", // Default LLM model
		EnableLLM:     cfg.EnableLLM,
		EnableTLS:     cfg.EnableTLS,
		AccessLog:     cfg.AccessLog,
		TimeFormat:    envOrDefault("CAPTAINSLOG_TIME_FORMAT", "system"),
		HistoryLimit:  envOrIntDefault("CAPTAINSLOG_HISTORY_LIMIT", 5),
		StreamURL:     cfg.StreamURL,
	}

	// Apply CLI history-limit override
	if *flagHistoryLimit > 0 { settings.HistoryLimit = *flagHistoryLimit }

	// Load persisted settings from file (env vars override)
	if data, err := os.ReadFile(configFile); err == nil {
		// Migrate legacy field names (v0.1 → v1.0)
		var rawMap map[string]json.RawMessage
		if json.Unmarshal(data, &rawMap) == nil {
			migrations := map[string]string{
				"ollama_url":    "llm_url",
				"enable_ollama": "enable_llm",
			}
			migrated := false
			for oldKey, newKey := range migrations {
				if val, ok := rawMap[oldKey]; ok {
					if _, exists := rawMap[newKey]; !exists {
						rawMap[newKey] = val
					}
					delete(rawMap, oldKey)
					migrated = true
				}
			}
			if migrated {
				data, _ = json.Marshal(rawMap)
				logger.Info("migrated legacy settings fields", "path", configFile)
			}
		}

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
			if saved.LLMURL != "" {
				settings.LLMURL = saved.LLMURL
			}
			if saved.LLMModel != "" {
				settings.LLMModel = saved.LLMModel
			}
			if os.Getenv("CAPTAINSLOG_ENABLE_LLM") == "" {
				settings.EnableLLM = saved.EnableLLM
			}
			if os.Getenv("CAPTAINSLOG_ACCESS_LOG") == "" {
				settings.AccessLog = saved.AccessLog
			}
			if saved.HistoryLimit > 0 {
				settings.HistoryLimit = saved.HistoryLimit
			}
			if saved.TimeFormat != "" {
				settings.TimeFormat = saved.TimeFormat
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
		expected := []byte("Bearer " + cfg.AuthToken)
		return func(w http.ResponseWriter, r *http.Request) {
			token := []byte(r.Header.Get("Authorization"))
			if subtle.ConstantTimeCompare(token, expected) != 1 {
				// WHY 401? Constant-time compare failed — either the token is wrong
				// or the Authorization header is missing. We don't distinguish to
				// prevent timing-based token enumeration.
				httputil.Error(w, r, logger, http.StatusUnauthorized, "unauthorized",
					"WHY: Bearer token mismatch or missing Authorization header")
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
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self' http://127.0.0.1:* http://localhost:*; media-src 'self' blob:")
			next.ServeHTTP(w, r)
		})
	}

	// --- Structured access logging (Grafana/Loki compatible JSON) ---
	accessLog := func(next http.Handler) http.Handler {
		accessLogger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			settings.mu.RLock()
			logEnabled := settings.AccessLog
			settings.mu.RUnlock()
			if !logEnabled {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(rw, r)
			accessLogger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote", r.RemoteAddr,
				"user_agent", r.UserAgent(),
				"bytes", rw.bytes,
			)
		})
	}

	// --- Rate limiting ---
	allowIPs := strings.Split(cfg.RateAllow, ",")
	limiter := ratelimit.New(cfg.RateLimit, time.Minute, allowIPs)
	// Periodic cleanup of stale visitor entries
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			limiter.Cleanup()
		}
	}()

	// --- Recordings storage ---
	recordingsDir := filepath.Join(configDir, "recordings")
	os.MkdirAll(recordingsDir, 0755)

	// Save a recording
	mux.HandleFunc("/api/recordings", withAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			// WHY 405? Recording uploads are always POST with multipart body.
			// GET/PUT/DELETE on this endpoint are meaningless.
			httputil.Error(w, r, logger, http.StatusMethodNotAllowed, "method not allowed",
				"WHY: /api/recordings only accepts POST with multipart file upload")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 50<<20) // 50MB limit
		file, header, err := r.FormFile("file")
		if err != nil {
			// WHY 400? The multipart form must contain a 'file' field.
			// This fails when the client sends JSON instead of multipart,
			// or when the file exceeds the 50MB MaxBytesReader limit.
			httputil.Error(w, r, logger, http.StatusBadRequest, "no file provided",
				"WHY: r.FormFile('file') failed — missing multipart field or body too large")
			return
		}
		defer file.Close()

		// Generate timestamped filename
		ext := filepath.Ext(header.Filename)
		if ext == "" {
			ext = ".webm"
		}
		filename := fmt.Sprintf("%s%s", time.Now().Format("2006-01-02_15-04-05"), ext)
		destPath := filepath.Join(recordingsDir, filename)

		dest, err := os.Create(destPath)
		if err != nil {
			// WHY 500? os.Create failed — likely a permissions issue on the
			// recordings directory, or the disk is full.
			httputil.ServerError(w, r, logger, "recording save failed",
				"WHY: os.Create failed on recordings dir — check permissions and disk space", err)
			return
		}
		defer dest.Close()
		if _, err := io.Copy(dest, file); err != nil {
			// WHY 500? io.Copy failed mid-write — disk full, I/O error, or the
			// client disconnected during upload.
			httputil.ServerError(w, r, logger, "recording write failed",
				"WHY: io.Copy failed during file write — likely disk full or I/O error", err)
			return
		}

		logger.Info("recording saved", "file", filename, "size", header.Size)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"filename": filename, "status": "saved"})
	}))

	// Serve recordings for playback
	mux.Handle("/api/recordings/", http.StripPrefix("/api/recordings/", http.FileServer(http.Dir(recordingsDir))))

	// --- OpenAI-compatible API ---
	mux.HandleFunc("/v1/audio/transcriptions", withAuth(whisperProxy.Transcribe))
	mux.HandleFunc("/v1/audio/translations", withAuth(whisperProxy.Translate))

	// --- Vault save ---
	mux.HandleFunc("/api/vault/save", withAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			// WHY 405? Vault saves are write-only — POST with JSON body.
			httputil.Error(w, r, logger, http.StatusMethodNotAllowed, "method not allowed",
				"WHY: /api/vault/save only accepts POST with JSON body")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit
		var req struct {
			Text     string `json:"text"`
			Language string `json:"language"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// WHY 400? JSON decode failed — malformed JSON, wrong content-type,
			// or body exceeds the 1MB MaxBytesReader limit.
			httputil.Error(w, r, logger, http.StatusBadRequest, "invalid request body",
				"WHY: JSON decode failed — malformed body or exceeded 1MB limit")
			return
		}
		settings.mu.RLock()
		dir := settings.VaultDir
		dateFmt := settings.DateFormat
		title := settings.FileTitle
		settings.mu.RUnlock()
		saver := vault.New(dir, dateFmt, title, logger)
		if saver == nil {
			// WHY 501? vault.New returns nil when VaultDir is empty.
			// The user hasn't configured a vault directory yet.
			httputil.Error(w, r, logger, http.StatusNotImplemented,
				"vault directory not configured — set it in Preferences",
				"WHY: settings.VaultDir is empty — user must set vault path in Preferences")
			return
		}
		file, err := saver.Save(req.Text, req.Language)
		if err != nil {
			// WHY 500? vault.Save failed — directory doesn't exist, permissions
			// denied, or disk full.
			httputil.ServerError(w, r, logger, "vault save failed",
				"WHY: vault.Save failed — check vault directory exists and is writable", err)
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
			// Auth required for writes when token is configured
			if cfg.AuthToken != "" {
				expected := []byte("Bearer " + cfg.AuthToken)
				token := []byte(r.Header.Get("Authorization"))
				if subtle.ConstantTimeCompare(token, expected) != 1 {
					// WHY 401? Settings writes require auth when a token is configured.
					// Prevents unauthorized settings changes over the network.
					httputil.Error(w, r, logger, http.StatusUnauthorized, "unauthorized",
						"WHY: settings PUT requires valid Bearer token when auth is configured")
					return
				}
			}
			r.Body = http.MaxBytesReader(w, r.Body, 64<<10) // 64KB limit
			var update runtimeSettings
			if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
				// WHY 400? Settings update body must be valid JSON matching runtimeSettings.
				httputil.Error(w, r, logger, http.StatusBadRequest, "invalid request body",
					"WHY: settings JSON decode failed — malformed body or exceeded 64KB limit")
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
			if update.LLMURL != "" {
				settings.LLMURL = update.LLMURL
			}
			if update.LLMModel != "" {
				settings.LLMModel = update.LLMModel
			}
			settings.EnableLLM = update.EnableLLM
			settings.EnableTLS = update.EnableTLS
			settings.AccessLog = update.AccessLog
			if update.TimeFormat != "" {
				settings.TimeFormat = update.TimeFormat
			}
			if update.HistoryLimit > 0 {
				settings.HistoryLimit = update.HistoryLimit
			}
			if update.DefaultExportFormat != "" {
				settings.DefaultExportFormat = update.DefaultExportFormat
			}
			// Advanced transcription parameters
			settings.WordTimestamps = update.WordTimestamps
			if update.BeamSize > 0 {
				settings.BeamSize = update.BeamSize
			}
			settings.Temperature = update.Temperature
			if update.ConditionOnPreviousText != nil {
				settings.ConditionOnPreviousText = update.ConditionOnPreviousText
			}
			settings.mu.Unlock()

			// Persist to file
			go func() {
				settings.mu.RLock()
				data, err := json.MarshalIndent(settings, "", "  ")
				settings.mu.RUnlock()
				if err == nil {
					if writeErr := os.WriteFile(configFile, data, 0600); writeErr != nil {
						// WHY log only (no HTTP response)? This runs in a goroutine after
						// the HTTP response has already been sent. Settings are applied in
						// memory — persistence failure means they'll reset on restart.
						logger.Error("failed to persist settings", "error", writeErr, "why", "os.WriteFile failed — settings applied in memory but won't survive restart")
					} else {
						logger.Info("settings persisted", "path", configFile)
					}
				}
			}()

			logger.Info("settings updated", "vault_dir", settings.VaultDir, "language", settings.Language)
			json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
		default:
			// WHY 405? Settings API only supports GET (read) and PUT (update).
			httputil.Error(w, r, logger, http.StatusMethodNotAllowed, "method not allowed",
				"WHY: /api/settings only accepts GET and PUT")
		}
	})

	// --- Health ---
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		settings.mu.RLock()
		vaultDir := settings.VaultDir
		whisperURL := settings.WhisperURL
		llmURL := settings.LLMURL
		enableLLM := settings.EnableLLM
		accessLogOn := settings.AccessLog
		settings.mu.RUnlock()

		status := map[string]any{
			"status":    "ok",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"stardate":  stardate.Now(),
			"version":   version,
			"whisper":   "unknown",
			"llm":       "disabled",
			"vault":     vaultDir != "",
			"tls":       cfg.EnableTLS,
		}

		// Diagnostics (for troubleshooting)
		diag := map[string]any{
			"config_dir":   configDir,
			"settings_file": configFile,
			"whisper_url":  whisperURL,
			"llm_url":      llmURL,
			"rate_limit":   cfg.RateLimit,
			"access_log":   accessLogOn,
			"log_format":   logFormat,
		}
		if vaultDir != "" {
			if _, err := os.Stat(vaultDir); err != nil {
				diag["vault_dir"] = vaultDir + " (NOT FOUND)"
			} else {
				diag["vault_dir"] = vaultDir + " (ok)"
			}
		}
		if _, err := os.Stat(configFile); err != nil {
			diag["settings_file_exists"] = false
		} else {
			diag["settings_file_exists"] = true
		}

		if err := whisperProxy.Health(); err != nil {
			status["whisper"] = "unreachable"
			diag["whisper_error"] = err.Error()
		} else {
			status["whisper"] = "connected"
		}
		
		// LLM health check (if enabled)
		if enableLLM && llmURL != "" {
			healthClient := &http.Client{Timeout: 5 * time.Second}
			if resp, err := healthClient.Get(llmURL + "/v1/models"); err != nil {
				status["llm"] = "unreachable"
				diag["llm_error"] = err.Error()
			} else {
				resp.Body.Close()
				status["llm"] = "connected"
			}
		}

		// Include diagnostics if ?diag=true or ?verbose
		if r.URL.Query().Has("diag") || r.URL.Query().Has("verbose") {
			status["diagnostics"] = diag
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	})

	// --- Version and update check ---
	var (
		cachedLatest    string
		cachedReleaseAt time.Time
	)
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		result := map[string]any{
			"version": version,
		}
		// Check for updates via GitHub releases API (cached 1 hour)
		if time.Since(cachedReleaseAt) > time.Hour || cachedLatest == "" {
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Get("https://api.github.com/repos/ryan-winkler/captainslog-whisper/releases/latest")
			if err == nil {
				var release struct {
					TagName string `json:"tag_name"`
					HTMLURL string `json:"html_url"`
				}
				if json.NewDecoder(resp.Body).Decode(&release) == nil && release.TagName != "" {
					cachedLatest = strings.TrimPrefix(release.TagName, "v")
					cachedReleaseAt = time.Now()
				}
				resp.Body.Close()
			}
		}
		if cachedLatest != "" {
			result["latest"] = cachedLatest
			result["update_available"] = cachedLatest != version
			result["release_url"] = "https://github.com/ryan-winkler/captainslog-whisper/releases/latest"
		}
		json.NewEncoder(w).Encode(result)
	})

	// --- Model discovery (dynamic from backends) ---
	mux.HandleFunc("/api/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		result := map[string]any{
			"whisper": []map[string]string{},
		}

		// Query whisper-fastapi for available models
		settings.mu.RLock()
		whisperURL := settings.WhisperURL
		settings.mu.RUnlock()

		client := &http.Client{Timeout: 3 * time.Second}

		// whisper-fastapi exposes GET /v1/models (some versions)
		if resp, err := client.Get(whisperURL + "/v1/models"); err == nil {
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
			resp.Body.Close()
		}

		// Fallback: provide known model list if backend doesn't support /v1/models
		whisperModels, ok := result["whisper"].([]map[string]string)
		if !ok || len(whisperModels) == 0 {
			result["whisper"] = []map[string]string{
				{"id": "large-v3", "name": "large-v3 (best accuracy)"},
				{"id": "large-v2", "name": "large-v2"},
				{"id": "medium", "name": "medium (balanced)"},
				{"id": "small", "name": "small (fast)"},
				{"id": "base", "name": "base (faster)"},
				{"id": "tiny", "name": "tiny (instant)"},
			}
		}

		// Query Local LLM for available models (Ollama or LM Studio)
		if settings.EnableLLM {
			// Try standard OpenAI /v1/models first (LM Studio, modern Ollama)
			if resp, err := client.Get(settings.LLMURL + "/v1/models"); err == nil {
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
					result["llm"] = models
				}
				resp.Body.Close()
			}
			
			// Fallback: Try Ollama proprietary /api/tags if /v1/models fails or is empty
			if _, ok := result["llm"]; !ok {
				if resp, err := client.Get(settings.LLMURL + "/api/tags"); err == nil {
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
						result["llm"] = models
					}
					resp.Body.Close()
				}
			}
		}

		json.NewEncoder(w).Encode(result)
	})

	// --- Config ---
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"vault_enabled": settings.VaultDir != "",
			"llm_enabled":   settings.EnableLLM,
			"auth_required": cfg.AuthToken != "",
			"tls_enabled":   cfg.EnableTLS,
		})
	})

	// --- LLM Chat Proxy ---
	// WHY: Browser cannot call Ollama/LM Studio directly due to CORS.
	// This endpoint proxies the OpenAI-compatible chat/completions request
	// through Captain's Log so the browser never hits CORS.
	mux.HandleFunc("/api/llm/chat", withAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httputil.Error(w, r, logger, http.StatusMethodNotAllowed, "POST only", "")
			return
		}

		settings.mu.RLock()
		enabled := settings.EnableLLM
		llmURL := settings.LLMURL
		settings.mu.RUnlock()

		if !enabled || llmURL == "" {
			httputil.Error(w, r, logger, http.StatusServiceUnavailable,
				"LLM not enabled — enable in Settings → Connections", "")
			return
		}

		// Build the target URL: prefer /v1/chat/completions
		target := llmURL
		if !strings.HasSuffix(target, "/v1") {
			target += "/v1"
		}
		target += "/chat/completions"

		// Forward the request body to the LLM
		proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, r.Body)
		if err != nil {
			httputil.Error(w, r, logger, http.StatusInternalServerError, "failed to create proxy request", err.Error())
			return
		}
		proxyReq.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 120 * time.Second}
		resp, err := client.Do(proxyReq)
		if err != nil {
			httputil.Error(w, r, logger, http.StatusBadGateway,
				"LLM unreachable — is Ollama/LM Studio running?", err.Error())
			return
		}
		defer resp.Body.Close()

		// Forward the response headers and body
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}))

	// --- Open file location (system folder) ---
	mux.HandleFunc("/api/open", withAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			// WHY 405? File open requests are POST only — they trigger side effects (desktop UI interaction).
			httputil.Error(w, r, logger, http.StatusMethodNotAllowed, "method not allowed",
				"WHY: /api/open only accepts POST — triggers OS folder open side effect")
			return
		}
		var req struct {
			Path      string `json:"path"`      // Absolute or ~/ path
			Recording string `json:"recording"` // Filename of a recording
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, r, logger, http.StatusBadRequest, "invalid request body",
				"WHY: JSON decode failed")
			return
		}
		if req.Path == "" && req.Recording == "" {
			httputil.Error(w, r, logger, http.StatusBadRequest, "path or recording required",
				"WHY: JSON body must contain 'path' or 'recording'")
			return
		}

		var targetPath string
		if req.Recording != "" {
			// Safely resolve the recording within the recordings directory
			targetPath = filepath.Join(recordingsDir, req.Recording)
			// Prevent path traversal like "../../etc/passwd" in the filename
			if filepath.Dir(targetPath) != filepath.Clean(recordingsDir) {
				httputil.Error(w, r, logger, http.StatusBadRequest, "invalid recording filename",
					"WHY: path traversal attempt in recording filename")
				return
			}
		} else {
			// Expand ~/ if present
			if strings.HasPrefix(req.Path, "~/") {
				home, err := os.UserHomeDir()
				if err == nil {
					req.Path = filepath.Join(home, req.Path[2:])
				}
			}
			resolved, err := filepath.Abs(req.Path)
			if err != nil {
				httputil.Error(w, r, logger, http.StatusBadRequest, "invalid path",
					"WHY: filepath.Abs failed — path is malformed")
				return
			}
			
			// Security validation for explicit paths
			allowed := false
			settings.mu.RLock()
			vaultDir := settings.VaultDir
			settings.mu.RUnlock()
			for _, prefix := range []string{configDir, vaultDir} {
				if prefix == "" {
					continue
				}
				absPrefix, err := filepath.Abs(prefix)
				if err != nil {
					continue
				}
				if strings.HasPrefix(resolved, absPrefix) {
					allowed = true
					break
				}
			}
			if !allowed {
				httputil.Error(w, r, logger, http.StatusForbidden, "path not in allowed directories",
					"WHY: resolved path is outside configDir and vaultDir — possible path traversal")
				return
			}
			targetPath = resolved
		}

		// Open the parent directory (not the file itself)
		dir := filepath.Dir(targetPath)
		if _, err := os.Stat(dir); err != nil {
			httputil.Error(w, r, logger, http.StatusNotFound, "directory not found",
				"WHY: os.Stat failed on parent directory")
			return
		}

		// Cross-platform open command
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "windows":
			cmd = exec.Command("explorer", filepath.FromSlash(dir))
		case "darwin":
			cmd = exec.Command("open", dir)
		default: // linux, freebsd, etc
			cmd = exec.Command("xdg-open", dir)
		}
		// Start the command and Wait() in a goroutine to reap the child process.
		// Without Wait(), the child becomes a zombie and leaks OS process table entries.
		if err := cmd.Start(); err != nil {
			logger.Warn("failed to open directory", "dir", dir, "error", err)
		} else {
			go cmd.Wait()
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"opened": dir})
	}))

	// --- Static web UI ---
	webSub, err := fs.Sub(webFS, "web")
	if err != nil {
		// WHY fatal-level error? If the embedded web files can't load, the binary
		// is corrupted — there's nothing to serve. This should never happen with
		// a properly built binary.
		logger.Error("failed to load embedded web files", "error", err, "why", "binary may be corrupted — rebuild with go build")
		os.Exit(1)
	}
	mux.Handle("/", http.FileServer(http.FS(webSub)))

	// --- Start ---
	server := &http.Server{
		Addr:         cfg.ListenAddr(),
		Handler:      accessLog(limiter.Middleware(secure(mux))),
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
			// WHY fallback to HTTP? TLS cert generation can fail (disk permissions,
			// OpenSSL issues). Running without TLS is better than not starting at all —
			// the user can fix TLS later and restart.
			logger.Error("TLS setup failed, falling back to HTTP", "error", err, "why", "cert generation failed — running without TLS")
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

	// WHY stdout (not stderr)? The startup banner is informational, not an error.
	// journalctl and docker logs capture stdout by default.
	fmt.Fprintf(os.Stdout, "\n  🖖 Captain's Log v%s\n  → Stardate %s\n  → %s://%s\n  → API: %s://%s/v1/audio/transcriptions\n\n", version, sd, proto, cfg.ListenAddr(), proto, cfg.ListenAddr())

	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		var err error
		if proto == "https" {
			err = server.ListenAndServeTLS("", "")
		} else {
			err = server.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			// WHY os.Exit(1)? If the server can't bind to the port (already in use,
			// permissions), there's nothing to recover — exit so systemd can restart us.
			logger.Error("server failed", "error", err, "why", "ListenAndServe failed — port may be in use or permission denied")
			os.Exit(1)
		}
	}()

	<-stop
	logger.Info("shutting down gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		// WHY log but continue? Shutdown errors are non-fatal — the server is
		// already stopping. This can happen if active connections don't drain
		// within the 10-second timeout.
		logger.Error("shutdown error", "error", err, "why", "graceful shutdown timed out — some connections may not have drained")
	}
	logger.Info("goodbye 🖖")
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrIntDefault(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// responseWriter wraps http.ResponseWriter to capture status code and bytes for access logging.
type responseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytes += n
	return n, err
}
