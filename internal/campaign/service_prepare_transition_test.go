package campaign

import (
	"context"
	"testing"
	"time"
)

func TestServicePrepareTransitionDoesNotPersist(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC)
	entity := Campaign{
		ID:        "campaign-atomic-1",
		Status:    StatusScheduled,
		Version:   7,
		UpdatedAt: now.Add(-time.Minute),
	}
	repository := NewMemoryRepository()
	if err := repository.Create(ctx, entity); err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	service.clock = func() time.Time { return now }

	prepared, err := service.PrepareTransition(ctx, entity.ID, TransitionInput{
		Action:          ActionStartDispatch,
		ActorID:         "operator-atomic-1",
		ExpectedVersion: entity.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Status != StatusDispatching || prepared.Version != entity.Version+1 {
		t.Fatalf("unexpected prepared campaign: %+v", prepared)
	}
	persisted, err := repository.Get(ctx, entity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != StatusScheduled || persisted.Version != entity.Version {
		t.Fatalf("preparation persisted campaign state: %+v", persisted)
	}
}
