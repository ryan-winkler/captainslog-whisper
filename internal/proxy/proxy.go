// Package proxy provides an OpenAI-compatible API proxy for Whisper backends.
package proxy

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// Proxy forwards transcription requests to a Whisper-compatible backend.
type Proxy struct {
	backendURL string
	client     *http.Client
	logger     *slog.Logger
}

// New creates a new Proxy targeting the given backend URL.
func New(backendURL string, logger *slog.Logger) *Proxy {
	return &Proxy{
		backendURL: strings.TrimRight(backendURL, "/"),
		client:     &http.Client{},
		logger:     logger,
	}
}

// Transcribe handles POST /v1/audio/transcriptions
// Accepts multipart/form-data with:
//   - file: audio file (required)
//   - model: model name (ignored — backend decides)
//   - language: ISO language code (optional)
//   - response_format: json, text, srt, vtt (default: json)
//   - prompt: initial prompt (optional)
func (p *Proxy) Transcribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Limit upload size to 100MB
	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)

	// Forward the entire multipart request to the backend
	backendURL := fmt.Sprintf("%s/v1/audio/transcriptions", p.backendURL)

	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, backendURL, r.Body)
	if err != nil {
		p.logger.Error("failed to create proxy request", "error", err)
		http.Error(w, `{"error": "internal server error"}`, http.StatusInternalServerError)
		return
	}

	// Copy content-type header (important for multipart boundary)
	proxyReq.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	proxyReq.ContentLength = r.ContentLength

	resp, err := p.client.Do(proxyReq)
	if err != nil {
		p.logger.Error("backend request failed", "error", err, "url", backendURL)
		http.Error(w, `{"error": "transcription backend unavailable"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Forward response headers and body
	for k, v := range resp.Header {
		for _, val := range v {
			w.Header().Set(k, val)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)

	p.logger.Info("transcription proxied", "status", resp.StatusCode)
}

// Translate handles POST /v1/audio/translations
func (p *Proxy) Translate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)

	backendURL := fmt.Sprintf("%s/v1/audio/translations", p.backendURL)

	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, backendURL, r.Body)
	if err != nil {
		p.logger.Error("failed to create proxy request", "error", err)
		http.Error(w, `{"error": "internal server error"}`, http.StatusInternalServerError)
		return
	}

	proxyReq.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	proxyReq.ContentLength = r.ContentLength

	resp, err := p.client.Do(proxyReq)
	if err != nil {
		p.logger.Error("backend request failed", "error", err, "url", backendURL)
		http.Error(w, `{"error": "translation backend unavailable"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, v := range resp.Header {
		for _, val := range v {
			w.Header().Set(k, val)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// Health checks if the backend is reachable.
func (p *Proxy) Health() error {
	resp, err := p.client.Get(fmt.Sprintf("%s/docs", p.backendURL))
	if err != nil {
		return fmt.Errorf("backend unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("backend returned status %d", resp.StatusCode)
	}
	return nil
}
