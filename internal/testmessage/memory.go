package testmessage

import (
	"context"
	"sort"
	"sync"
	"time"
)

type MemoryRepository struct {
	mu            sync.Mutex
	recipients    map[string]Recipient
	sends         map[string]Send
	byIdempotency map[string]string
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{recipients: map[string]Recipient{}, sends: map[string]Send{}, byIdempotency: map[string]string{}}
}
func (r *MemoryRepository) CreateRecipient(_ context.Context, v Recipient) (Recipient, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.recipients {
		if string(e.MSISDNLookupHash) == string(v.MSISDNLookupHash) && e.Status != RecipientRevoked {
			return Recipient{}, ErrConflict
		}
	}
	r.recipients[v.ID] = v
	return v, nil
}
func (r *MemoryRepository) ListRecipientPage(_ context.Context, limit int, before *time.Time, beforeID string) ([]Recipient, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Recipient, 0, len(r.recipients))
	for _, value := range r.recipients {
		if before != nil && !(value.CreatedAt.Before(*before) || (value.CreatedAt.Equal(*before) && value.ID < beforeID)) {
			continue
		}
		out = append(out, value)
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

func (r *MemoryRepository) GetRecipient(_ context.Context, id string) (Recipient, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.recipients[id]
	if !ok {
		return Recipient{}, ErrNotFound
	}
	return v, nil
}
func (r *MemoryRepository) ListRecipients(_ context.Context) ([]Recipient, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Recipient, 0, len(r.recipients))
	for _, v := range r.recipients {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
func (r *MemoryRepository) CompareAndSwapRecipient(_ context.Context, v Recipient, expected int64) (Recipient, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.recipients[v.ID]
	if !ok {
		return Recipient{}, ErrNotFound
	}
	if cur.Version != expected {
		return Recipient{}, ErrConflict
	}
	v.Version = expected + 1
	r.recipients[v.ID] = v
	return v, nil
}
func (r *MemoryRepository) CreateSend(_ context.Context, v Send) (Send, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id, ok := r.byIdempotency[v.IdempotencyKey]; ok {
		existing := r.sends[id]
		if !sameSendRequest(existing, v) {
			return Send{}, ErrConflict
		}
		return existing, nil
	}
	r.sends[v.ID] = v
	r.byIdempotency[v.IdempotencyKey] = v.ID
	return v, nil
}
func (r *MemoryRepository) GetSend(_ context.Context, id string) (Send, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.sends[id]
	if !ok {
		return Send{}, ErrNotFound
	}
	return v, nil
}
func (r *MemoryRepository) ListSends(_ context.Context, campaignID string) ([]Send, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Send{}
	for _, v := range r.sends {
		if campaignID == "" || v.CampaignID == campaignID {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
func (r *MemoryRepository) ListSendPage(_ context.Context, campaignID string, limit int, before *time.Time, beforeID string) ([]Send, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []Send{}
	for _, value := range r.sends {
		if value.CampaignID != campaignID {
			continue
		}
		if before != nil && (value.CreatedAt.After(*before) || (value.CreatedAt.Equal(*before) && value.ID >= beforeID)) {
			continue
		}
		items = append(items, value)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}
func (r *MemoryRepository) ClaimSends(_ context.Context, owner string, now time.Time, lease time.Duration, limit int) ([]Send, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit <= 0 {
		limit = 20
	}
	ids := []string{}
	for id, v := range r.sends {
		pendingReady := v.Status == SendPending && (v.AttemptCount == 0 || !v.UpdatedAt.Add(30*time.Second).After(now))
		if pendingReady || (v.Status == SendProcessing && v.LeaseExpiresAt != nil && !v.LeaseExpiresAt.After(now)) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	out := []Send{}
	for _, id := range ids {
		v := r.sends[id]
		v.Status = SendProcessing
		v.LeaseOwner = owner
		v.LeaseVersion++
		v.AttemptCount++
		e := now.Add(lease)
		v.LeaseExpiresAt = &e
		v.UpdatedAt = now
		r.sends[id] = v
		out = append(out, v)
	}
	return out, nil
}
func (r *MemoryRepository) CompleteSend(_ context.Context, v Send, status SendStatus, providerID, code string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.sends[v.ID]
	if !ok {
		return ErrNotFound
	}
	if cur.LeaseOwner != v.LeaseOwner || cur.LeaseVersion != v.LeaseVersion {
		return ErrLeaseConflict
	}
	cur.Status = status
	cur.ProviderMessageID = providerID
	cur.FailureCode = code
	cur.LeaseOwner = ""
	cur.LeaseExpiresAt = nil
	cur.UpdatedAt = now
	if status == SendAccepted || status == SendFailed || status == SendUnknown {
		t := now
		cur.CompletedAt = &t
	}
	r.sends[v.ID] = cur
	return nil
}
