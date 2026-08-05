package operations

import (
	"context"
	"sync"
	"time"
)

type MemoryRepository struct {
	mu        sync.RWMutex
	incidents map[string]Incident
	exports   map[string]ExportRequest
	dashboard Dashboard
	reports   map[string]CampaignReport
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{incidents: map[string]Incident{}, exports: map[string]ExportRequest{}, reports: map[string]CampaignReport{}}
}
func (r *MemoryRepository) Dashboard(_ context.Context, now time.Time) (Dashboard, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d := r.dashboard
	d.GeneratedAt = now
	for _, i := range r.incidents {
		if i.Status != IncidentResolved {
			d.OpenIncidents++
			if i.Severity == SeverityCritical {
				d.CriticalIncidents++
			}
		}
	}
	return d, nil
}
func (r *MemoryRepository) ListIncidents(_ context.Context, status IncidentStatus, limit int) ([]Incident, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if limit <= 0 {
		limit = 100
	}
	out := []Incident{}
	for _, v := range r.incidents {
		if status == "" || v.Status == status {
			out = append(out, v)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
func (r *MemoryRepository) GetIncident(_ context.Context, id string) (Incident, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.incidents[id]
	if !ok {
		return Incident{}, ErrNotFound
	}
	return v, nil
}
func (r *MemoryRepository) CreateIncident(_ context.Context, v Incident) (Incident, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.incidents[v.ID] = v
	return v, nil
}
func (r *MemoryRepository) UpdateIncident(_ context.Context, v Incident, expected int64) (Incident, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.incidents[v.ID]
	if !ok {
		return Incident{}, ErrNotFound
	}
	if cur.Version != expected {
		return Incident{}, ErrConflict
	}
	v.Version = expected + 1
	r.incidents[v.ID] = v
	return v, nil
}
func (r *MemoryRepository) CampaignReport(_ context.Context, id string, now time.Time) (CampaignReport, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.reports[id]
	if !ok {
		return CampaignReport{}, ErrNotFound
	}
	v.GeneratedAt = now
	return v, nil
}
func (r *MemoryRepository) CreateExport(_ context.Context, v ExportRequest) (ExportRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.exports[v.ID] = v
	return v, nil
}
func (r *MemoryRepository) GetExport(_ context.Context, id string) (ExportRequest, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.exports[id]
	if !ok {
		return ExportRequest{}, ErrNotFound
	}
	return v, nil
}
func (r *MemoryRepository) UpdateExport(_ context.Context, v ExportRequest, expected int64) (ExportRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.exports[v.ID]
	if !ok {
		return ExportRequest{}, ErrNotFound
	}
	if cur.Version != expected {
		return ExportRequest{}, ErrConflict
	}
	v.Version = expected + 1
	r.exports[v.ID] = v
	return v, nil
}

func (r *MemoryRepository) ListExceptions(_ context.Context, _ string, _ int) ([]DeliveryException, error) {
	return []DeliveryException{}, nil
}
