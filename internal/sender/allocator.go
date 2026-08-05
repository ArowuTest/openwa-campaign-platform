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
	PoolID                string
	GatewayPoolID         string
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

type AllocationRoute struct {
	SenderPoolID      string
	LegacyPool        string
	GatewayPoolID     string
	SpecificSessionID string
}

func (r AllocationRoute) Validate() error {
	if strings.TrimSpace(r.SpecificSessionID) != "" {
		if strings.TrimSpace(r.SenderPoolID) != "" || strings.TrimSpace(r.LegacyPool) != "" {
			return errors.New("specific-session allocation cannot also select a sender pool")
		}
		return nil
	}
	if strings.TrimSpace(r.SenderPoolID) == "" && strings.TrimSpace(r.LegacyPool) == "" {
		return errors.New("sender pool allocation is required")
	}
	return nil
}

type Allocator interface {
	Assign(context.Context, string, AllocationRoute, time.Time) (string, error)
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
func (a *MemoryAllocator) Assign(_ context.Context, recipientID string, route AllocationRoute, now time.Time) (string, error) {
	recipientID = strings.TrimSpace(recipientID)
	if recipientID == "" {
		return "", errors.New("recipient is required")
	}
	if err := route.Validate(); err != nil {
		return "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if existing := a.assignments[recipientID]; existing != "" {
		session, ok := a.sessions[existing]
		if !ok || !sessionMatchesRoute(session, route) {
			return "", ErrAssignmentConflict
		}
		if !sessionHealthyForAllocation(session, now) {
			return "", ErrNoHealthySession
		}
		return existing, nil
	}
	candidates := make([]Session, 0)
	for _, value := range a.sessions {
		if !sessionMatchesRoute(value, route) || !sessionHealthyForAllocation(value, now) {
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

func sessionMatchesRoute(value Session, route AllocationRoute) bool {
	if route.SpecificSessionID != "" && value.ID != strings.TrimSpace(route.SpecificSessionID) {
		return false
	}
	if route.SenderPoolID != "" && value.PoolID != strings.TrimSpace(route.SenderPoolID) {
		return false
	}
	if route.SenderPoolID == "" && route.LegacyPool != "" && value.Pool != strings.TrimSpace(route.LegacyPool) {
		return false
	}
	return route.GatewayPoolID == "" || value.GatewayPoolID == strings.TrimSpace(route.GatewayPoolID)
}

func sessionHealthyForAllocation(value Session, now time.Time) bool {
	if !value.NodeReady || value.NodeDraining || !value.LeaseExpiresAt.After(now) {
		return false
	}
	if value.Status != StatusReady && value.Status != StatusBusy {
		return false
	}
	if value.SafeMessagesPerMinute < 0 || value.SafeDailyCapacity < 0 {
		return false
	}
	if value.SafeDailyCapacity > 0 && value.SafeDailyCapacity <= value.SentToday {
		return false
	}
	return value.InFlightLimit <= 0 || value.InFlight < value.InFlightLimit
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
