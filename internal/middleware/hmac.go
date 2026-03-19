package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"
)

// HMACAuth validates the X-API-Key header against a rotating HMAC.
// The client computes HMAC-SHA256(secret, "2006-01-02") using today's UTC date.
// The server accepts today's or yesterday's HMAC to handle the midnight boundary.
func HMACAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-API-Key")
			if apiKey == "" {
				http.Error(w, "missing API key", http.StatusUnauthorized)
				return
			}

			now := time.Now().UTC()
			today := now.Format("2006-01-02")
			yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")

			if !validHMAC(secret, today, apiKey) && !validHMAC(secret, yesterday, apiKey) {
				http.Error(w, "invalid API key", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func validHMAC(secret, date, candidate string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(date))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(candidate))
}
