package operations

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

type MemoryRepository struct {
	mu                  sync.RWMutex
	incidents           map[string]Incident
	exports             map[string]ExportRequest
	downloadGrants      map[string]DownloadGrant
	dashboard           Dashboard
	reports             map[string]CampaignReport
	financial           map[string]CampaignFinancialReconciliation
	organisationReports map[string]OrganisationPerformanceReport
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{incidents: map[string]Incident{}, exports: map[string]ExportRequest{}, downloadGrants: map[string]DownloadGrant{}, reports: map[string]CampaignReport{}, financial: map[string]CampaignFinancialReconciliation{}, organisationReports: map[string]OrganisationPerformanceReport{}}
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

func (r *MemoryRepository) CampaignFinancialReconciliation(_ context.Context, id string, now time.Time) (CampaignFinancialReconciliation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.financial[id]
	if !ok {
		return CampaignFinancialReconciliation{}, ErrNotFound
	}
	v.GeneratedAt = now
	return v, nil
}

func (r *MemoryRepository) OrganisationPerformanceReport(_ context.Context, id string, now time.Time) (OrganisationPerformanceReport, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.organisationReports[id]
	if !ok {
		return OrganisationPerformanceReport{}, ErrNotFound
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
func (r *MemoryRepository) ListExports(_ context.Context, query ExportQuery) (ExportPage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	limit := query.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	items := make([]ExportRequest, 0, len(r.exports))
	for _, item := range r.exports {
		if query.Status != "" && item.Status != query.Status {
			continue
		}
		if value := strings.TrimSpace(query.Kind); value != "" && item.Kind != strings.ToUpper(value) {
			continue
		}
		if value := strings.TrimSpace(query.RequestedBy); value != "" && item.RequestedBy != value {
			continue
		}
		if query.AfterCreatedAt != nil {
			if item.CreatedAt.After(query.AfterCreatedAt.UTC()) {
				continue
			}
			if item.CreatedAt.Equal(query.AfterCreatedAt.UTC()) && item.ID >= query.AfterID {
				continue
			}
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	page := ExportPage{}
	if len(items) > limit {
		page.Items = append([]ExportRequest(nil), items[:limit]...)
		last := page.Items[len(page.Items)-1]
		page.NextAfter = last.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + last.ID
	} else {
		page.Items = append([]ExportRequest(nil), items...)
	}
	return page, nil
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

func (r *MemoryRepository) CreateDownloadGrant(_ context.Context, grant DownloadGrant) (DownloadGrant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.exports[grant.ExportID]; !ok {
		return DownloadGrant{}, ErrNotFound
	}
	for _, existing := range r.downloadGrants {
		if existing.TokenHash == grant.TokenHash {
			return DownloadGrant{}, ErrConflict
		}
	}
	r.downloadGrants[grant.ID] = grant
	return grant, nil
}

func (r *MemoryRepository) ConsumeDownloadGrant(_ context.Context, exportID, tokenHash, actorID string, now time.Time) (ExportRequest, DownloadGrant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	export, ok := r.exports[exportID]
	if !ok {
		return ExportRequest{}, DownloadGrant{}, ErrNotFound
	}
	if export.Status != ExportReady || export.RevokedAt != nil || export.ExpiresAt == nil || !export.ExpiresAt.After(now) {
		return ExportRequest{}, DownloadGrant{}, ErrConflict
	}
	for id, grant := range r.downloadGrants {
		if grant.ExportID != exportID || grant.TokenHash != tokenHash || grant.ActorID != actorID {
			continue
		}
		if grant.UsedAt != nil || grant.RevokedAt != nil || !grant.ExpiresAt.After(now) {
			return ExportRequest{}, DownloadGrant{}, ErrConflict
		}
		used := now.UTC()
		grant.UsedAt = &used
		r.downloadGrants[id] = grant
		export.DownloadCount++
		export.LastDownloadedAt = &used
		export.UpdatedAt = used
		export.Version++
		r.exports[exportID] = export
		return export, grant, nil
	}
	return ExportRequest{}, DownloadGrant{}, ErrNotFound
}

func (r *MemoryRepository) RevokeDownloadGrants(_ context.Context, exportID string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, grant := range r.downloadGrants {
		if grant.ExportID == exportID && grant.UsedAt == nil && grant.RevokedAt == nil {
			revoked := now.UTC()
			grant.RevokedAt = &revoked
			r.downloadGrants[id] = grant
		}
	}
	return nil
}
