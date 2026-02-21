// Package watcher monitors a directory for new audio files and auto-transcribes them.
//
// When a new audio file (wav, mp3, mp4, m4a, ogg, flac, webm) is detected,
// it is sent to the configured Whisper backend for transcription. The result
// is saved to the vault directory and broadcast to connected SSE clients.
//
// Inspired by Scriberr's folder watcher feature.
package watcher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// audioExtensions are the file types we auto-transcribe.
var audioExtensions = map[string]bool{
	".wav":  true,
	".mp3":  true,
	".mp4":  true,
	".m4a":  true,
	".ogg":  true,
	".flac": true,
	".webm": true,
	".opus": true,
	".wma":  true,
}

// Event represents a watcher event sent to SSE clients.
type Event struct {
	Type      string `json:"type"`      // "transcription", "error", "started"
	Filename  string `json:"filename"`
	Text      string `json:"text,omitempty"`
	Error     string `json:"error,omitempty"`
	Timestamp string `json:"timestamp"`
}

// Watcher monitors a directory for new audio files.
type Watcher struct {
	dir        string
	whisperURL string
	vaultDir   string
	language   string
	logger     *slog.Logger
	client     *http.Client

	// SSE clients
	mu       sync.Mutex
	clients  map[chan Event]struct{}
	stopCh   chan struct{}
	fsw      *fsnotify.Watcher

	// Track files we've already processed (avoid duplicates)
	processed map[string]bool
}

// New creates a Watcher for the given directory.
func New(dir, whisperURL, vaultDir, language string, logger *slog.Logger) *Watcher {
	return &Watcher{
		dir:        dir,
		whisperURL: strings.TrimRight(whisperURL, "/"),
		vaultDir:   vaultDir,
		language:   language,
		logger:     logger,
		client:     &http.Client{Timeout: 600 * time.Second}, // Long timeout for transcription
		clients:    make(map[chan Event]struct{}),
		stopCh:     make(chan struct{}),
		processed:  make(map[string]bool),
	}
}

// Start begins watching the directory. Call Stop() to clean up.
func (w *Watcher) Start() error {
	if w.dir == "" {
		return fmt.Errorf("watch directory is empty")
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(w.dir, 0755); err != nil {
		return fmt.Errorf("create watch dir: %w", err)
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create fsnotify watcher: %w", err)
	}
	w.fsw = fsw

	if err := fsw.Add(w.dir); err != nil {
		fsw.Close()
		return fmt.Errorf("watch dir %s: %w", w.dir, err)
	}

	w.logger.Info("folder watcher started", "dir", w.dir)
	w.broadcast(Event{Type: "started", Timestamp: time.Now().Format(time.RFC3339)})

	go w.loop()
	return nil
}

// Stop shuts down the watcher.
func (w *Watcher) Stop() {
	close(w.stopCh)
	if w.fsw != nil {
		w.fsw.Close()
	}
}

// Subscribe returns a channel that receives watcher events.
func (w *Watcher) Subscribe() chan Event {
	ch := make(chan Event, 16)
	w.mu.Lock()
	w.clients[ch] = struct{}{}
	w.mu.Unlock()
	return ch
}

// Unsubscribe removes an SSE client.
func (w *Watcher) Unsubscribe(ch chan Event) {
	w.mu.Lock()
	delete(w.clients, ch)
	w.mu.Unlock()
	close(ch)
}

func (w *Watcher) broadcast(ev Event) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for ch := range w.clients {
		select {
		case ch <- ev:
		default:
			// Client buffer full — skip rather than block
		}
	}
}

func (w *Watcher) loop() {
	// Debounce: wait for file to be fully written before processing
	pending := make(map[string]time.Time)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return

		case event, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			// Only handle Create and Write events
			if event.Op&(fsnotify.Create|fsnotify.Write) == 0 {
				continue
			}
			ext := strings.ToLower(filepath.Ext(event.Name))
			if !audioExtensions[ext] {
				continue
			}
			// Debounce: update the pending timestamp
			pending[event.Name] = time.Now()

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			w.logger.Error("watcher error", "error", err)

		case <-ticker.C:
			// Process files that have been stable for 3+ seconds
			now := time.Now()
			for path, lastSeen := range pending {
				if now.Sub(lastSeen) < 3*time.Second {
					continue // Still being written
				}
				delete(pending, path)

				if w.processed[path] {
					continue
				}
				w.processed[path] = true

				go w.processFile(path)
			}
		}
	}
}

func (w *Watcher) processFile(path string) {
	filename := filepath.Base(path)
	w.logger.Info("auto-transcribing", "file", filename)

	w.broadcast(Event{
		Type:      "processing",
		Filename:  filename,
		Timestamp: time.Now().Format(time.RFC3339),
	})

	text, err := w.transcribe(path)
	if err != nil {
		w.logger.Error("transcription failed", "file", filename, "error", err)
		w.broadcast(Event{
			Type:      "error",
			Filename:  filename,
			Error:     err.Error(),
			Timestamp: time.Now().Format(time.RFC3339),
		})
		return
	}

	w.logger.Info("transcription complete", "file", filename, "chars", len(text))

	// Save to vault if configured
	if w.vaultDir != "" && text != "" {
		vaultPath := filepath.Join(w.vaultDir, strings.TrimSuffix(filename, filepath.Ext(filename))+".md")
		content := fmt.Sprintf("---\ntitle: %s\ndate: %s\ntags: [auto-transcription, folder-watch]\n---\n\n%s\n",
			strings.TrimSuffix(filename, filepath.Ext(filename)),
			time.Now().Format(time.RFC3339),
			text,
		)
		if err := os.WriteFile(vaultPath, []byte(content), 0644); err != nil {
			w.logger.Error("vault save failed", "file", vaultPath, "error", err)
		} else {
			w.logger.Info("saved to vault", "file", vaultPath)
		}
	}

	w.broadcast(Event{
		Type:      "transcription",
		Filename:  filename,
		Text:      text,
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

func (w *Watcher) transcribe(audioPath string) (string, error) {
	// Read audio file
	audioData, err := os.ReadFile(audioPath)
	if err != nil {
		return "", fmt.Errorf("read audio: %w", err)
	}

	// Build multipart form request (same as browser upload)
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(part, bytes.NewReader(audioData)); err != nil {
		return "", fmt.Errorf("copy audio data: %w", err)
	}

	writer.WriteField("response_format", "json")
	if w.language != "" && w.language != "und" {
		writer.WriteField("language", w.language)
	}
	writer.Close()

	// Send to Whisper backend
	url := w.whisperURL + "/v1/audio/transcriptions"
	req, err := http.NewRequest(http.MethodPost, url, &buf)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := w.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("whisper request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("whisper returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	return strings.TrimSpace(result.Text), nil
}

// SSEHandler returns an HTTP handler for Server-Sent Events.
func (w *Watcher) SSEHandler() http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		flusher, ok := rw.(http.Flusher)
		if !ok {
			http.Error(rw, "streaming not supported", http.StatusInternalServerError)
			return
		}

		rw.Header().Set("Content-Type", "text/event-stream")
		rw.Header().Set("Cache-Control", "no-cache")
		rw.Header().Set("Connection", "keep-alive")

		ch := w.Subscribe()
		defer w.Unsubscribe(ch)

		// Send initial connected event
		fmt.Fprintf(rw, "data: {\"type\":\"connected\"}\n\n")
		flusher.Flush()

		for {
			select {
			case ev, ok := <-ch:
				if !ok {
					return
				}
				data, _ := json.Marshal(ev)
				fmt.Fprintf(rw, "data: %s\n\n", data)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	}
}
