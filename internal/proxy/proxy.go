// Package proxy provides an OpenAI-compatible API proxy for Whisper backends.
package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// Proxy forwards transcription requests to a Whisper-compatible backend.
type Proxy struct {
	backendURL   string
	client       *http.Client // Long timeout for audio transcription (120s)
	healthClient *http.Client // Short timeout for health checks (5s)
	logger       *slog.Logger
}

// New creates a new Proxy targeting the given backend URL.
func New(backendURL string, logger *slog.Logger) *Proxy {
	return &Proxy{
		backendURL:   strings.TrimRight(backendURL, "/"),
		client:       &http.Client{Timeout: 120 * time.Second},
		healthClient: &http.Client{Timeout: 5 * time.Second},
		logger:       logger,
	}
}

// Transcribe handles POST /v1/audio/transcriptions
// Accepts multipart/form-data with:
//   - file: audio file (required)
//   - model: model name (ignored — backend decides)
//   - language: ISO language code (optional)
//   - response_format: json, text, srt, vtt (default: json)
//   - prompt: initial prompt (optional)
//
// When the client requests "json" format, the proxy enriches the response
// by also fetching SRT from the backend and parsing it into segments with
// real timestamps. This works around backends that don't support verbose_json.
func (p *Proxy) Transcribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Limit upload size to 100MB
	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)

	// Buffer the entire request body so we can replay it for SRT
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		p.logger.Error("failed to read request body", "error", err)
		http.Error(w, `{"error": "failed to read request body"}`, http.StatusBadRequest)
		return
	}
	contentType := r.Header.Get("Content-Type")

	backendURL := fmt.Sprintf("%s/v1/audio/transcriptions", p.backendURL)

	// Determine if the requested format is JSON (the default) so we can enrich
	// the response with SRT-parsed segments. We must properly parse the multipart
	// form to read the response_format field — NOT substring match on raw binary.
	isJSON := true // default format is json
	requestedFormat := extractMultipartField(bodyBytes, contentType, "response_format")
	if requestedFormat != "" && requestedFormat != "json" {
		isJSON = false
	}

	// Make the primary request (whatever format the client asked for)
	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, backendURL, bytes.NewReader(bodyBytes))
	if err != nil {
		p.logger.Error("failed to create proxy request", "error", err)
		http.Error(w, `{"error": "internal server error"}`, http.StatusInternalServerError)
		return
	}
	proxyReq.Header.Set("Content-Type", contentType)
	proxyReq.ContentLength = int64(len(bodyBytes))

	resp, err := p.client.Do(proxyReq)
	if err != nil {
		p.logger.Error("backend request failed", "error", err, "url", backendURL)
		http.Error(w, `{"error": "transcription backend unavailable"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// If NOT a JSON request or the primary failed, just forward as-is
	if !isJSON || resp.StatusCode != http.StatusOK {
		for k, v := range resp.Header {
			for _, val := range v {
				w.Header().Add(k, val)
			}
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		p.logger.Info("transcription proxied", "status", resp.StatusCode)
		return
	}

	// JSON request — read the JSON response
	jsonBody, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, `{"error": "failed to read backend response"}`, http.StatusInternalServerError)
		return
	}

	var jsonResp map[string]interface{}
	if err := json.Unmarshal(jsonBody, &jsonResp); err != nil {
		// Not valid JSON — forward as-is
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(jsonBody)
		return
	}

	// Now make a parallel SRT request to get segments with timestamps
	srtBody := replaceMIMEField(bodyBytes, contentType, "response_format", "srt")
	srtReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, backendURL, bytes.NewReader(srtBody))
	if err == nil {
		srtReq.Header.Set("Content-Type", contentType)
		srtReq.ContentLength = int64(len(srtBody))
		srtResp, srtErr := p.client.Do(srtReq)
		if srtErr == nil && srtResp.StatusCode == http.StatusOK {
			srtData, _ := io.ReadAll(srtResp.Body)
			srtResp.Body.Close()
			segments := parseSRT(string(srtData))
			if len(segments) > 0 {
				jsonResp["segments"] = segments
				p.logger.Info("enriched JSON with SRT segments", "count", len(segments))
			}
		} else if srtResp != nil {
			srtResp.Body.Close()
		}
	}

	// Return the enriched JSON response
	enriched, _ := json.Marshal(jsonResp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(enriched)
	p.logger.Info("transcription proxied (enriched)", "status", resp.StatusCode)
}

// extractMultipartField reads a single form-field value from a buffered
// multipart body. It properly parses the multipart stream so it never matches
// on binary audio data. Returns "" if the field is not found or parsing fails.
func extractMultipartField(body []byte, contentType, fieldName string) string {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	boundary, ok := params["boundary"]
	if !ok {
		return ""
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		name := part.FormName()
		if name == "" || part.FileName() != "" {
			// Skip file parts — don't read audio into memory
			part.Close()
			continue
		}
		if name == fieldName {
			val, _ := io.ReadAll(io.LimitReader(part, 1024))
			part.Close()
			return strings.TrimSpace(string(val))
		}
		part.Close()
	}
	return ""
}

// replaceMIMEField replaces a multipart form field value in a raw body.
// This is a simple find-and-replace that works for typical multipart form data
// where the field is formatted as: Content-Disposition: form-data; name="response_format"\r\n\r\njson
func replaceMIMEField(body []byte, contentType, field, newValue string) []byte {
	s := string(body)
	// Find the field in the multipart body
	fieldPattern := "name=\"" + field + "\""
	idx := strings.Index(s, fieldPattern)
	if idx < 0 {
		return body // field not found, return as-is
	}
	// Find the value after \r\n\r\n
	afterField := s[idx+len(fieldPattern):]
	headerEnd := strings.Index(afterField, "\r\n\r\n")
	if headerEnd < 0 {
		return body
	}
	valueStart := idx + len(fieldPattern) + headerEnd + 4
	// Find the boundary after the value
	valueEnd := strings.Index(s[valueStart:], "\r\n")
	if valueEnd < 0 {
		return body
	}
	// Replace the old value with the new one
	result := s[:valueStart] + newValue + s[valueStart+valueEnd:]
	return []byte(result)
}

// parseSRT parses an SRT subtitle string into segments with start/end times.
func parseSRT(srt string) []map[string]interface{} {
	var segments []map[string]interface{}
	blocks := strings.Split(strings.TrimSpace(srt), "\n\n")
	for _, block := range blocks {
		lines := strings.Split(strings.TrimSpace(block), "\n")
		if len(lines) < 3 {
			continue
		}
		// Line 0: index, Line 1: timestamps, Line 2+: text
		timeLine := lines[1]
		parts := strings.Split(timeLine, " --> ")
		if len(parts) != 2 {
			continue
		}
		start := parseSRTTime(strings.TrimSpace(parts[0]))
		end := parseSRTTime(strings.TrimSpace(parts[1]))
		text := strings.Join(lines[2:], " ")
		segments = append(segments, map[string]interface{}{
			"start": start,
			"end":   end,
			"text":  text,
		})
	}
	return segments
}

// parseSRTTime converts "HH:MM:SS,mmm" to seconds as float64.
func parseSRTTime(t string) float64 {
	t = strings.Replace(t, ",", ".", 1)
	parts := strings.Split(t, ":")
	if len(parts) != 3 {
		return 0
	}
	h := parseFloat(parts[0])
	m := parseFloat(parts[1])
	s := parseFloat(parts[2])
	return h*3600 + m*60 + s
}

func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
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
			w.Header().Add(k, val)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// Health checks if the backend is reachable.
// Uses a dedicated short-timeout client (5s) to avoid blocking on the
// 120s transcription client timeout during health probes.
func (p *Proxy) Health() error {
	resp, err := p.healthClient.Get(fmt.Sprintf("%s/v1/models", p.backendURL))
	if err != nil {
		return fmt.Errorf("backend unreachable: %w", err)
	}
	// Drain and close the body to return the connection to the pool.
	// Without draining, the TCP connection stays open until GC, exhausting
	// the transport's connection limit under repeated health checks.
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10)) // cap at 1KB
	resp.Body.Close()
	return nil
}
