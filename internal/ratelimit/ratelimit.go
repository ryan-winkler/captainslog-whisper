// Package ratelimit provides a per-IP token bucket rate limiter with allow list support.
package ratelimit

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Limiter is a per-IP rate limiter with an allow list.
type Limiter struct {
	mu        sync.Mutex
	visitors  map[string]*visitor
	rate      int           // requests per window
	window    time.Duration // window duration
	allowList map[string]bool
	allowNets []*net.IPNet // pre-parsed CIDRs for O(1) per-request check
	enabled   bool
}

type visitor struct {
	tokens    int
	lastReset time.Time
}

// New creates a rate limiter. rate is requests per window.
// allowList is a list of IPs/CIDRs that bypass limiting.
// Pass rate=0 to disable limiting entirely.
func New(rate int, window time.Duration, allowList []string) *Limiter {
	allowed := make(map[string]bool)
	var nets []*net.IPNet
	for _, entry := range allowList {
		entry = strings.TrimSpace(entry)
		if strings.Contains(entry, "/") {
			// Pre-parse CIDR at init time — avoids re-parsing on every request
			if _, network, err := net.ParseCIDR(entry); err == nil {
				nets = append(nets, network)
			}
		} else {
			allowed[entry] = true
		}
	}
	return &Limiter{
		visitors:  make(map[string]*visitor),
		rate:      rate,
		window:    window,
		allowList: allowed,
		allowNets: nets,
		enabled:   rate > 0,
	}
}

// Allow checks if a request from the given IP is allowed.
func (l *Limiter) Allow(ip string) bool {
	if !l.enabled {
		return true
	}

	// Normalize IP (strip port)
	host, _, err := net.SplitHostPort(ip)
	if err != nil {
		host = ip
	}

	// Check allow list (exact IP match or pre-parsed CIDR)
	if l.isAllowed(host) {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	v, exists := l.visitors[host]
	now := time.Now()

	if !exists || now.Sub(v.lastReset) >= l.window {
		l.visitors[host] = &visitor{tokens: l.rate - 1, lastReset: now}
		return true
	}

	if v.tokens > 0 {
		v.tokens--
		return true
	}

	return false
}

func (l *Limiter) isAllowed(ip string) bool {
	if l.allowList[ip] {
		return true
	}
	// Check pre-parsed CIDR ranges
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, network := range l.allowNets {
		if network.Contains(parsed) {
			return true
		}
	}
	return false
}

// Middleware returns an HTTP middleware that enforces rate limits.
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	if !l.enabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(r.RemoteAddr) {
			http.Error(w, `{"error": "rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Cleanup removes stale visitors. Call periodically.
func (l *Limiter) Cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-l.window * 2)
	for ip, v := range l.visitors {
		if v.lastReset.Before(cutoff) {
			delete(l.visitors, ip)
		}
	}
}
