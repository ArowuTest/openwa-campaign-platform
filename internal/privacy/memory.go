package privacy

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"
)

type MemoryRepository struct {
	mu         sync.RWMutex
	cases      map[string]Case
	events     map[string][]Event
	holds      map[string]LegalHold
	holdEvents map[string][]LegalHoldEvent
	subjects   map[string]SubjectPackage
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{cases: map[string]Case{}, events: map[string][]Event{}, holds: map[string]LegalHold{}, holdEvents: map[string][]LegalHoldEvent{}, subjects: map[string]SubjectPackage{}}
}

func (r *MemoryRepository) SeedSubject(lookup []byte, subject SubjectPackage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subjects[hex.EncodeToString(lookup)] = subject
}

func (r *MemoryRepository) Create(_ context.Context, item Case, event Event) (Case, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.cases[item.ID]; exists {
		return Case{}, ErrConflict
	}
	if subject, ok := r.subjects[hex.EncodeToString(item.SubjectLookupHMAC)]; ok {
		item.ContactID = subject.ContactID
	}
	r.cases[item.ID] = cloneCase(item)
	r.events[item.ID] = append(r.events[item.ID], event)
	return cloneCase(item), nil
}

func (r *MemoryRepository) Get(_ context.Context, id string) (Case, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, ok := r.cases[id]
	if !ok {
		return Case{}, ErrNotFound
	}
	return cloneCase(item), nil
}

func (r *MemoryRepository) List(_ context.Context, query Query) (Page, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	limit := query.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	items := make([]Case, 0, len(r.cases))
	for _, item := range r.cases {
		if query.Status != "" && item.Status != query.Status {
			continue
		}
		if query.Type != "" && item.Type != query.Type {
			continue
		}
		if value := strings.TrimSpace(query.AssignedTo); value != "" && item.AssignedTo != value {
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
		items = append(items, cloneCase(item))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	page := Page{}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextAfter = last.CreatedAt.Format(time.RFC3339Nano) + "|" + last.ID
	} else {
		page.Items = items
	}
	return page, nil
}

func (r *MemoryRepository) ListEvents(_ context.Context, id string, limit int) ([]Event, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.cases[id]; !ok {
		return nil, ErrNotFound
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	items := r.events[id]
	if len(items) > limit {
		items = items[len(items)-limit:]
	}
	out := make([]Event, len(items))
	copy(out, items)
	return out, nil
}

func (r *MemoryRepository) AppendEvent(_ context.Context, event Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.cases[event.CaseID]; !ok {
		return ErrNotFound
	}
	r.events[event.CaseID] = append(r.events[event.CaseID], event)
	return nil
}

func (r *MemoryRepository) update(id string, expected int64, mutate func(*Case) error, event Event) (Case, error) {
	item, ok := r.cases[id]
	if !ok {
		return Case{}, ErrNotFound
	}
	if item.Version != expected {
		return Case{}, ErrConflict
	}
	if err := mutate(&item); err != nil {
		return Case{}, err
	}
	item.Version++
	r.cases[id] = cloneCase(item)
	event.CaseVersion = item.Version
	r.events[id] = append(r.events[id], event)
	return cloneCase(item), nil
}

func (r *MemoryRepository) Assign(_ context.Context, id, assignee, reason string, expected int64, now time.Time, event Event) (Case, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.update(id, expected, func(c *Case) error {
		if c.Status != StatusOpen && c.Status != StatusAssigned {
			return ErrConflict
		}
		c.Status = StatusAssigned
		c.AssignedTo = assignee
		c.UpdatedAt = now
		return nil
	}, event)
}
func (r *MemoryRepository) Submit(_ context.Context, id, actor string, expected int64, now time.Time, event Event) (Case, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.update(id, expected, func(c *Case) error {
		if c.Status != StatusOpen && c.Status != StatusAssigned {
			return ErrConflict
		}
		c.Status = StatusPendingApproval
		c.SubmittedBy = actor
		c.UpdatedAt = now
		return nil
	}, event)
}
func (r *MemoryRepository) Decide(_ context.Context, id string, approve bool, reason, actor string, expected int64, now time.Time, event Event) (Case, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.update(id, expected, func(c *Case) error {
		if c.Status != StatusPendingApproval {
			return ErrConflict
		}
		c.DecidedBy = actor
		c.DecisionReason = reason
		c.UpdatedAt = now
		if approve {
			c.Status = StatusApproved
		} else {
			c.Status = StatusRejected
			t := now
			c.RejectedAt = &t
		}
		return nil
	}, event)
}

func (r *MemoryRepository) LoadSubjectPackage(_ context.Context, lookup []byte, now time.Time) (SubjectPackage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.subjects[hex.EncodeToString(lookup)]
	if !ok {
		return SubjectPackage{}, ErrNotFound
	}
	v.GeneratedAt = now
	return cloneSubject(v), nil
}

func (r *MemoryRepository) Execute(_ context.Context, current Case, actor, reason string, cipher []byte, keyVersion, checksum string, now time.Time, event Event) (Case, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.update(current.ID, current.Version, func(c *Case) error {
		if c.Status != StatusApproved {
			return ErrConflict
		}
		key := hex.EncodeToString(c.SubjectLookupHMAC)
		subject, ok := r.subjects[key]
		if !ok {
			return ErrNotFound
		}
		switch c.Type {
		case CaseRectification:
			if err := ValidateRequestedChanges(c.Type, c.RequestedChanges); err != nil {
				return err
			}
			var changes map[string]any
			if err := jsonUnmarshal(c.RequestedChanges, &changes); err != nil {
				return err
			}
			if subject.Profile == nil {
				subject.Profile = map[string]any{}
			}
			for k, v := range changes {
				subject.Profile[k] = v
			}
		case CaseErasure:
			subject.EncryptedMSISDN = []byte("anonymised")
			subject.MaskedMSISDN = "ANONYMISED"
			subject.Status = "ANONYMISED"
			subject.Profile = map[string]any{}
			subject.Attributes = nil
		case CaseObjection:
			subject.Status = "SUPPRESSED"
		case CaseRestriction:
			subject.ProcessingRestricted = true
		}
		r.subjects[key] = subject
		c.Status = StatusCompleted
		c.ExecutedBy = actor
		c.ExecutionReason = reason
		c.ResultCiphertext = append([]byte(nil), cipher...)
		c.ResultKeyVersion = keyVersion
		c.ResultSHA256 = checksum
		t := now
		c.CompletedAt = &t
		c.UpdatedAt = now
		return nil
	}, event)
}

func (r *MemoryRepository) CreateLegalHold(_ context.Context, hold LegalHold, event LegalHoldEvent) (LegalHold, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.holds[hold.ID]; exists {
		return LegalHold{}, ErrConflict
	}
	r.holds[hold.ID] = hold
	r.holdEvents[hold.ID] = append(r.holdEvents[hold.ID], event)
	return hold, nil
}

func (r *MemoryRepository) SubmitLegalHold(_ context.Context, id, actor string, expected int64, now time.Time, event LegalHoldEvent) (LegalHold, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.holds[id]
	if !ok {
		return LegalHold{}, ErrNotFound
	}
	if h.Version != expected || h.Status != HoldDraft {
		return LegalHold{}, ErrConflict
	}
	h.Status = HoldPendingApproval
	h.SubmittedBy = actor
	h.Version++
	r.holds[id] = h
	event.HoldVersion = h.Version
	event.OccurredAt = now.UTC()
	r.holdEvents[id] = append(r.holdEvents[id], event)
	return h, nil
}

func (r *MemoryRepository) DecideLegalHold(_ context.Context, id string, approve bool, reason, actor string, expected int64, now time.Time, event LegalHoldEvent) (LegalHold, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.holds[id]
	if !ok {
		return LegalHold{}, ErrNotFound
	}
	if h.Version != expected || h.Status != HoldPendingApproval || h.CreatedBy == actor || h.SubmittedBy == actor {
		return LegalHold{}, ErrConflict
	}
	h.DecidedBy = actor
	h.DecisionReason = reason
	h.Version++
	if approve {
		h.Status = HoldActive
		t := now.UTC()
		h.ActivatedAt = &t
	} else {
		h.Status = HoldRejected
		t := now.UTC()
		h.RejectedAt = &t
	}
	r.holds[id] = h
	event.HoldVersion = h.Version
	event.OccurredAt = now.UTC()
	r.holdEvents[id] = append(r.holdEvents[id], event)
	return h, nil
}

func (r *MemoryRepository) ListLegalHoldEvents(_ context.Context, id string, limit int) ([]LegalHoldEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, exists := r.holds[id]; !exists {
		return nil, ErrNotFound
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	source := r.holdEvents[id]
	out := make([]LegalHoldEvent, 0, min(limit, len(source)))
	for i := len(source) - 1; i >= 0 && len(out) < limit; i-- {
		item := source[i]
		item.Evidence = append([]byte(nil), item.Evidence...)
		out = append(out, item)
	}
	return out, nil
}

func (r *MemoryRepository) ListLegalHolds(_ context.Context, lookup []byte, activeOnly bool, limit int) ([]LegalHold, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	key := hex.EncodeToString(lookup)
	out := []LegalHold{}
	for _, h := range r.holds {
		if hex.EncodeToString(h.SubjectLookupHMAC) != key {
			continue
		}
		if activeOnly && (h.Status != HoldActive || (h.ExpiresAt != nil && !h.ExpiresAt.After(time.Now().UTC()))) {
			continue
		}
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *MemoryRepository) ListLegalHoldPage(_ context.Context, lookup []byte, activeOnly bool, limit int, before *time.Time, beforeID string) ([]LegalHold, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	key := hex.EncodeToString(lookup)
	out := make([]LegalHold, 0, len(r.holds))
	now := time.Now().UTC()
	for _, h := range r.holds {
		if hex.EncodeToString(h.SubjectLookupHMAC) != key {
			continue
		}
		if activeOnly && (h.Status != HoldActive || h.ReleasedAt != nil || (h.ExpiresAt != nil && !h.ExpiresAt.After(now))) {
			continue
		}
		if before != nil && !(h.CreatedAt.Before(*before) || (h.CreatedAt.Equal(*before) && h.ID < beforeID)) {
			continue
		}
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit <= 0 || limit > len(out) {
		limit = len(out)
	}
	return out[:limit], nil
}

func (r *MemoryRepository) ReleaseLegalHold(_ context.Context, id, reason, actor string, expected int64, now time.Time, event LegalHoldEvent) (LegalHold, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.holds[id]
	if !ok {
		return LegalHold{}, ErrNotFound
	}
	if h.Version != expected || h.Status != HoldActive || h.ReleasedAt != nil {
		return LegalHold{}, ErrConflict
	}
	h.Version++
	h.Status = HoldReleased
	h.ReleaseReason = reason
	h.ReleasedBy = actor
	t := now.UTC()
	h.ReleasedAt = &t
	r.holds[id] = h
	event.HoldVersion = h.Version
	event.OccurredAt = t
	r.holdEvents[id] = append(r.holdEvents[id], event)
	return h, nil
}

func (r *MemoryRepository) HasActiveLegalHold(_ context.Context, lookup []byte, scope string, now time.Time) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	key := hex.EncodeToString(lookup)
	for _, h := range r.holds {
		if hex.EncodeToString(h.SubjectLookupHMAC) != key || h.Status != HoldActive || h.ReleasedAt != nil || (h.ExpiresAt != nil && !h.ExpiresAt.After(now)) {
			continue
		}
		if h.Scope == "ALL" || h.Scope == scope {
			return true, nil
		}
	}
	return false, nil
}

func cloneCase(v Case) Case {
	v.SubjectLookupHMAC = append([]byte(nil), v.SubjectLookupHMAC...)
	v.RequestedChanges = append([]byte(nil), v.RequestedChanges...)
	v.ResultCiphertext = append([]byte(nil), v.ResultCiphertext...)
	return v
}
func cloneSubject(v SubjectPackage) SubjectPackage {
	v.EncryptedMSISDN = append([]byte(nil), v.EncryptedMSISDN...)
	if v.Profile != nil {
		profile := make(map[string]any, len(v.Profile))
		for key, value := range v.Profile {
			profile[key] = value
		}
		v.Profile = profile
	}
	v.Attributes = cloneRecordSlice(v.Attributes)
	v.Consents = cloneRecordSlice(v.Consents)
	v.Suppressions = cloneRecordSlice(v.Suppressions)
	v.CampaignHistory = cloneRecordSlice(v.CampaignHistory)
	return v
}

func cloneRecordSlice(items []map[string]any) []map[string]any {
	if items == nil {
		return nil
	}
	out := make([]map[string]any, len(items))
	for i, item := range items {
		copyItem := make(map[string]any, len(item))
		for key, value := range item {
			copyItem[key] = value
		}
		out[i] = copyItem
	}
	return out
}
func jsonUnmarshal(raw []byte, dst any) error { return json.Unmarshal(raw, dst) }

func (r *MemoryRepository) ListEventPage(_ context.Context, id string, limit int, before *time.Time, beforeID string) ([]Event, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.cases[id]; !ok {
		return nil, ErrNotFound
	}
	items := append([]Event(nil), r.events[id]...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].OccurredAt.Equal(items[j].OccurredAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].OccurredAt.After(items[j].OccurredAt)
	})
	out := make([]Event, 0, limit)
	for _, item := range items {
		if before != nil && (item.OccurredAt.After(*before) || item.OccurredAt.Equal(*before) && item.ID >= beforeID) {
			continue
		}
		out = append(out, item)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}
func (r *MemoryRepository) ListLegalHoldEventPage(_ context.Context, id string, limit int, before *time.Time, beforeID string) ([]LegalHoldEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.holds[id]; !ok {
		return nil, ErrNotFound
	}
	items := append([]LegalHoldEvent(nil), r.holdEvents[id]...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].OccurredAt.Equal(items[j].OccurredAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].OccurredAt.After(items[j].OccurredAt)
	})
	out := make([]LegalHoldEvent, 0, limit)
	for _, item := range items {
		if before != nil && (item.OccurredAt.After(*before) || item.OccurredAt.Equal(*before) && item.ID >= beforeID) {
			continue
		}
		out = append(out, item)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}
