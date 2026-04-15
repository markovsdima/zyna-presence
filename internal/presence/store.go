package presence

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"time"
)

// Store keeps last_seen timestamps in memory and flushes them to a JSON file.
type Store struct {
	mu       sync.RWMutex
	lastSeen map[string]time.Time
	filePath string
}

// NewStore creates a store and loads existing data from the JSON file.
func NewStore(filePath string) (*Store, error) {
	s := &Store{
		lastSeen: make(map[string]time.Time),
		filePath: filePath,
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}

	if len(data) > 0 {
		if err := json.Unmarshal(data, &s.lastSeen); err != nil {
			return nil, err
		}
	}

	slog.Info("loaded last_seen data", "users", len(s.lastSeen))
	return s, nil
}

// SetLastSeen records the last seen time for a user.
func (s *Store) SetLastSeen(userID string, t time.Time) {
	s.mu.Lock()
	s.lastSeen[userID] = t.UTC()
	s.mu.Unlock()
}

// GetLastSeen returns the last seen time for a user.
func (s *Store) GetLastSeen(userID string) (time.Time, bool) {
	s.mu.RLock()
	t, ok := s.lastSeen[userID]
	s.mu.RUnlock()
	return t, ok
}

// GetMultipleLastSeen returns last seen times for multiple users.
func (s *Store) GetMultipleLastSeen(userIDs []string) map[string]*time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]*time.Time, len(userIDs))
	for _, id := range userIDs {
		if t, ok := s.lastSeen[id]; ok {
			ts := t
			result[id] = &ts
		}
	}
	return result
}

// Flush writes the current state to the JSON file atomically.
func (s *Store) Flush() error {
	s.mu.RLock()
	data, err := json.Marshal(s.lastSeen)
	s.mu.RUnlock()
	if err != nil {
		return err
	}

	tmpPath := s.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.filePath)
}

// StartPeriodicFlush runs Flush every interval until the context is cancelled.
// It also flushes one final time on shutdown.
func (s *Store) StartPeriodicFlush(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := s.Flush(); err != nil {
				slog.Error("failed to flush last_seen", "error", err)
			}
		case <-ctx.Done():
			if err := s.Flush(); err != nil {
				slog.Error("failed to flush last_seen on shutdown", "error", err)
			}
			return
		}
	}
}
