package sender

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAtomicHeartbeatReturnsBoundOwnershipReceipt(t *testing.T) {
	for name, fixture := range map[string]func(*testing.T) *atomicHeartbeatFixture{"memory": newAtomicMemoryFixture, "postgres": newAtomicPostgresFixture} {
		t.Run(name, func(t *testing.T) {
			f := fixture(t)
			response, err := f.send(t)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				Ownership *struct {
					NodeID         string    `json:"nodeId"`
					SessionID      string    `json:"sessionId"`
					BootID         string    `json:"bootId"`
					LeaseVersion   int64     `json:"leaseVersion"`
					LeaseExpiresAt time.Time `json:"leaseExpiresAt"`
					ServerNow      time.Time `json:"serverNow"`
				} `json:"ownership"`
			}
			if err = json.Unmarshal(raw, &wire); err != nil {
				t.Fatal(err)
			}
			if wire.Ownership == nil {
				t.Fatal("accepted heartbeat omitted current boot lease receipt")
			}
			r := wire.Ownership
			if r.NodeID != f.report.NodeID || r.SessionID != f.report.SessionID || r.BootID != f.report.BootID || r.LeaseVersion != 1 || !r.ServerNow.Equal(f.now) || !r.LeaseExpiresAt.Equal(f.now.Add(f.service.LeaseTTL)) {
				t.Fatalf("receipt is not bound to the committed lease: %+v", r)
			}
		})
	}
}
