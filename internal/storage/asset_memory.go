package storage

import (
	"context"
	"sync"
	"time"
)

type MemoryAssetRepository struct {
	mu    sync.Mutex
	items map[string]Asset
}

func NewMemoryAssetRepository() *MemoryAssetRepository {
	return &MemoryAssetRepository{items: map[string]Asset{}}
}
func (r *MemoryAssetRepository) Create(_ context.Context, a Asset) (Asset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[a.ID]; ok {
		return Asset{}, ErrAssetConflict
	}
	r.items[a.ID] = a
	return a, nil
}
func (r *MemoryAssetRepository) Get(_ context.Context, id string) (Asset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.items[id]
	if !ok {
		return Asset{}, ErrAssetNotFound
	}
	return a, nil
}
func (r *MemoryAssetRepository) UpdateScan(_ context.Context, id string, e int64, status AssetStatus, sig string, now time.Time) (Asset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.items[id]
	if !ok {
		return Asset{}, ErrAssetNotFound
	}
	if a.Version != e {
		return Asset{}, ErrAssetConflict
	}
	a.Status = status
	a.ScanSignature = sig
	a.Version++
	a.UpdatedAt = now
	r.items[id] = a
	return a, nil
}
func (r *MemoryAssetRepository) Revoke(_ context.Context, id string, e int64, _ string, _ string, now time.Time) (Asset, error) {
	return r.UpdateScan(context.Background(), id, e, AssetStatusRevoked, "", now)
}
