package sender

import (
	"context"
	"errors"
	"testing"
)

type fakeSessionGateway struct {
	calls []string
	fail  string
}

func (f *fakeSessionGateway) hit(name string) (SessionGatewayResult, error) {
	f.calls = append(f.calls, name)
	if f.fail == name {
		return SessionGatewayResult{}, errors.New("gateway failure")
	}
	return SessionGatewayResult{SessionID: "session", Status: name}, nil
}
func (f *fakeSessionGateway) Create(context.Context, Node, GovernedSession) (SessionGatewayResult, error) {
	return f.hit("create")
}
func (f *fakeSessionGateway) Start(context.Context, Node, GovernedSession) (SessionGatewayResult, error) {
	return f.hit("start")
}
func (f *fakeSessionGateway) Stop(context.Context, Node, GovernedSession) (SessionGatewayResult, error) {
	return f.hit("stop")
}
func (f *fakeSessionGateway) Logout(context.Context, Node, GovernedSession) (SessionGatewayResult, error) {
	return f.hit("logout")
}
func (f *fakeSessionGateway) Delete(context.Context, Node, GovernedSession) error {
	_, e := f.hit("delete")
	return e
}
func (f *fakeSessionGateway) QR(context.Context, Node, GovernedSession) (SessionGatewayResult, error) {
	return f.hit("qr")
}
func (f *fakeSessionGateway) PairingCode(context.Context, Node, GovernedSession, string) (SessionGatewayResult, error) {
	return f.hit("pair")
}
func (f *fakeSessionGateway) Drain(context.Context, Node, GovernedSession) error {
	_, e := f.hit("drain")
	return e
}
func (f *fakeSessionGateway) Resume(context.Context, Node, GovernedSession) error {
	_, e := f.hit("resume")
	return e
}
func (f *fakeSessionGateway) Health(context.Context, Node, GovernedSession) (SessionGatewayResult, error) {
	return f.hit("health")
}

type noWork bool

func (n noWork) HasActiveSessionWork(context.Context, string) (bool, error) { return bool(n), nil }

func lifecycleFixture(t *testing.T) (*SessionLifecycleService, GovernedSession) {
	t.Helper()
	ctx := context.Background()
	store := NewMemoryGovernanceStore()
	gov := &GovernanceService{Store: store}
	pool, _ := gov.CreatePool(ctx, Pool{Name: "p", Status: "ACTIVE", MaxMessagesPerMinute: 10, DailyCapacity: 100}, "actor", "create pool")
	node, _ := gov.RegisterNode(ctx, Node{Name: "node", Status: "READY", InternalURL: "https://gateway.internal", GatewayPoolID: "gateway", Provider: "OPENWA", Engine: "WHATSAPP_WEB_JS", AdapterVersion: "1", Capacity: 2}, "actor", "register node")
	session, err := gov.RegisterSession(ctx, GovernedSession{NodeID: node.ID, PoolID: pool.ID, GatewayPoolID: "gateway", MaskedMSISDN: "+234 ***", EngineType: "WHATSAPP_WEB_JS", Status: StatusNew, SafeMessagesPerMinute: 1, SafeDailyCapacity: 10, InFlightLimit: 1}, []byte("cipher"), "actor", "register sender")
	if err != nil {
		t.Fatal(err)
	}
	return &SessionLifecycleService{Governance: gov, Gateway: &fakeSessionGateway{}, ActiveWork: noWork(false)}, session
}
func TestLifecycleCreateAndStartNeverMarksReady(t *testing.T) {
	svc, s := lifecycleFixture(t)
	ctx := context.Background()
	s, _, err := svc.Create(ctx, s.ID, s.Version, "actor", "create gateway session")
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != StatusPairing {
		t.Fatalf("got %s", s.Status)
	}
	if _, err := svc.PairingCode(ctx, s.ID, "2348012345678"); err != nil {
		t.Fatal(err)
	}
	s, _, err = svc.Start(ctx, s.ID, s.Version, "actor", "start paired session")
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != StatusConnecting {
		t.Fatalf("start must wait for heartbeat, got %s", s.Status)
	}
}
func TestLifecycleDestructiveOperationRequiresNoActiveWork(t *testing.T) {
	svc, s := lifecycleFixture(t)
	svc.ActiveWork = noWork(true)
	if _, err := svc.Delete(context.Background(), s.ID, s.Version, "actor", "delete unused sender"); err == nil {
		t.Fatal("expected active work rejection")
	}
}
func TestLifecycleDoesNotTransitionAfterGatewayFailure(t *testing.T) {
	svc, s := lifecycleFixture(t)
	svc.Gateway = &fakeSessionGateway{fail: "create"}
	if _, _, err := svc.Create(context.Background(), s.ID, s.Version, "actor", "create gateway session"); err == nil {
		t.Fatal("expected gateway error")
	}
	current, _ := svc.Governance.Store.GetSession(context.Background(), s.ID)
	if current.Status != StatusNew {
		t.Fatalf("unexpected state %s", current.Status)
	}
}

func TestLifecycleRejectsInvalidTransitionBeforeGatewaySideEffect(t *testing.T) {
	svc, session := lifecycleFixture(t)
	gateway := &fakeSessionGateway{}
	svc.Gateway = gateway
	if _, _, err := svc.Stop(context.Background(), session.ID, session.Version, "actor", "stop new session safely"); err == nil {
		t.Fatal("expected invalid NEW to PAUSED transition rejection")
	}
	if len(gateway.calls) != 0 {
		t.Fatalf("gateway was called before transition validation: %v", gateway.calls)
	}
}
