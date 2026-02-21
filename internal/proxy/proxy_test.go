// Package proxy — tests for the Whisper API proxy.
//
// These tests verify the verbose_json upgrade, SRT fallback, format passthrough,
// multipart field extraction/replacement, and SRT parsing. All tests use
// httptest servers to simulate real backend behavior without network I/O.
package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestProxy creates a proxy pointed at the given backend URL with a no-op logger.
func newTestProxy(backendURL string) *Proxy {
	return New(backendURL, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// buildMultipartBody constructs a multipart/form-data body with an audio file
// and optional form fields. Returns the body bytes and content-type header.
func buildMultipartBody(t *testing.T, audioData []byte, fields map[string]string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	// Write the audio file part
	part, err := w.CreateFormFile("file", "test.wav")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(audioData); err != nil {
		t.Fatalf("Write audio: %v", err)
	}

	// Write form fields
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("WriteField %s: %v", k, err)
		}
	}
	w.Close()
	return buf.Bytes(), w.FormDataContentType()
}

// TestTranscribe_VerboseJSONUpgrade verifies that when the client requests "json",
// the proxy upgrades the request to "verbose_json" before sending to the backend.
func TestTranscribe_VerboseJSONUpgrade(t *testing.T) {
	var receivedFormat string

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Parse the multipart to check what format the proxy sent
		r.ParseMultipartForm(10 << 20)
		receivedFormat = r.FormValue("response_format")

		// Return verbose_json with segments
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"text": "hello world",
			"segments": []map[string]any{
				{"start": 0.0, "end": 1.5, "text": "hello"},
				{"start": 1.5, "end": 3.0, "text": "world"},
			},
		})
	}))
	defer backend.Close()

	p := newTestProxy(backend.URL)

	body, ct := buildMultipartBody(t, []byte("fake-audio"), map[string]string{
		"response_format": "json",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	p.Transcribe(rec, req)

	// Verify upgrade happened
	if receivedFormat != "verbose_json" {
		t.Errorf("backend received format %q, want %q", receivedFormat, "verbose_json")
	}

	// Verify response is valid JSON with segments
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["text"] != "hello world" {
		t.Errorf("text = %q, want %q", resp["text"], "hello world")
	}
	segments, ok := resp["segments"].([]any)
	if !ok || len(segments) != 2 {
		t.Errorf("segments count = %v, want 2", resp["segments"])
	}
}

// TestTranscribe_VerboseJSONPassthrough verifies that if the client explicitly
// requests "verbose_json", the proxy does NOT rewrite the format.
func TestTranscribe_VerboseJSONPassthrough(t *testing.T) {
	var receivedFormat string

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(10 << 20)
		receivedFormat = r.FormValue("response_format")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"text":     "test",
			"segments": []any{},
		})
	}))
	defer backend.Close()

	p := newTestProxy(backend.URL)
	body, ct := buildMultipartBody(t, []byte("audio"), map[string]string{
		"response_format": "verbose_json",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	p.Transcribe(rec, req)

	if receivedFormat != "verbose_json" {
		t.Errorf("backend received format %q, want %q (passthrough)", receivedFormat, "verbose_json")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// TestTranscribe_NonJSONPassthrough verifies that text/srt/vtt formats are
// forwarded directly without any JSON processing.
func TestTranscribe_NonJSONPassthrough(t *testing.T) {
	for _, format := range []string{"text", "srt", "vtt"} {
		t.Run(format, func(t *testing.T) {
			var receivedFormat string
			responseBody := "This is raw " + format + " output"

			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.ParseMultipartForm(10 << 20)
				receivedFormat = r.FormValue("response_format")
				w.Header().Set("Content-Type", "text/plain")
				fmt.Fprint(w, responseBody)
			}))
			defer backend.Close()

			p := newTestProxy(backend.URL)
			body, ct := buildMultipartBody(t, []byte("audio"), map[string]string{
				"response_format": format,
			})
			req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body))
			req.Header.Set("Content-Type", ct)
			rec := httptest.NewRecorder()

			p.Transcribe(rec, req)

			// Non-JSON formats should NOT be rewritten
			if receivedFormat != format {
				t.Errorf("backend received format %q, want %q", receivedFormat, format)
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if got := rec.Body.String(); got != responseBody {
				t.Errorf("body = %q, want %q", got, responseBody)
			}
		})
	}
}

// TestTranscribe_DefaultFormatIsJSON verifies that when no response_format is
// specified, the proxy defaults to json (upgraded to verbose_json).
func TestTranscribe_DefaultFormatIsJSON(t *testing.T) {
	var receivedFormat string

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(10 << 20)
		receivedFormat = r.FormValue("response_format")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"text":     "default format test",
			"segments": []any{},
		})
	}))
	defer backend.Close()

	p := newTestProxy(backend.URL)
	// No response_format field
	body, ct := buildMultipartBody(t, []byte("audio"), nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	p.Transcribe(rec, req)

	if receivedFormat != "verbose_json" {
		t.Errorf("backend received format %q, want %q (default json → verbose_json)", receivedFormat, "verbose_json")
	}
}

// TestTranscribe_SRTFallback verifies that when the backend returns JSON without
// segments (backend doesn't support verbose_json), the proxy falls back to an
// SRT request to enrich the response.
func TestTranscribe_SRTFallback(t *testing.T) {
	callCount := 0

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(10 << 20)
		format := r.FormValue("response_format")
		callCount++

		w.Header().Set("Content-Type", "application/json")
		if format == "srt" {
			// SRT response
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "1\n00:00:00,000 --> 00:00:01,500\nhello\n\n2\n00:00:01,500 --> 00:00:03,000\nworld\n")
		} else {
			// verbose_json but without segments (backend ignores the format)
			json.NewEncoder(w).Encode(map[string]any{
				"text": "hello world",
			})
		}
	}))
	defer backend.Close()

	p := newTestProxy(backend.URL)
	body, ct := buildMultipartBody(t, []byte("audio"), map[string]string{
		"response_format": "json",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	p.Transcribe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// Should have made 2 calls: one for verbose_json, one for SRT fallback
	if callCount != 2 {
		t.Errorf("backend call count = %d, want 2 (verbose_json + SRT fallback)", callCount)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	segments, ok := resp["segments"].([]any)
	if !ok || len(segments) != 2 {
		t.Fatalf("expected 2 SRT-parsed segments, got %v", resp["segments"])
	}

	// Verify segment content
	seg0, _ := segments[0].(map[string]any)
	if seg0["text"] != "hello" {
		t.Errorf("segment 0 text = %q, want %q", seg0["text"], "hello")
	}
}

// TestTranscribe_BackendError verifies that backend errors are forwarded to the client.
func TestTranscribe_BackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error": "model not loaded"}`, http.StatusServiceUnavailable)
	}))
	defer backend.Close()

	p := newTestProxy(backend.URL)
	body, ct := buildMultipartBody(t, []byte("audio"), map[string]string{
		"response_format": "json",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	p.Transcribe(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

// TestTranscribe_MethodNotAllowed verifies that non-POST requests are rejected.
func TestTranscribe_MethodNotAllowed(t *testing.T) {
	p := newTestProxy("http://unused")
	req := httptest.NewRequest(http.MethodGet, "/v1/audio/transcriptions", nil)
	rec := httptest.NewRecorder()

	p.Transcribe(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

// TestTranscribe_BackendUnreachable verifies that connection failures return 502.
func TestTranscribe_BackendUnreachable(t *testing.T) {
	p := newTestProxy("http://127.0.0.1:1") // Port 1 — guaranteed unreachable
	body, ct := buildMultipartBody(t, []byte("audio"), map[string]string{
		"response_format": "text",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	p.Transcribe(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

// --- Unit tests for helper functions ---

func TestExtractMultipartField(t *testing.T) {
	body, ct := buildMultipartBody(t, []byte("audio-data"), map[string]string{
		"response_format": "json",
		"language":        "en",
	})

	if got := extractMultipartField(body, ct, "response_format"); got != "json" {
		t.Errorf("response_format = %q, want %q", got, "json")
	}
	if got := extractMultipartField(body, ct, "language"); got != "en" {
		t.Errorf("language = %q, want %q", got, "en")
	}
	if got := extractMultipartField(body, ct, "nonexistent"); got != "" {
		t.Errorf("nonexistent = %q, want empty", got)
	}
}

func TestExtractMultipartField_NoField(t *testing.T) {
	body, ct := buildMultipartBody(t, []byte("audio-data"), nil)
	if got := extractMultipartField(body, ct, "response_format"); got != "" {
		t.Errorf("should return empty for missing field, got %q", got)
	}
}

func TestExtractMultipartField_InvalidContentType(t *testing.T) {
	if got := extractMultipartField([]byte("data"), "text/plain", "field"); got != "" {
		t.Errorf("should return empty for non-multipart content-type, got %q", got)
	}
}

func TestReplaceMIMEField(t *testing.T) {
	body, ct := buildMultipartBody(t, []byte("audio"), map[string]string{
		"response_format": "json",
	})

	replaced := replaceMIMEField(body, ct, "response_format", "verbose_json")

	// Verify the replacement by extracting the field
	got := extractMultipartField(replaced, ct, "response_format")
	if got != "verbose_json" {
		t.Errorf("after replacement, response_format = %q, want %q", got, "verbose_json")
	}
}

func TestReplaceMIMEField_FieldNotFound(t *testing.T) {
	body, ct := buildMultipartBody(t, []byte("audio"), nil)

	replaced := replaceMIMEField(body, ct, "nonexistent", "value")

	// Should return body unchanged
	if !bytes.Equal(replaced, body) {
		t.Error("body should be unchanged when field not found")
	}
}

func TestParseSRT(t *testing.T) {
	srt := `1
00:00:00,000 --> 00:00:01,500
Hello world

2
00:00:01,500 --> 00:00:03,200
How are you

3
00:00:03,200 --> 00:00:05,000
I am fine`

	segments := parseSRT(srt)
	if len(segments) != 3 {
		t.Fatalf("parsed %d segments, want 3", len(segments))
	}

	tests := []struct {
		idx   int
		start float64
		end   float64
		text  string
	}{
		{0, 0.0, 1.5, "Hello world"},
		{1, 1.5, 3.2, "How are you"},
		{2, 3.2, 5.0, "I am fine"},
	}

	for _, tt := range tests {
		seg := segments[tt.idx]
		if seg["text"] != tt.text {
			t.Errorf("segment %d text = %q, want %q", tt.idx, seg["text"], tt.text)
		}
		if start, _ := seg["start"].(float64); start != tt.start {
			t.Errorf("segment %d start = %v, want %v", tt.idx, start, tt.start)
		}
		if end, _ := seg["end"].(float64); end != tt.end {
			t.Errorf("segment %d end = %v, want %v", tt.idx, end, tt.end)
		}
	}
}

func TestParseSRT_Empty(t *testing.T) {
	if segments := parseSRT(""); len(segments) != 0 {
		t.Errorf("empty SRT should return 0 segments, got %d", len(segments))
	}
}

func TestParseSRT_MalformedBlock(t *testing.T) {
	// Missing timestamp line
	srt := "1\nHello world\n"
	segments := parseSRT(srt)
	if len(segments) != 0 {
		t.Errorf("malformed SRT should return 0 segments, got %d", len(segments))
	}
}

func TestParseSRTTime(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"00:00:00,000", 0.0},
		{"00:00:01,500", 1.5},
		{"00:01:00,000", 60.0},
		{"01:00:00,000", 3600.0},
		{"01:23:45,678", 5025.678},
	}
	for _, tt := range tests {
		got := parseSRTTime(tt.input)
		if got != tt.want {
			t.Errorf("parseSRTTime(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestParseSRTTime_Invalid(t *testing.T) {
	if got := parseSRTTime("invalid"); got != 0 {
		t.Errorf("invalid time should return 0, got %v", got)
	}
}

// --- Translate tests ---

func TestTranslate_Success(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/translations" {
			t.Errorf("path = %q, want /v1/audio/translations", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"text": "translated"})
	}))
	defer backend.Close()

	p := newTestProxy(backend.URL)
	body, ct := buildMultipartBody(t, []byte("audio"), nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/translations", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	p.Translate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "translated") {
		t.Error("response should contain translated text")
	}
}

func TestTranslate_MethodNotAllowed(t *testing.T) {
	p := newTestProxy("http://unused")
	req := httptest.NewRequest(http.MethodGet, "/v1/audio/translations", nil)
	rec := httptest.NewRecorder()

	p.Translate(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

// --- Health tests ---

func TestHealth_Success(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("health path = %q, want /v1/models", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer backend.Close()

	p := newTestProxy(backend.URL)
	if err := p.Health(); err != nil {
		t.Errorf("Health() = %v, want nil", err)
	}
}

func TestHealth_Unreachable(t *testing.T) {
	p := newTestProxy("http://127.0.0.1:1")
	if err := p.Health(); err == nil {
		t.Error("Health() should return error for unreachable backend")
	}
}
