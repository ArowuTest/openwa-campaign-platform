package sender

import (
	"context"
	"fmt"
	"testing"
	"time"

	"campaign-platform/internal/gateway"
)

type capacityLeaseFixture struct {
	*atomicHeartbeatFixture
	capacity  func(time.Time) (CapacitySummary, error)
	nodeState func(string, bool, time.Time) error
}

func memoryCapacityLeaseFixture(t *testing.T, ttl time.Duration) *capacityLeaseFixture {
	f := newAtomicMemoryFixture(t)
	hook := f.service.Governance.Store.(*atomicMemoryReadHook)
	store := hook.MemoryGovernanceStore
	store.HeartbeatTTL = ttl
	store.Leases = f.service.Leases
	store.mu.Lock()
	store.pools["capacity-pool"] = Pool{ID: "capacity-pool", Name: "capacity", Status: "ACTIVE", MaxMessagesPerMinute: 100, DailyCapacity: 1000}
	session := store.sessions[f.report.SessionID]
	session.PoolID = "capacity-pool"
	store.sessions[session.ID] = session
	node := store.nodes[f.report.NodeID]
	node.LastHeartbeatAt = &f.now
	store.nodes[node.ID] = node
	store.mu.Unlock()
	return &capacityLeaseFixture{atomicHeartbeatFixture: f, capacity: func(at time.Time) (CapacitySummary, error) {
		return store.Capacity(context.Background(), "capacity-pool", at)
	}, nodeState: func(status string, draining bool, at time.Time) error {
		store.mu.Lock()
		defer store.mu.Unlock()
		node := store.nodes[f.report.NodeID]
		node.Status = status
		node.Draining = draining
		node.LastHeartbeatAt = &at
		store.nodes[node.ID] = node
		return nil
	}}
}
func postgresCapacityLeaseFixture(t *testing.T, ttl time.Duration) *capacityLeaseFixture {
	f := newAtomicPostgresFixture(t)
	store := f.service.Governance.Store.(*atomicPostgresReadHook).PostgreSQLGovernanceStore
	store.HeartbeatTTL = ttl
	var pool string
	if err := store.DB.QueryRow(`SELECT gen_random_uuid()::text`).Scan(&pool); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.Exec(`INSERT INTO sender_pools(id,name,status,max_messages_per_minute,daily_capacity,reserved_capacity,version) VALUES($1::uuid,$2,'ACTIVE',100,1000,0,1)`, pool, "capacity-"+pool); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := store.DB.Exec(`UPDATE sender_sessions SET sender_pool_id=NULL WHERE id=$1::uuid`, f.report.SessionID); err != nil {
			t.Error(err)
		}
		if _, err := store.DB.Exec(`DELETE FROM sender_pools WHERE id=$1::uuid`, pool); err != nil {
			t.Error(err)
		}
	})
	if _, err := store.DB.Exec(`UPDATE sender_sessions SET sender_pool_id=$2::uuid WHERE id=$1::uuid`, f.report.SessionID, pool); err != nil {
		t.Fatal(err)
	}
	setNode := func(status string, draining bool, at time.Time) error {
		_, err := store.DB.Exec(`UPDATE sender_nodes SET status=$2,draining=$3,last_heartbeat_at=$4 WHERE id=$1::uuid`, f.report.NodeID, status, draining, at)
		return err
	}
	if err := setNode("READY", false, f.now); err != nil {
		t.Fatal(err)
	}
	return &capacityLeaseFixture{atomicHeartbeatFixture: f, capacity: func(at time.Time) (CapacitySummary, error) { return store.Capacity(context.Background(), pool, at) }, nodeState: setNode}
}
func runCapacityLeaseCases(t *testing.T, fixture func(*testing.T, time.Duration) *capacityLeaseFixture) {
	cases := []struct {
		name           string
		ttl, timeSince time.Duration
		change         string
		ready          int
	}{
		{"current owner", 90 * time.Second, time.Second, "", 1},
		{"missing lease", 90 * time.Second, time.Second, "missing", 0},
		{"expired lease", 90 * time.Second, 90 * time.Second, "", 0},
		{"replacement boot", 90 * time.Second, time.Second, "replacement", 0},
		{"unhealthy node", 90 * time.Second, time.Second, "unhealthy", 0},
		{"draining node", 90 * time.Second, time.Second, "draining", 0},
		{"short configured heartbeat TTL", 10 * time.Second, 11 * time.Second, "long-lease", 0},
		{"long configured heartbeat TTL", 120 * time.Second, 100 * time.Second, "long-lease", 1},
		{"stale node heartbeat", 90 * time.Second, time.Second, "stale-node", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := fixture(t, c.ttl)
			f.service.LeaseTTL = c.ttl
			if c.change == "long-lease" {
				f.service.LeaseTTL = 5 * time.Minute
			}
			if _, err := f.send(t); err != nil {
				t.Fatal(err)
			}
			switch c.change {
			case "missing":
				lease, _, err := f.service.Leases.Get(context.Background(), f.report.SessionID)
				if err != nil {
					t.Fatal(err)
				}
				lease.Token = f.report.BootID
				if err = f.service.Leases.Release(context.Background(), lease); err != nil {
					t.Fatal(err)
				}
			case "replacement":
				if err := f.replaceBoot("capacity-replacement"); err != nil {
					t.Fatal(err)
				}
			case "unhealthy":
				if err := f.nodeState("UNHEALTHY", false, f.now); err != nil {
					t.Fatal(err)
				}
			case "draining":
				if err := f.nodeState("READY", true, f.now); err != nil {
					t.Fatal(err)
				}
			case "stale-node":
				if err := f.nodeState("READY", false, f.now.Add(-2*time.Minute)); err != nil {
					t.Fatal(err)
				}
			}
			result, err := f.capacity(f.now.Add(c.timeSince))
			if err != nil {
				t.Fatal(err)
			}
			if result.ReadySessions != c.ready {
				t.Errorf("ready sessions=%d want%d: %+v", result.ReadySessions, c.ready, result)
			}
			if c.ready == 0 && (result.AvailableMessagesPerMinute != 0 || result.AvailableDailyCapacity != 0 || result.HealthyNodes != 0) {
				t.Errorf("unowned/unhealthy session contributed capacity: %+v", result)
			}
			if c.ready == 1 && (result.AvailableMessagesPerMinute <= 0 || result.AvailableDailyCapacity <= 0 || result.HealthyNodes != 1) {
				t.Errorf("current owner lost usable capacity: %s", fmt.Sprint(result))
			}
		})
	}
}
func TestMemoryCapacityRequiresCurrentLease(t *testing.T) {
	runCapacityLeaseCases(t, memoryCapacityLeaseFixture)
}
func TestPostgreSQLCapacityRequiresCurrentLease(t *testing.T) {
	runCapacityLeaseCases(t, postgresCapacityLeaseFixture)
}

var _ gateway.LeaseStore = (*gateway.MemoryLeaseStore)(nil)
