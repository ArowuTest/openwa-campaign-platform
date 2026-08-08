package operations

import (
	"context"
	"errors"
	"testing"
	"time"
)

type operationsPageRepository struct {
	*MemoryRepository
	incidentItems  []Incident
	exceptionItems []DeliveryException
}

func (r *operationsPageRepository) ListIncidentPage(_ context.Context, status IncidentStatus, limit int, afterSeverity int, before *time.Time, beforeID string) ([]Incident, error) {
	items := make([]Incident, 0)
	for _, item := range r.incidentItems {
		if status != "" && item.Status != status {
			continue
		}
		rank := incidentSeverityRank(item.Severity)
		if before != nil && !(rank > afterSeverity || (rank == afterSeverity && (item.CreatedAt.Before(*before) || (item.CreatedAt.Equal(*before) && item.ID < beforeID)))) {
			continue
		}
		items = append(items, item)
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *operationsPageRepository) ListExceptionPage(_ context.Context, campaignID string, limit int, before *time.Time, beforeID string) ([]DeliveryException, error) {
	items := make([]DeliveryException, 0)
	for _, item := range r.exceptionItems {
		if campaignID != "" && item.CampaignID != campaignID {
			continue
		}
		if before != nil && !(item.UpdatedAt.Before(*before) || (item.UpdatedAt.Equal(*before) && item.RecipientID < beforeID)) {
			continue
		}
		items = append(items, item)
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func TestIncidentPagePreservesSeverityOrderingAndContinues(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo := &operationsPageRepository{MemoryRepository: NewMemoryRepository(), incidentItems: []Incident{
		{ID: "critical-new", Severity: SeverityCritical, Status: IncidentOpen, CreatedAt: base.Add(3 * time.Minute)},
		{ID: "critical-old", Severity: SeverityCritical, Status: IncidentOpen, CreatedAt: base.Add(2 * time.Minute)},
		{ID: "warning-new", Severity: SeverityWarning, Status: IncidentOpen, CreatedAt: base.Add(time.Minute)},
	}}
	service := &Service{Repo: repo}
	first, err := service.ListIncidentsPage(context.Background(), "", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != "critical-new" || first.Items[1].ID != "critical-old" {
		t.Fatalf("unexpected first incident page: %#v", first)
	}
	second, err := service.ListIncidentsPage(context.Background(), "", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != "warning-new" {
		t.Fatalf("unexpected second incident page: %#v", second)
	}
	if _, err := service.ListIncidentsPage(context.Background(), "", 2, "invalid"); !errors.Is(err, ErrInvalidIncidentCursor) {
		t.Fatalf("invalid incident cursor accepted: %v", err)
	}
}

func TestDeliveryExceptionPageContinuesWithoutDuplicates(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo := &operationsPageRepository{MemoryRepository: NewMemoryRepository(), exceptionItems: []DeliveryException{
		{RecipientID: "recipient-c", CampaignID: "campaign-a", UpdatedAt: base.Add(2 * time.Minute)},
		{RecipientID: "recipient-b", CampaignID: "campaign-a", UpdatedAt: base.Add(time.Minute)},
		{RecipientID: "recipient-a", CampaignID: "campaign-a", UpdatedAt: base},
	}}
	service := &Service{Repo: repo}
	first, err := service.ListExceptionsPage(context.Background(), "campaign-a", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].RecipientID != "recipient-c" || first.Items[1].RecipientID != "recipient-b" {
		t.Fatalf("unexpected first exception page: %#v", first)
	}
	second, err := service.ListExceptionsPage(context.Background(), "campaign-a", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].RecipientID != "recipient-a" {
		t.Fatalf("unexpected second exception page: %#v", second)
	}
	if _, err := service.ListExceptionsPage(context.Background(), "campaign-a", 2, "invalid"); !errors.Is(err, ErrInvalidDeliveryExceptionCursor) {
		t.Fatalf("invalid exception cursor accepted: %v", err)
	}
}
