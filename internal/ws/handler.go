package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/markovsdima/zyna-presence/internal/auth"
	"github.com/markovsdima/zyna-presence/internal/presence"
)

const (
	authTimeout      = 10 * time.Second
	readLimit        = 4096
	heartbeatTimeout = 60 * time.Second
)

// Client → server messages.
type incomingMessage struct {
	Type    string   `json:"type"`
	Token   string   `json:"token,omitempty"`
	UserIDs []string `json:"user_ids,omitempty"`
}

// Server → client messages.
type statusEntry struct {
	UserID   string     `json:"user_id"`
	Online   bool       `json:"online"`
	LastSeen *time.Time `json:"last_seen,omitempty"`
}

type statusesResponse struct {
	Type  string        `json:"type"`
	Users []statusEntry `json:"users"`
}

type tokenExpiredMsg struct {
	Type string `json:"type"`
}

type errorMsg struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Handler upgrades HTTP connections to WebSocket and manages the connection lifecycle.
type Handler struct {
	tracker   *presence.Tracker
	jwtSecret string
}

// NewHandler creates a WebSocket handler.
func NewHandler(tracker *presence.Tracker, jwtSecret string) *Handler {
	return &Handler{tracker: tracker, jwtSecret: jwtSecret}
}

// ServeHTTP upgrades the connection and enters the read loop.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	wsConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// iOS clients don't send Origin headers, so origin check is not applicable.
		// Auth is enforced via JWT in the first WebSocket message.
		InsecureSkipVerify: true,
	})
	if err != nil {
		slog.Error("websocket accept failed", "error", err)
		return
	}
	wsConn.SetReadLimit(readLimit)

	conn := &presence.Conn{
		WS:             wsConn,
		TokenRefreshed: make(chan struct{}, 1),
	}

	// First message must be auth within the timeout.
	if !h.handleAuth(conn) {
		wsConn.Close(websocket.StatusPolicyViolation, "auth failed")
		return
	}

	h.tracker.Connect(conn)
	defer h.tracker.Disconnect(conn)
	defer wsConn.Close(websocket.StatusNormalClosure, "")

	// Start token expiry watcher.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.watchTokenExpiry(ctx, conn)

	// Read loop with heartbeat timeout.
	for {
		readCtx, readCancel := context.WithTimeout(context.Background(), heartbeatTimeout)
		_, data, err := conn.WS.Read(readCtx)
		readCancel()
		if err != nil {
			slog.Debug("connection closed", "user_id", conn.UserID, "error", err)
			return
		}

		var msg incomingMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			slog.Debug("invalid message", "error", err)
			continue
		}

		switch msg.Type {
		case "auth":
			h.handleReauth(conn, msg.Token)
		case "subscribe":
			if authed, expiry := conn.GetAuth(); !authed || time.Now().After(expiry) {
				continue
			}
			if len(msg.UserIDs) > presence.MaxSubscribeSize {
				conn.WriteJSON(errorMsg{Type: "error", Message: "too many user_ids (max 200)"})
				continue
			}
			statuses := h.tracker.Subscribe(conn, msg.UserIDs)
			entries := make([]statusEntry, len(statuses))
			for i, s := range statuses {
				entries[i] = statusEntry{
					UserID:   s.UserID,
					Online:   s.Online,
					LastSeen: s.LastSeen,
				}
			}
			conn.WriteJSON(statusesResponse{
				Type:  "statuses",
				Users: entries,
			})
		case "ping":
			// Client heartbeat — no response needed, the read itself resets the deadline.
		}
	}
}

func (h *Handler) handleAuth(conn *presence.Conn) bool {
	ctx, cancel := context.WithTimeout(context.Background(), authTimeout)
	defer cancel()

	_, data, err := conn.WS.Read(ctx)
	if err != nil {
		return false
	}

	var msg incomingMessage
	if err := json.Unmarshal(data, &msg); err != nil || msg.Type != "auth" {
		return false
	}

	userID, expiresAt, err := auth.ValidateToken(msg.Token, h.jwtSecret)
	if err != nil {
		return false
	}

	conn.UserID = userID
	conn.SetAuth(true, expiresAt)
	return true
}

func (h *Handler) handleReauth(conn *presence.Conn, token string) {
	userID, expiresAt, err := auth.ValidateToken(token, h.jwtSecret)
	if err != nil {
		conn.WriteJSON(errorMsg{Type: "error", Message: "invalid token"})
		return
	}
	if userID != conn.UserID {
		conn.WriteJSON(errorMsg{Type: "error", Message: "user_id mismatch"})
		return
	}
	conn.SetAuth(true, expiresAt)

	// Signal the watcher to restart with the new expiry.
	select {
	case conn.TokenRefreshed <- struct{}{}:
	default:
	}
}

func (h *Handler) watchTokenExpiry(ctx context.Context, conn *presence.Conn) {
	for {
		_, expiry := conn.GetAuth()
		remaining := time.Until(expiry)
		if remaining <= 0 {
			conn.WriteJSON(tokenExpiredMsg{Type: "token_expired"})
			// Wait for reauth or disconnect.
			select {
			case <-conn.TokenRefreshed:
				continue
			case <-ctx.Done():
				return
			}
		}

		timer := time.NewTimer(remaining)
		select {
		case <-timer.C:
			conn.WriteJSON(tokenExpiredMsg{Type: "token_expired"})
			// Wait for reauth or disconnect.
			select {
			case <-conn.TokenRefreshed:
				continue
			case <-ctx.Done():
				timer.Stop()
				return
			}
		case <-conn.TokenRefreshed:
			timer.Stop()
			continue
		case <-ctx.Done():
			timer.Stop()
			return
		}
	}
}
