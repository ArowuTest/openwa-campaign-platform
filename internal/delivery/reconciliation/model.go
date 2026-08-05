package reconciliation

import (
	"context"
	"errors"
	"time"

	"campaign-platform/internal/delivery"
)

var ErrLeaseConflict = errors.New("metric reconciliation lease conflict")

type Lease struct {
	Owner     string
	Version   int64
	ExpiresAt time.Time
}

type Work struct {
	CampaignID string
	Lease      Lease
}

type Observation struct {
	CampaignID     string
	Canonical      delivery.Metrics
	Stored         delivery.Metrics
	Matched        bool
	ObservedAt     time.Time
	CanonicalTotal int64
	StoredTotal    int64
}

type Repository interface {
	Claim(context.Context, string, time.Time, time.Duration, int) ([]Work, error)
	Renew(context.Context, Work, time.Time, time.Duration) error
	Record(context.Context, Work, Observation, time.Time, time.Duration) error
	Fail(context.Context, Work, time.Time, error, time.Duration) error
}

type Calculator interface {
	Observe(context.Context, string, time.Time) (Observation, error)
}
