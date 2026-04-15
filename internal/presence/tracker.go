package presence

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// StatusInfo holds presence status for a user.
type StatusInfo struct {
	UserID   string
	Online   bool
	LastSeen *time.Time
}

// Conn represents a single WebSocket connection.
type Conn struct {
	UserID        string
	WS            *websocket.Conn
	mu            sync.Mutex   // protects WebSocket writes
	authMu        sync.RWMutex // protects Authenticated and TokenExpiry
	subscribedTo  map[string]struct{}
	authenticated bool
	tokenExpiry   time.Time
	// TokenRefreshed is signalled when the client re-authenticates with a new JWT.
	TokenRefreshed chan struct{}
	disconnected   bool
}

// SetAuth updates auth state. Safe for concurrent use.
func (c *Conn) SetAuth(authenticated bool, expiry time.Time) {
	c.authMu.Lock()
	c.authenticated = authenticated
	c.tokenExpiry = expiry
	c.authMu.Unlock()
}

// GetAuth returns the current auth state. Safe for concurrent use.
func (c *Conn) GetAuth() (authenticated bool, expiry time.Time) {
	c.authMu.RLock()
	authenticated, expiry = c.authenticated, c.tokenExpiry
	c.authMu.RUnlock()
	return
}

const writeTimeout = 10 * time.Second

// WriteJSON sends a JSON message to the client, serializing writes.
func (c *Conn) WriteJSON(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	return c.WS.Write(ctx, websocket.MessageText, data)
}

// Tracker manages online connections, subscriptions, and presence broadcasts.
type Tracker struct {
	mu          sync.RWMutex
	connections map[string]map[*Conn]struct{} // user_id → set of conns
	connCount   map[string]int                // user_id → active connection count
	subscribers map[string]map[*Conn]struct{} // watched_user_id → conns watching
	store       *Store
}

// NewTracker creates a tracker backed by the given store.
func NewTracker(store *Store) *Tracker {
	return &Tracker{
		connections: make(map[string]map[*Conn]struct{}),
		connCount:   make(map[string]int),
		subscribers: make(map[string]map[*Conn]struct{}),
		store:       store,
	}
}

// Connect registers a connection. Broadcasts online if this is the user's first device.
func (t *Tracker) Connect(conn *Conn) {
	t.mu.Lock()

	if t.connections[conn.UserID] == nil {
		t.connections[conn.UserID] = make(map[*Conn]struct{})
	}
	t.connections[conn.UserID][conn] = struct{}{}
	t.connCount[conn.UserID]++
	firstDevice := t.connCount[conn.UserID] == 1

	var watchers []*Conn
	if firstDevice {
		for watcher := range t.subscribers[conn.UserID] {
			watchers = append(watchers, watcher)
		}
	}

	t.mu.Unlock()

	if firstDevice {
		slog.Info("user online", "user_id", conn.UserID)
		t.broadcast(watchers, presenceMsg{
			Type:   "presence",
			UserID: conn.UserID,
			Online: true,
		})
	}
}

// Disconnect removes a connection. Broadcasts offline if this was the user's last device.
// Safe to call multiple times — subsequent calls are no-ops.
func (t *Tracker) Disconnect(conn *Conn) {
	t.mu.Lock()

	if conn.disconnected {
		t.mu.Unlock()
		return
	}
	conn.disconnected = true

	if conns, ok := t.connections[conn.UserID]; ok {
		delete(conns, conn)
		t.connCount[conn.UserID]--
		if t.connCount[conn.UserID] <= 0 {
			delete(t.connections, conn.UserID)
			delete(t.connCount, conn.UserID)
		}
	}

	lastDevice := t.connCount[conn.UserID] == 0

	// Clean up this connection's subscriptions.
	for watchedID := range conn.subscribedTo {
		if subs, ok := t.subscribers[watchedID]; ok {
			delete(subs, conn)
			if len(subs) == 0 {
				delete(t.subscribers, watchedID)
			}
		}
	}

	var watchers []*Conn
	var now time.Time
	if lastDevice {
		now = time.Now().UTC()
		for watcher := range t.subscribers[conn.UserID] {
			watchers = append(watchers, watcher)
		}
	}

	t.mu.Unlock()

	if lastDevice {
		t.store.SetLastSeen(conn.UserID, now)
		slog.Info("user offline", "user_id", conn.UserID)
		t.broadcast(watchers, presenceMsg{
			Type:     "presence",
			UserID:   conn.UserID,
			Online:   false,
			LastSeen: &now,
		})
	}
}

// MaxSubscribeSize is the maximum number of user IDs in a single subscribe message.
const MaxSubscribeSize = 200

// Subscribe replaces the connection's subscription list and returns current statuses.
func (t *Tracker) Subscribe(conn *Conn, userIDs []string) []StatusInfo {
	t.mu.Lock()

	// Remove old subscriptions.
	for watchedID := range conn.subscribedTo {
		if subs, ok := t.subscribers[watchedID]; ok {
			delete(subs, conn)
			if len(subs) == 0 {
				delete(t.subscribers, watchedID)
			}
		}
	}

	// Set new subscriptions.
	conn.subscribedTo = make(map[string]struct{}, len(userIDs))
	for _, uid := range userIDs {
		conn.subscribedTo[uid] = struct{}{}
		if t.subscribers[uid] == nil {
			t.subscribers[uid] = make(map[*Conn]struct{})
		}
		t.subscribers[uid][conn] = struct{}{}
	}

	// Collect current statuses while holding the lock.
	statuses := make([]StatusInfo, 0, len(userIDs))
	for _, uid := range userIDs {
		if t.connCount[uid] > 0 {
			statuses = append(statuses, StatusInfo{
				UserID: uid,
				Online: true,
			})
		} else {
			st := StatusInfo{
				UserID: uid,
				Online: false,
			}
			if ts, ok := t.store.GetLastSeen(uid); ok {
				st.LastSeen = &ts
			}
			statuses = append(statuses, st)
		}
	}

	t.mu.Unlock()

	return statuses
}

// presenceMsg is the JSON message sent to subscribers on status change.
type presenceMsg struct {
	Type     string     `json:"type"`
	UserID   string     `json:"user_id"`
	Online   bool       `json:"online"`
	LastSeen *time.Time `json:"last_seen,omitempty"`
}

func (t *Tracker) broadcast(watchers []*Conn, msg presenceMsg) {
	for _, w := range watchers {
		if err := w.WriteJSON(msg); err != nil {
			slog.Debug("failed to send presence event", "error", err)
		}
	}
}
