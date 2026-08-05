package campaignworkspace

import (
	"context"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
)

type campaignReader struct{ value campaign.Campaign }

func (r campaignReader) Get(context.Context, string) (campaign.Campaign, error) { return r.value, nil }

type metricReader struct{ value DeliveryMetrics }

func (r metricReader) Metrics(context.Context, string) (DeliveryMetrics, error) {
	return r.value, nil
}

func TestWorkspaceTagsNotesAndArchive(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	c := campaign.Campaign{ID: "c1", Status: campaign.StatusCompleted}
	svc := &Service{Repository: NewMemoryRepository(), Campaigns: campaignReader{c}, Clock: func() time.Time { return now }}
	w, err := svc.Get(context.Background(), "c1")
	if err != nil || w.Archive.Version != 1 {
		t.Fatalf("get: %+v %v", w, err)
	}
	w, err = svc.SetTags(context.Background(), "c1", SetTagsInput{Tags: []string{" Priority ", "priority", "events"}, ActorID: "u1", Reason: "classify campaign", ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Tags) != 2 || w.Tags[0] != "events" || w.Archive.Version != 2 {
		t.Fatalf("tags: %+v", w)
	}
	note, err := svc.AddNote(context.Background(), "c1", AddNoteInput{Category: NoteCompliance, Body: "Consent evidence reviewed again.", ActorID: "u1"})
	if err != nil || note.Category != NoteCompliance {
		t.Fatalf("note: %+v %v", note, err)
	}
	w, err = svc.Archive(context.Background(), "c1", ArchiveInput{ActorID: "u2", Reason: "campaign retention lifecycle", ExpectedVersion: 2})
	if err != nil || !w.Archive.Archived {
		t.Fatalf("archive: %+v %v", w, err)
	}
	if _, err := svc.SetTags(context.Background(), "c1", SetTagsInput{Tags: []string{"xx"}, ActorID: "u1", Reason: "bad version", ExpectedVersion: 2}); err != ErrConflict {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestArchiveRejectsNonTerminalCampaign(t *testing.T) {
	svc := &Service{Repository: NewMemoryRepository(), Campaigns: campaignReader{campaign.Campaign{ID: "c1", Status: campaign.StatusScheduled}}}
	_, err := svc.Archive(context.Background(), "c1", ArchiveInput{ActorID: "u1", Reason: "too early", ExpectedVersion: 1})
	if err == nil {
		t.Fatal("expected archive rejection")
	}
}

func TestCancellationImpactPreservesUnknownAndCannotRecall(t *testing.T) {
	svc := &Service{Repository: NewMemoryRepository(), Campaigns: campaignReader{campaign.Campaign{ID: "c1", Status: campaign.StatusDispatching}}, Metrics: metricReader{DeliveryMetrics{Authorised: 10, Queued: 5, Submitted: 3, Delivered: 2, Unknown: 1}}}
	got, err := svc.CancellationImpact(context.Background(), "c1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.CanCancel || got.Outstanding != 15 || got.CannotRecall != 5 || !got.RequiresReconciliation {
		t.Fatalf("impact: %+v", got)
	}
}

func TestNormalizeTags(t *testing.T) {
	got, err := NormalizeTags([]string{"Alpha", "alpha", "beta-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "alpha" {
		t.Fatalf("tags=%v", got)
	}
	if _, err := NormalizeTags([]string{"x"}); err == nil {
		t.Fatal("expected invalid short tag")
	}
}
