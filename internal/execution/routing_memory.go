package execution

import (
	"context"
	"sort"
	"sync"
	"time"
)

type MemoryRoutingPlanStore struct {
	mu           sync.Mutex
	plans        map[string]RoutingPlan
	byCampaign   map[string][]string
	reservations map[string][]CapacityReservation
	idempotency  map[string]string
}

func NewMemoryRoutingPlanStore() *MemoryRoutingPlanStore {
	return &MemoryRoutingPlanStore{plans: map[string]RoutingPlan{}, byCampaign: map[string][]string{}, reservations: map[string][]CapacityReservation{}, idempotency: map[string]string{}}
}
func (s *MemoryRoutingPlanStore) Create(_ context.Context, p RoutingPlan, r []CapacityReservation) (RoutingPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.plans[p.ID]; ok {
		return RoutingPlan{}, ErrRoutingPlanConflict
	}
	key := p.CampaignID + "\x00" + p.IdempotencyKey
	if existingID, ok := s.idempotency[key]; ok {
		existing := s.plans[existingID]
		if existing.RequestHash != p.RequestHash {
			return RoutingPlan{}, ErrRoutingPlanConflict
		}
		return existing, nil
	}
	var next int64 = 1
	for _, planID := range s.byCampaign[p.CampaignID] {
		if existing := s.plans[planID]; existing.Version >= next {
			next = existing.Version + 1
		}
	}
	p.Version = next
	s.plans[p.ID] = p
	s.idempotency[key] = p.ID
	s.byCampaign[p.CampaignID] = append(s.byCampaign[p.CampaignID], p.ID)
	s.reservations[p.ID] = append([]CapacityReservation(nil), r...)
	return p, nil
}
func (s *MemoryRoutingPlanStore) Get(_ context.Context, id string) (RoutingPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.plans[id]
	if !ok {
		return RoutingPlan{}, ErrRoutingPlanNotFound
	}
	return p, nil
}
func (s *MemoryRoutingPlanStore) ListByCampaign(_ context.Context, id string) ([]RoutingPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []RoutingPlan{}
	for _, pid := range s.byCampaign[id] {
		out = append(out, s.plans[pid])
	}
	return out, nil
}
func (s *MemoryRoutingPlanStore) ListRoutingPlanPage(_ context.Context, campaignID string, limit int, beforeVersion int64) ([]RoutingPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]RoutingPlan, 0)
	for _, planID := range s.byCampaign[campaignID] {
		plan := s.plans[planID]
		if beforeVersion > 0 && plan.Version >= beforeVersion {
			continue
		}
		items = append(items, plan)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Version > items[j].Version })
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *MemoryRoutingPlanStore) LatestByCampaign(_ context.Context, id string) (RoutingPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.byCampaign[id]
	if len(ids) == 0 {
		return RoutingPlan{}, ErrRoutingPlanNotFound
	}
	latest := s.plans[ids[0]]
	for _, planID := range ids[1:] {
		value := s.plans[planID]
		if value.Version > latest.Version || (value.Version == latest.Version && value.ApprovedAt.After(latest.ApprovedAt)) {
			latest = value
		}
	}
	return latest, nil
}
func (s *MemoryRoutingPlanStore) Reservations(_ context.Context, id string) ([]CapacityReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.reservations[id]
	if !ok {
		return nil, ErrRoutingPlanNotFound
	}
	return append([]CapacityReservation(nil), r...), nil
}
func (s *MemoryRoutingPlanStore) ActivateReservations(_ context.Context, id string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.reservations[id]
	if !ok {
		return ErrRoutingPlanNotFound
	}
	for i := range r {
		if r[i].Status == "HELD" {
			r[i].Status = "ACTIVE"
			r[i].FencingVersion++
			r[i].UpdatedAt = now
		}
	}
	s.reservations[id] = r
	return nil
}
func (s *MemoryRoutingPlanStore) ReleaseReservations(_ context.Context, id, actor string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.reservations[id]
	if !ok {
		return ErrRoutingPlanNotFound
	}
	for i := range r {
		if r[i].Status == "HELD" || r[i].Status == "ACTIVE" {
			r[i].Status = "RELEASED"
			r[i].FencingVersion++
			r[i].UpdatedAt = now
		}
	}
	s.reservations[id] = r
	return nil
}

func (s *MemoryRoutingPlanStore) PoolExecutionReport(_ context.Context, id string) ([]PoolExecutionReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.plans[id]
	if !ok {
		return nil, ErrRoutingPlanNotFound
	}
	out := make([]PoolExecutionReport, 0, len(p.Routes))
	for _, r := range p.Routes {
		out = append(out, PoolExecutionReport{RoutingPlanID: id, SenderPoolID: r.SenderPoolID, GatewayPoolID: r.GatewayPoolID, Provider: r.Provider, Engine: r.Engine, ProviderAdapterVersion: r.ProviderAdapterVersion, ProviderDefinitionID: r.ProviderDefinitionID, ProviderDefinitionVersion: r.ProviderDefinitionVersion, GatewayPoolVersion: r.GatewayPoolVersion, MaximumRecipients: r.MaximumRecipients, ReservedMessagesPerMinute: r.ReservedMessagesPerMinute, ReservedHourlyUnits: r.ReservedHourlyUnits, ReservedDailyUnits: r.ReservedDailyUnits})
	}
	return out, nil
}
