package delivery

import (
	"context"
	"database/sql"
	"errors"
	"sync"
)

var ErrMetricsNotFound = errors.New("campaign metrics not found")

type MetricsRepository interface {
	Get(context.Context, string) (Metrics, error)
}

type MetricsService struct{ repository MetricsRepository }

func NewMetricsService(repository MetricsRepository) *MetricsService {
	return &MetricsService{repository: repository}
}
func (s *MetricsService) Get(ctx context.Context, campaignID string) (Metrics, error) {
	if s == nil || s.repository == nil {
		return Metrics{}, errors.New("metrics repository is required")
	}
	return s.repository.Get(ctx, campaignID)
}

type MemoryMetricsRepository struct {
	mu    sync.RWMutex
	items map[string]Metrics
}

func NewMemoryMetricsRepository() *MemoryMetricsRepository {
	return &MemoryMetricsRepository{items: make(map[string]Metrics)}
}
func (r *MemoryMetricsRepository) Get(_ context.Context, campaignID string) (Metrics, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.items[campaignID]
	if !ok {
		return Metrics{}, ErrMetricsNotFound
	}
	return value, nil
}
func (r *MemoryMetricsRepository) Set(campaignID string, value Metrics) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[campaignID] = value
}

type PostgreSQLMetricsRepository struct{ DB *sql.DB }

func (r *PostgreSQLMetricsRepository) Get(ctx context.Context, campaignID string) (Metrics, error) {
	if r == nil || r.DB == nil {
		return Metrics{}, errors.New("database is required")
	}
	var value Metrics
	err := r.DB.QueryRowContext(ctx, `SELECT authorised_total,queued_total,submitted_total,sent_total,delivered_total,read_total,failed_total,unknown_total,suppressed_total,excluded_final_check_total FROM campaign_metrics WHERE campaign_id=$1::uuid`, campaignID).Scan(
		&value.AuthorisedTotal, &value.QueuedTotal, &value.SubmittedTotal, &value.SentTotal, &value.DeliveredTotal, &value.ReadTotal, &value.FailedTotal, &value.UnknownTotal, &value.SuppressedTotal, &value.ExcludedFinalCheckTotal,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Metrics{}, ErrMetricsNotFound
	}
	return value, err
}

func (m Metrics) ActiveObligations() int64 {
	return m.AuthorisedTotal + m.QueuedTotal + m.SubmittedTotal
}
func (m Metrics) TotalAccounted() int64 {
	return m.AuthorisedTotal + m.QueuedTotal + m.SubmittedTotal + m.SentTotal + m.DeliveredTotal + m.ReadTotal + m.FailedTotal + m.UnknownTotal + m.SuppressedTotal + m.ExcludedFinalCheckTotal
}
