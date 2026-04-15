package handler

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/markovsdima/zyna-presence/internal/auth"
)

const jwtTTL = 24 * time.Hour

// AuthHandler handles POST /presence/auth.
type AuthHandler struct {
	synapse     *auth.SynapseClient // nil in dev mode
	jwtSecret   string
	limiter     *rateLimiter
	devMode     bool
	devPassword string
}

// NewAuthHandler creates an auth handler with Synapse verification.
func NewAuthHandler(synapse *auth.SynapseClient, jwtSecret string) *AuthHandler {
	rl := newRateLimiter(10, time.Minute)
	return &AuthHandler{
		synapse:   synapse,
		jwtSecret: jwtSecret,
		limiter:   rl,
	}
}

// NewDevAuthHandler creates an auth handler with simple password verification.
// In dev mode, the client sends the password in Authorization header and user_id in X-User-ID.
func NewDevAuthHandler(jwtSecret, devPassword string) *AuthHandler {
	rl := newRateLimiter(10, time.Minute)
	return &AuthHandler{
		jwtSecret:   jwtSecret,
		limiter:     rl,
		devMode:     true,
		devPassword: devPassword,
	}
}

type authResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"`
	UserID    string `json:"user_id"`
}

func (h *AuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ip := clientIP(r)
	if !h.limiter.allow(ip) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	var userID string
	if h.devMode {
		password := r.Header.Get("Authorization")
		if subtle.ConstantTimeCompare([]byte(password), []byte(h.devPassword)) != 1 {
			http.Error(w, "invalid password", http.StatusUnauthorized)
			return
		}
		userID = r.Header.Get("X-User-ID")
		if userID == "" {
			http.Error(w, "X-User-ID header is required in dev mode", http.StatusBadRequest)
			return
		}
	} else {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			http.Error(w, "missing or invalid Authorization header", http.StatusUnauthorized)
			return
		}
		matrixToken := strings.TrimPrefix(header, "Bearer ")
		var err error
		userID, err = h.synapse.VerifyToken(r.Context(), matrixToken)
		if err != nil {
			slog.Warn("synapse token verification failed", "error", err, "ip", ip)
			http.Error(w, "invalid matrix token", http.StatusUnauthorized)
			return
		}
	}

	token, err := auth.GenerateToken(userID, h.jwtSecret, jwtTTL)
	if err != nil {
		slog.Error("failed to generate JWT", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(authResponse{
		Token:     token,
		ExpiresIn: int(jwtTTL.Seconds()),
		UserID:    userID,
	})
}

// Rate limiter.

type rateLimiter struct {
	mu       sync.Mutex
	counters map[string]*ipCounter
	max      int
	window   time.Duration
}

type ipCounter struct {
	count   int
	resetAt time.Time
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	rl := &rateLimiter{
		counters: make(map[string]*ipCounter),
		max:      max,
		window:   window,
	}
	go rl.cleanup()
	return rl
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	c, ok := rl.counters[ip]
	if !ok || now.After(c.resetAt) {
		rl.counters[ip] = &ipCounter{count: 1, resetAt: now.Add(rl.window)}
		return true
	}
	c.count++
	return c.count <= rl.max
}

func (rl *rateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, c := range rl.counters {
			if now.After(c.resetAt) {
				delete(rl.counters, ip)
			}
		}
		rl.mu.Unlock()
	}
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		// Take the first IP in the chain.
		if i := strings.IndexByte(forwarded, ','); i != -1 {
			return strings.TrimSpace(forwarded[:i])
		}
		return strings.TrimSpace(forwarded)
	}
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return ip
	}
	return r.RemoteAddr
}
