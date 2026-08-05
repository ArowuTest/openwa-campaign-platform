package campaignworkspace

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"campaign-platform/internal/campaign"
)

var (
	ErrNotFound = errors.New("campaign workspace not found")
	ErrConflict = errors.New("campaign workspace version conflict")
)

type Repository interface {
	Get(context.Context, string) (Workspace, error)
	Ensure(context.Context, string) error
	SetTags(context.Context, string, []string, string, string, int64, time.Time) (Workspace, error)
	AddNote(context.Context, Note) error
	Archive(context.Context, string, bool, string, string, int64, time.Time) (Workspace, error)
}

type CampaignReader interface {
	Get(context.Context, string) (campaign.Campaign, error)
}
type DeliveryMetrics struct {
	Authorised int64
	Queued     int64
	Pending    int64
	Submitted  int64
	Sent       int64
	Delivered  int64
	Read       int64
	Unknown    int64
}
type MetricsReader interface {
	Metrics(context.Context, string) (DeliveryMetrics, error)
}
type MetricsReaderFunc func(context.Context, string) (DeliveryMetrics, error)

func (f MetricsReaderFunc) Metrics(ctx context.Context, id string) (DeliveryMetrics, error) {
	return f(ctx, id)
}

type Service struct {
	Repository Repository
	Campaigns  CampaignReader
	Metrics    MetricsReader
	Clock      func() time.Time
}

func (s *Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}
func (s *Service) Get(ctx context.Context, id string) (Workspace, error) {
	if _, err := s.Campaigns.Get(ctx, id); err != nil {
		return Workspace{}, err
	}
	if err := s.Repository.Ensure(ctx, id); err != nil {
		return Workspace{}, err
	}
	return s.Repository.Get(ctx, id)
}
func (s *Service) SetTags(ctx context.Context, id string, in SetTagsInput) (Workspace, error) {
	if _, err := s.Campaigns.Get(ctx, id); err != nil {
		return Workspace{}, err
	}
	tags, err := NormalizeTags(in.Tags)
	if err != nil {
		return Workspace{}, err
	}
	if in.ActorID == "" || len(in.Reason) < 3 {
		return Workspace{}, errors.New("actor and meaningful reason are required")
	}
	if err := s.Repository.Ensure(ctx, id); err != nil {
		return Workspace{}, err
	}
	return s.Repository.SetTags(ctx, id, tags, in.ActorID, in.Reason, in.ExpectedVersion, s.now())
}
func (s *Service) AddNote(ctx context.Context, id string, in AddNoteInput) (Note, error) {
	if _, err := s.Campaigns.Get(ctx, id); err != nil {
		return Note{}, err
	}
	note, err := NewNote(id, in, s.now())
	if err != nil {
		return Note{}, err
	}
	if err := s.Repository.Ensure(ctx, id); err != nil {
		return Note{}, err
	}
	if err := s.Repository.AddNote(ctx, note); err != nil {
		return Note{}, err
	}
	return note, nil
}
func (s *Service) Archive(ctx context.Context, id string, in ArchiveInput) (Workspace, error) {
	return s.setArchive(ctx, id, true, in)
}
func (s *Service) Restore(ctx context.Context, id string, in ArchiveInput) (Workspace, error) {
	return s.setArchive(ctx, id, false, in)
}
func (s *Service) setArchive(ctx context.Context, id string, archived bool, in ArchiveInput) (Workspace, error) {
	entity, err := s.Campaigns.Get(ctx, id)
	if err != nil {
		return Workspace{}, err
	}
	if archived && !CanArchive(entity.Status) {
		return Workspace{}, errors.New("only terminal campaigns can be archived")
	}
	if in.ActorID == "" || len(in.Reason) < 3 {
		return Workspace{}, errors.New("actor and meaningful reason are required")
	}
	if err := s.Repository.Ensure(ctx, id); err != nil {
		return Workspace{}, err
	}
	return s.Repository.Archive(ctx, id, archived, in.ActorID, in.Reason, in.ExpectedVersion, s.now())
}

type CancellationImpact struct {
	CampaignID                string          `json:"campaignId"`
	Status                    campaign.Status `json:"status"`
	CanCancel                 bool            `json:"canCancel"`
	Outstanding               int64           `json:"outstanding"`
	SubmittedOrLater          int64           `json:"submittedOrLater"`
	Unknown                   int64           `json:"unknown"`
	CannotRecall              int64           `json:"cannotRecall"`
	RequiresReconciliation    bool            `json:"requiresReconciliation"`
	ReservationsShouldRelease bool            `json:"reservationsShouldRelease"`
	Warnings                  []string        `json:"warnings"`
	AssessedAt                time.Time       `json:"assessedAt"`
}

func (s *Service) CancellationImpact(ctx context.Context, id string) (CancellationImpact, error) {
	entity, err := s.Campaigns.Get(ctx, id)
	if err != nil {
		return CancellationImpact{}, err
	}
	out := CancellationImpact{CampaignID: id, Status: entity.Status, CanCancel: entity.Status != campaign.StatusCompleted && entity.Status != campaign.StatusCompletedWithExceptions && entity.Status != campaign.StatusCancelled, Warnings: []string{}, AssessedAt: s.now()}
	if s.Metrics != nil {
		m, err := s.Metrics.Metrics(ctx, id)
		if err != nil {
			return CancellationImpact{}, err
		}
		out.Outstanding = m.Pending + m.Authorised + m.Queued
		out.SubmittedOrLater = m.Submitted + m.Sent + m.Delivered + m.Read
		out.CannotRecall = out.SubmittedOrLater
		out.Unknown = m.Unknown
		out.RequiresReconciliation = m.Unknown > 0
	}
	out.ReservationsShouldRelease = out.CanCancel
	if out.CannotRecall > 0 {
		out.Warnings = append(out.Warnings, "MESSAGES_ALREADY_SUBMITTED_CANNOT_BE_RECALLED")
	}
	if out.Unknown > 0 {
		out.Warnings = append(out.Warnings, "UNKNOWN_OUTCOMES_MUST_BE_RECONCILED_BEFORE_ANY_RESEND")
	}
	if !out.CanCancel {
		out.Warnings = append(out.Warnings, "CAMPAIGN_ALREADY_TERMINAL")
	}
	return out, nil
}

type MemoryRepository struct {
	mu    sync.RWMutex
	items map[string]Workspace
}

func NewMemoryRepository() *MemoryRepository { return &MemoryRepository{items: map[string]Workspace{}} }
func (r *MemoryRepository) Ensure(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[id]; !ok {
		r.items[id] = Workspace{CampaignID: id, Tags: []string{}, Archive: ArchiveRecord{CampaignID: id, Version: 1}, Notes: []Note{}}
	}
	return nil
}
func (r *MemoryRepository) Get(_ context.Context, id string) (Workspace, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.items[id]
	if !ok {
		return Workspace{}, ErrNotFound
	}
	v.Tags = append([]string(nil), v.Tags...)
	v.Notes = append([]Note(nil), v.Notes...)
	return v, nil
}
func (r *MemoryRepository) SetTags(_ context.Context, id string, tags []string, actor, reason string, expected int64, now time.Time) (Workspace, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.items[id]
	if !ok {
		return Workspace{}, ErrNotFound
	}
	if v.Archive.Version != expected {
		return Workspace{}, ErrConflict
	}
	v.Tags = append([]string(nil), tags...)
	v.Archive.Version++
	r.items[id] = v
	return v, nil
}
func (r *MemoryRepository) AddNote(_ context.Context, n Note) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.items[n.CampaignID]
	if !ok {
		return ErrNotFound
	}
	v.Notes = append(v.Notes, n)
	sort.Slice(v.Notes, func(i, j int) bool { return v.Notes[i].CreatedAt.After(v.Notes[j].CreatedAt) })
	r.items[n.CampaignID] = v
	return nil
}
func (r *MemoryRepository) Archive(_ context.Context, id string, archived bool, actor, reason string, expected int64, now time.Time) (Workspace, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.items[id]
	if !ok {
		return Workspace{}, ErrNotFound
	}
	if v.Archive.Version != expected {
		return Workspace{}, ErrConflict
	}
	v.Archive.Archived = archived
	v.Archive.Reason = reason
	v.Archive.Version++
	if archived {
		t := now.UTC()
		v.Archive.ArchivedAt = &t
		v.Archive.ArchivedBy = actor
	} else {
		v.Archive.ArchivedAt = nil
		v.Archive.ArchivedBy = ""
	}
	r.items[id] = v
	return v, nil
}
