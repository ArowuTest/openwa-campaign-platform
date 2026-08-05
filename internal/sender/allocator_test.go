package sender

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAssignmentIsStableAndConcurrentSafe(t *testing.T) {
	now := time.Now().UTC()
	allocator := NewMemoryAllocator(
		Session{ID: "session-a", Pool: "pool-1", Status: StatusReady, NodeReady: true, LeaseExpiresAt: now.Add(time.Hour)},
		Session{ID: "session-b", Pool: "pool-1", Status: StatusReady, NodeReady: true, LeaseExpiresAt: now.Add(time.Hour)},
	)
	results := make(chan string, 20)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := allocator.Assign(context.Background(), "recipient-1", AllocationRoute{LegacyPool: "pool-1"}, now)
			if err != nil {
				t.Error(err)
				return
			}
			results <- value
		}()
	}
	wg.Wait()
	close(results)
	first := ""
	for value := range results {
		if first == "" {
			first = value
		}
		if value != first {
			t.Fatalf("unstable assignment %s != %s", value, first)
		}
	}
}
func TestAssignmentRejectsUnhealthyOrUnleasedSessions(t *testing.T) {
	now := time.Now().UTC()
	allocator := NewMemoryAllocator(Session{ID: "expired", Pool: "pool", Status: StatusReady, NodeReady: true, LeaseExpiresAt: now.Add(-time.Second)})
	if _, err := allocator.Assign(context.Background(), "recipient", AllocationRoute{LegacyPool: "pool"}, now); err != ErrNoHealthySession {
		t.Fatalf("err=%v", err)
	}
}

func TestPostgresAllocationQueryEnforcesLeaseHealthAndInflightBounds(t *testing.T) {
	required := []string{
		"sl.worker_node_id=ss.node_id",
		"ss.sender_pool_id=nullif($2,'')::uuid",
		"ss.gateway_pool_id=nullif($4,'')::uuid",
		"ss.id=nullif($5,'')::uuid",
		"ss.last_heartbeat_at>$7",
		"sn.last_heartbeat_at>$7",
		"sl.expires_at>$6",
		"safe_messages_per_minute",
		"safe_daily_capacity",
		"active.status IN ('CLAIMED','SUBMITTING')",
		"ss.in_flight_limit",
		"FOR UPDATE OF ss SKIP LOCKED",
	}
	for _, fragment := range required {
		if !strings.Contains(postgresAllocationQuery, fragment) {
			t.Fatalf("allocation query missing %q", fragment)
		}
	}
}

func TestAssignmentPrefersHealthAndAvailableCapacity(t *testing.T) {
	now := time.Now().UTC()
	allocator := NewMemoryAllocator(
		Session{ID: "busy-high", Pool: "pool", Status: StatusBusy, NodeReady: true, LeaseExpiresAt: now.Add(time.Hour), SafeMessagesPerMinute: 100, SafeDailyCapacity: 1000, SentToday: 900, InFlight: 8, InFlightLimit: 10, RecentFailureRate: 0.2, LastSuccessfulAt: now.Add(-time.Minute)},
		Session{ID: "ready-low", Pool: "pool", Status: StatusReady, NodeReady: true, LeaseExpiresAt: now.Add(time.Hour), SafeMessagesPerMinute: 100, SafeDailyCapacity: 1000, SentToday: 100, InFlight: 1, InFlightLimit: 10, RecentFailureRate: 0.01, LastSuccessfulAt: now},
	)
	assigned, err := allocator.Assign(context.Background(), "recipient-health", AllocationRoute{LegacyPool: "pool"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if assigned != "ready-low" {
		t.Fatalf("assigned=%s", assigned)
	}
}

func TestAssignmentExcludesExhaustedAndInflightLimitedSessions(t *testing.T) {
	now := time.Now().UTC()
	allocator := NewMemoryAllocator(
		Session{ID: "daily-exhausted", Pool: "pool", Status: StatusReady, NodeReady: true, LeaseExpiresAt: now.Add(time.Hour), SafeMessagesPerMinute: 100, SafeDailyCapacity: 1000, SentToday: 1000},
		Session{ID: "inflight-exhausted", Pool: "pool", Status: StatusReady, NodeReady: true, LeaseExpiresAt: now.Add(time.Hour), SafeMessagesPerMinute: 100, SafeDailyCapacity: 1000, InFlight: 10, InFlightLimit: 10},
	)
	if _, err := allocator.Assign(context.Background(), "recipient-full", AllocationRoute{LegacyPool: "pool"}, now); err != ErrNoHealthySession {
		t.Fatalf("err=%v", err)
	}
}

func TestExistingAssignmentRejectsRouteDrift(t *testing.T) {
	now := time.Now().UTC()
	allocator := NewMemoryAllocator(Session{ID: "session-a", PoolID: "pool-a", GatewayPoolID: "gateway-a", Status: StatusReady, NodeReady: true, LeaseExpiresAt: now.Add(time.Hour), SafeMessagesPerMinute: 10, SafeDailyCapacity: 100, InFlightLimit: 10})
	if _, err := allocator.Assign(context.Background(), "recipient-route", AllocationRoute{SenderPoolID: "pool-a", GatewayPoolID: "gateway-a"}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := allocator.Assign(context.Background(), "recipient-route", AllocationRoute{SenderPoolID: "pool-b", GatewayPoolID: "gateway-b"}, now); err != ErrAssignmentConflict {
		t.Fatalf("expected route conflict, got %v", err)
	}
}

func TestExistingAssignmentRequiresCurrentSessionHealth(t *testing.T) {
	now := time.Now().UTC()
	allocator := NewMemoryAllocator(Session{ID: "session-a", PoolID: "pool-a", GatewayPoolID: "gateway-a", Status: StatusReady, NodeReady: true, LeaseExpiresAt: now.Add(time.Hour), SafeMessagesPerMinute: 10, SafeDailyCapacity: 100, InFlightLimit: 10})
	route := AllocationRoute{SenderPoolID: "pool-a", GatewayPoolID: "gateway-a"}
	if _, err := allocator.Assign(context.Background(), "recipient-health-recheck", route, now); err != nil {
		t.Fatal(err)
	}
	allocator.mu.Lock()
	value := allocator.sessions["session-a"]
	value.LeaseExpiresAt = now.Add(-time.Second)
	allocator.sessions["session-a"] = value
	allocator.mu.Unlock()
	if _, err := allocator.Assign(context.Background(), "recipient-health-recheck", route, now); err != ErrNoHealthySession {
		t.Fatalf("expected unhealthy assignment rejection, got %v", err)
	}
}

func TestPostgresExistingAssignmentValidationIsRouteAndLeaseAware(t *testing.T) {
	required := []string{
		"ss.sender_pool_id=nullif($2,'')::uuid",
		"ss.logical_sender_pool=$3",
		"ss.gateway_pool_id=nullif($4,'')::uuid",
		"ss.id=nullif($5,'')::uuid",
		"ss.last_heartbeat_at>$7",
		"sn.last_heartbeat_at>$7",
		"sender_session_leases",
		"sl.expires_at>$6",
		"FOR SHARE OF ss,sn",
	}
	for _, fragment := range required {
		if !strings.Contains(postgresAssignedSessionValidationQuery, fragment) {
			t.Fatalf("assigned-session validation query missing %q", fragment)
		}
	}
}
