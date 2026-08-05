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
	StatusQuarantined  Status = "QUARANTINED"
	StatusRetired      Status = "RETIRED"
)

type Session struct {
	ID                    string
	Pool                  string
	Status                Status
	LeaseExpiresAt        time.Time
	NodeReady             bool
	NodeDraining          bool
	SafeMessagesPerMinute int
	SafeDailyCapacity     int64
	SentToday             int64
	InFlight              int
	InFlightLimit         int
	RecentFailureRate     float64
	LastSuccessfulAt      time.Time
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
		if value.SafeMessagesPerMinute < 0 || value.SafeDailyCapacity < 0 {
			continue
		}
		if value.SafeDailyCapacity > 0 && value.SafeDailyCapacity <= value.SentToday {
			continue
		}
		if value.InFlightLimit > 0 && value.InFlight >= value.InFlightLimit {
			continue
		}
		candidates = append(candidates, value)
	}
	if len(candidates) == 0 {
		return "", ErrNoHealthySession
	}
	sort.Slice(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.Status != right.Status {
			return left.Status == StatusReady
		}
		leftDaily, rightDaily := utilisationRatio(left.SentToday, left.SafeDailyCapacity), utilisationRatio(right.SentToday, right.SafeDailyCapacity)
		if leftDaily != rightDaily {
			return leftDaily < rightDaily
		}
		leftFlight, rightFlight := utilisationRatio(int64(left.InFlight), int64(left.InFlightLimit)), utilisationRatio(int64(right.InFlight), int64(right.InFlightLimit))
		if leftFlight != rightFlight {
			return leftFlight < rightFlight
		}
		if left.RecentFailureRate != right.RecentFailureRate {
			return left.RecentFailureRate < right.RecentFailureRate
		}
		if !left.LastSuccessfulAt.Equal(right.LastSuccessfulAt) {
			return left.LastSuccessfulAt.After(right.LastSuccessfulAt)
		}
		leftScore := assignmentScore(recipientID, left.ID)
		rightScore := assignmentScore(recipientID, right.ID)
		if leftScore == rightScore {
			return left.ID < right.ID
		}
		return leftScore < rightScore
	})
	a.assignments[recipientID] = candidates[0].ID
	return candidates[0].ID, nil
}
func utilisationRatio(used, capacity int64) float64 {
	if capacity <= 0 {
		return 1
	}
	return float64(used) / float64(capacity)
}

func assignmentScore(recipientID, sessionID string) string {
	sum := sha256.Sum256([]byte(recipientID + "\x1f" + sessionID))
	return string(sum[:])
}
