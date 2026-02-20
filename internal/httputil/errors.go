// Package httputil provides centralized HTTP error handling for Captain's Log.
//
// Every HTTP error response MUST go through this package so that:
//  1. Errors are always logged to stdout (visible in journalctl/docker logs)
//  2. Responses are always JSON-formatted for API consumers
//  3. Each error site documents WHY with a descriptive message
//  4. Status codes are consistent across all endpoints
//
// Usage:
//
//	httputil.Error(w, r, logger, http.StatusBadRequest, "no file in multipart form",
//	    "WHY: recording upload requires a 'file' field in the multipart body")
package httputil

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Error writes a JSON error response, logs the error to the configured logger,
// and records the request context (method, path, remote addr) for debugging.
//
// The 'reason' parameter is the user-facing error message returned in the JSON body.
// The 'why' parameter is an internal explanation logged but NOT sent to the client —
// it documents the root cause and helps future maintainers understand the decision.
//
// Example:
//
//	httputil.Error(w, r, logger, 401, "unauthorized",
//	    "WHY: constant-time compare failed — token mismatch or missing Authorization header")
func Error(w http.ResponseWriter, r *http.Request, logger *slog.Logger, status int, reason string, why string) {
	// Always log errors so they appear in stdout/log files.
	// Include request context for tracing — without method+path+remote,
	// errors in production are nearly impossible to correlate.
	logger.Error(reason,
		"status", status,
		"method", r.Method,
		"path", r.URL.Path,
		"remote", r.RemoteAddr,
		"why", why,
	)

	// Respond with JSON so API consumers and the browser UI can parse the error.
	// Plain text responses break fetch().json() in the frontend.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"error":  reason,
		"status": status,
	})
}

// ServerError is a convenience for 500 Internal Server Error — the most common
// error type for unexpected failures (disk full, permission denied, etc.).
// These always warrant investigation, so they log at Error level with full context.
func ServerError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, reason string, why string, err error) {
	logger.Error(reason,
		"status", http.StatusInternalServerError,
		"method", r.Method,
		"path", r.URL.Path,
		"remote", r.RemoteAddr,
		"why", why,
		"error", err,
	)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	// Never leak internal error details to the client — the 'reason' is safe,
	// but the actual 'err' may contain filesystem paths, config values, etc.
	json.NewEncoder(w).Encode(map[string]any{
		"error":  reason,
		"status": http.StatusInternalServerError,
	})
}
