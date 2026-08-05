package delivery

import "testing"

func TestDeltaMovesOneRecipientBetweenTruthfulBuckets(t *testing.T) {
	delta := Delta(StatusSent, StatusDelivered)
	if delta.SentTotal != -1 || delta.DeliveredTotal != 1 {
		t.Fatalf("unexpected delta: %#v", delta)
	}
	if unchanged := Delta(StatusDelivered, StatusDelivered); unchanged != (Metrics{}) {
		t.Fatalf("same-state event must not move metrics: %#v", unchanged)
	}
}
