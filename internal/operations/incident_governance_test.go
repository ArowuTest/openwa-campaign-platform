package operations

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestIncidentTimelineCapturesAssignmentAndTerminalState(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	alerts := NewMemoryAlertStore()
	svc := &Service{Repo: repo, Alerting: alerts, Clock: func() time.Time { return now }}

	incident, err := svc.CreateIncident(context.Background(), Incident{Category: "GATEWAY", Severity: SeverityCritical, Summary: "Gateway unavailable", Detail: "gateway pool lost quorum"}, "actor-a", "corr-a")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	incident, err = svc.UpdateIncident(context.Background(), incident.ID, incident.Version, IncidentInvestigating, "operator-b", "checking gateway nodes", "actor-b", "corr-b")
	if err != nil {
		t.Fatal(err)
	}
	if incident.OwnerID != "operator-b" || incident.Resolution != "" || incident.ResolvedAt != nil {
		t.Fatalf("bad investigating state: %#v", incident)
	}
	timeline, err := svc.IncidentTimeline(context.Background(), incident.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(timeline) != 3 || timeline[0].EventType != "CREATED" || timeline[1].EventType != "ASSIGNED" || timeline[2].EventType != "INVESTIGATION_NOTE" {
		t.Fatalf("unexpected timeline: %#v", timeline)
	}

	now = now.Add(time.Minute)
	incident, err = svc.UpdateIncident(context.Background(), incident.ID, incident.Version, IncidentResolved, "operator-b", "gateway quorum restored", "actor-b", "corr-c")
	if err != nil {
		t.Fatal(err)
	}
	if incident.ResolvedAt == nil || incident.Resolution == "" {
		t.Fatalf("missing terminal evidence: %#v", incident)
	}
	if _, err = svc.UpdateIncident(context.Background(), incident.ID, incident.Version, IncidentInvestigating, "operator-b", "reopen without a new incident", "actor-b", "corr-d"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("terminal incident reopened: %v", err)
	}
}

func TestIncidentRejectsNoOpAndTerminalWithoutResolution(t *testing.T) {
	repo := NewMemoryRepository()
	alerts := NewMemoryAlertStore()
	svc := &Service{Repo: repo, Alerting: alerts}
	incident, err := svc.CreateIncident(context.Background(), Incident{Category: "QUEUE", Severity: SeverityWarning, Summary: "Queue delay"}, "actor", "corr")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.UpdateIncident(context.Background(), incident.ID, incident.Version, IncidentOpen, "", "", "actor", "corr"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("no-op accepted: %v", err)
	}
	if _, err = svc.UpdateIncident(context.Background(), incident.ID, incident.Version, IncidentResolved, "operator", "", "actor", "corr"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("resolution-less terminal state accepted: %v", err)
	}
}
