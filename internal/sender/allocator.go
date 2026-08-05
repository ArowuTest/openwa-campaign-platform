package sender

import (
	"context"
	"crypto/sha256"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrNoHealthySession   = errors.New("no healthy sender session is available")
	ErrAssignmentConflict = errors.New("recipient is already assigned to a different sender session")
)

type Status string

const (
	StatusReady        Status = "READY"
	StatusBusy         Status = "BUSY"
	StatusPaused       Status = "PAUSED"
	StatusDisconnected Status = "DISCONNECTED"
	StatusRestricted   Status = "RESTRICTED"
	StatusRetired      Status = "RETIRED"
)

type Session struct {
	ID             string
	Pool           string
	Status         Status
	LeaseExpiresAt time.Time
	NodeReady      bool
	NodeDraining   bool
}

type Allocator interface {
	Assign(context.Context, string, string, time.Time) (string, error)
}

type MemoryAllocator struct {
	mu          sync.Mutex
	sessions    map[string]Session
	assignments map[string]string
}

func NewMemoryAllocator(sessions ...Session) *MemoryAllocator {
	a := &MemoryAllocator{sessions: make(map[string]Session), assignments: make(map[string]string)}
	for _, value := range sessions {
		a.sessions[value.ID] = value
	}
	return a
}
func (a *MemoryAllocator) Assign(_ context.Context, recipientID, pool string, now time.Time) (string, error) {
	recipientID, pool = strings.TrimSpace(recipientID), strings.TrimSpace(pool)
	if recipientID == "" || pool == "" {
		return "", errors.New("recipient and sender pool are required")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if existing := a.assignments[recipientID]; existing != "" {
		return existing, nil
	}
	candidates := make([]Session, 0)
	for _, value := range a.sessions {
		if value.Pool != pool || !value.NodeReady || value.NodeDraining || !value.LeaseExpiresAt.After(now) {
			continue
		}
		if value.Status != StatusReady && value.Status != StatusBusy {
			continue
		}
		candidates = append(candidates, value)
	}
	if len(candidates) == 0 {
		return "", ErrNoHealthySession
	}
	sort.Slice(candidates, func(i, j int) bool {
		left := assignmentScore(recipientID, candidates[i].ID)
		right := assignmentScore(recipientID, candidates[j].ID)
		if left == right {
			return candidates[i].ID < candidates[j].ID
		}
		return left < right
	})
	a.assignments[recipientID] = candidates[0].ID
	return candidates[0].ID, nil
}
func assignmentScore(recipientID, sessionID string) string {
	sum := sha256.Sum256([]byte(recipientID + "\x1f" + sessionID))
	return string(sum[:])
}
