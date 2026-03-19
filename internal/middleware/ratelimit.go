package middleware

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markovsdima/zyna-presence/internal/storage"
)

// IPRateLimit enforces a per-IP request limit using a sliding window in Redis.
func IPRateLimit(store storage.PresenceStore, maxRequests int) func(http.Handler) http.Handler {
	window := time.Minute

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			key := fmt.Sprintf("rl:ip:%s", ip)

			count, err := store.IncrementRateLimit(r.Context(), key, window)
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}

			if count > int64(maxRequests) {
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// UserHeartbeatRateLimit enforces a minimum interval between heartbeats per user_id.
func UserHeartbeatRateLimit(store storage.PresenceStore, interval time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := chi.URLParam(r, "userID")
			if userID == "" {
				next.ServeHTTP(w, r)
				return
			}

			key := fmt.Sprintf("rl:uid:%s", userID)
			ok, err := store.SetIfNotExists(r.Context(), key, interval)
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if !ok {
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		// Take the first IP in the chain
		if ip, _, err := net.SplitHostPort(forwarded); err == nil {
			return ip
		}
		return forwarded
	}
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return ip
	}
	return r.RemoteAddr
}
