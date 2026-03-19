package service

import (
	"context"
	"time"

	"github.com/markovsdima/zyna-presence/internal/storage"
)

type UserStatus struct {
	Online   bool       `json:"online"`
	LastSeen *time.Time `json:"last_seen,omitempty"`
}

type PresenceService struct {
	store storage.PresenceStore
	ttl   time.Duration
}

func NewPresenceService(store storage.PresenceStore, ttl time.Duration) *PresenceService {
	return &PresenceService{store: store, ttl: ttl}
}

func (s *PresenceService) Heartbeat(ctx context.Context, userID string) error {
	return s.store.SetPresence(ctx, userID, s.ttl)
}

func (s *PresenceService) BatchStatus(ctx context.Context, userIDs []string) (map[string]UserStatus, error) {
	presenceMap, err := s.store.GetPresence(ctx, userIDs)
	if err != nil {
		return nil, err
	}

	result := make(map[string]UserStatus, len(userIDs))
	for _, id := range userIDs {
		info := presenceMap[id]
		result[id] = UserStatus{
			Online:   info.Online,
			LastSeen: info.LastSeen,
		}
	}
	return result, nil
}
