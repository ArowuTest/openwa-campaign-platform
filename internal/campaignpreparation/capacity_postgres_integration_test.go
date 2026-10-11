package campaignpreparation_test

import (
	prep "campaign-platform/internal/campaignpreparation"
	"campaign-platform/internal/execution"
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestPostgreSQLCampaignReadinessMeasuredPool(t *testing.T) {
	readinessSQLRoles(t, func(t *testing.T, owner, db *sql.DB) {
		now := time.Now().UTC()
		pool, gateway, otherPool, otherGateway := sqlID(t), sqlID(t), sqlID(t), sqlID(t)
		for _, p := range []string{pool, otherPool} {
			sqlMust(t, owner, `INSERT INTO sender_pools(id,name,status,max_messages_per_minute,daily_capacity,reserved_capacity) VALUES($1::uuid,$2,'ACTIVE',25,2500,100)`, p, "U3 "+p)
		}
		for _, g := range []string{gateway, otherGateway} {
			sqlMust(t, owner, `INSERT INTO gateway_pools(id,name,provider,engine,adapter_version,status,capabilities,minimum_healthy_nodes,version) VALUES($1::uuid,$2,'OPENWA','BAILEYS','1.2.3','ACTIVE','["SEND_TEXT"]'::jsonb,1,1)`, g, "U3 "+g)
		}
		// Literal hand-derived baseline: two READY sessions at 10/min and 1000/day,
		// 100 already sent each, minus 100 static reserve => 20/min, 1700/day.
		cases := []struct {
			name, sessionStatus, engine, nodeStatus, nodeProvider, nodeEngine, adapter string
			sessionAge, nodeAge                                                        time.Duration
			draining                                                                   bool
			pool, gateway, nodeGateway                                                 string
		}{
			{"ready1", "READY", "BAILEYS", "READY", "OPENWA", "BAILEYS", "1.2.3", 0, 0, false, pool, gateway, gateway},
			{"ready2", "READY", "BAILEYS", "READY", "OPENWA", "BAILEYS", "1.2.3", 0, 0, false, pool, gateway, gateway},
			{"wrong pool", "READY", "BAILEYS", "READY", "OPENWA", "BAILEYS", "1.2.3", 0, 0, false, otherPool, gateway, gateway},
			{"wrong gateway", "READY", "BAILEYS", "READY", "OPENWA", "BAILEYS", "1.2.3", 0, 0, false, pool, otherGateway, otherGateway},
			{"node gateway", "READY", "BAILEYS", "READY", "OPENWA", "BAILEYS", "1.2.3", 0, 0, false, pool, gateway, otherGateway},
			{"node identity absent", "READY", "BAILEYS", "READY", "", "", "1.2.3", 0, 0, false, pool, gateway, gateway},
			{"node engine", "READY", "BAILEYS", "READY", "OPENWA", "WHATSAPP_WEB_JS", "1.2.3", 0, 0, false, pool, gateway, gateway},
			{"node adapter", "READY", "BAILEYS", "READY", "OPENWA", "BAILEYS", "2.0.0", 0, 0, false, pool, gateway, gateway},
			{"session alias", "READY", "baileys", "READY", "OPENWA", "BAILEYS", "1.2.3", 0, 0, false, pool, gateway, gateway},
			{"session stale", "READY", "BAILEYS", "READY", "OPENWA", "BAILEYS", "1.2.3", 90 * time.Second, 0, false, pool, gateway, gateway},
			{"node stale", "READY", "BAILEYS", "READY", "OPENWA", "BAILEYS", "1.2.3", 0, 90 * time.Second, false, pool, gateway, gateway},
			{"draining", "READY", "BAILEYS", "READY", "OPENWA", "BAILEYS", "1.2.3", 0, 0, true, pool, gateway, gateway},
			{"offline", "READY", "BAILEYS", "OFFLINE", "OPENWA", "BAILEYS", "1.2.3", 0, 0, false, pool, gateway, gateway},
			{"paused", "PAUSED", "BAILEYS", "READY", "OPENWA", "BAILEYS", "1.2.3", 0, 0, false, pool, gateway, gateway},
		}
		var firstSession string
		for i, tc := range cases {
			node, session := sqlID(t), sqlID(t)
			if i == 0 {
				firstSession = session
			}
			sqlMust(t, owner, `INSERT INTO sender_nodes(id,name,status,gateway_pool_id,provider,engine,adapter_version,last_heartbeat_at,draining,internal_url) VALUES($1::uuid,$2,$3,$4::uuid,NULLIF($5,''),NULLIF($6,''),$7,$8,$9,'http://127.0.0.1:1')`, node, "U3 "+node, tc.nodeStatus, tc.nodeGateway, tc.nodeProvider, tc.nodeEngine, tc.adapter, now.Add(-tc.nodeAge), tc.draining)
			sqlMust(t, owner, `INSERT INTO sender_sessions(id,node_id,sender_pool_id,gateway_pool_id,encrypted_msisdn,masked_msisdn,engine_type,status,safe_messages_per_minute,safe_daily_capacity,sent_today,last_heartbeat_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,decode('00','hex'),'synthetic',$5,$6,10,1000,100,$7)`, session, node, tc.pool, tc.gateway, tc.engine, tc.sessionStatus, now.Add(-tc.sessionAge))
		}
		reader := &prep.PostgreSQLCapacityReader{DB: db}
		route := execution.PoolRoute{SenderPoolID: pool, GatewayPoolID: gateway, Provider: "OPENWA", Engine: "BAILEYS", ProviderAdapterVersion: "1.2.3"}
		got, e := reader.RouteCapacity(context.Background(), route, now)
		if e != nil {
			t.Fatalf("expected live eligible capacity: %v", e)
		}
		if got.HealthySessions != 2 || got.HealthyNodes != 2 || got.AvailableMessagesPerMinute != 20 || got.AvailableHourlyUnits != 1200 || got.AvailableDailyUnits != 1700 {
			t.Fatalf("exact eligible aggregate: %+v", got)
		}
		sqlMust(t, owner, `UPDATE sender_sessions SET status='BUSY' WHERE id=$1::uuid`, firstSession)
		busy, e := reader.RouteCapacity(context.Background(), route, now)
		if e != nil || busy != got {
			t.Fatalf("BUSY healthy semantics changed: %+v %v", busy, e)
		}
		for i := 0; i < 3; i++ {
			again, e := reader.RouteCapacity(context.Background(), route, now)
			if e != nil || again != got {
				t.Fatalf("repeat aggregate differs: %+v %v", again, e)
			}
		}
		t.Logf("measured healthy sessions=%d nodes=%d mpm=%d hourly=%d daily=%d; exact wrong-route/stale/draining evidence excluded", got.HealthySessions, got.HealthyNodes, got.AvailableMessagesPerMinute, got.AvailableHourlyUnits, got.AvailableDailyUnits)
		// No fixture cleanup DELETE: disposable database teardown belongs to harness owner.
	})
}

func TestPostgreSQLCampaignReadinessNoncanonicalRegistration(t *testing.T) {
	readinessSQLRoles(t, func(t *testing.T, owner, db *sql.DB) {
		f := newSQLPreparationFixture(t, owner, db, false)
		h, token := sqlReadinessHandler(t, f.svc)
		reader := &prep.PostgreSQLCapacityReader{DB: db}
		route := execution.PoolRoute{SenderPoolID: f.pool, GatewayPoolID: f.gateway, Provider: "OPENWA", Engine: "BAILEYS", ProviderAdapterVersion: f.campaign.Transport.AdapterVersion}
		otherPool := sqlID(t)
		sqlMust(t, owner, `INSERT INTO sender_pools(id,name,status,max_messages_per_minute,daily_capacity,reserved_capacity) VALUES($1::uuid,$2,'ACTIVE',30,3000,100)`, otherPool, "U3 "+otherPool)
		for _, tc := range []struct {
			name, engine, code string
			age                time.Duration
			otherPool          bool
			identityError      bool
		}{
			{"lowercase alias only", "baileys", "CAPACITY_ROUTE_IDENTITY_UNAVAILABLE", 0, false, true},
			{"legacy identity only", "openwa", "CAPACITY_ROUTE_IDENTITY_UNAVAILABLE", 0, false, true},
			{"canonical stale", "BAILEYS", "CAPACITY_UNAVAILABLE", 90 * time.Second, false, false},
			{"noncanonical stale", "baileys", "CAPACITY_UNAVAILABLE", 90 * time.Second, false, false},
			{"other canonical engine", "WHATSAPP_WEB_JS", "CAPACITY_UNAVAILABLE", 0, false, false},
			{"absent matching registrations", "BAILEYS", "CAPACITY_UNAVAILABLE", 0, true, false},
			{"noncanonical wrong pool", "baileys", "CAPACITY_UNAVAILABLE", 0, true, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				pool := f.pool
				if tc.otherPool {
					pool = otherPool
				}
				// Fixture changes precede the complete read-only footprint boundary.
				sqlMust(t, owner, `UPDATE sender_sessions SET sender_pool_id=$2::uuid,engine_type=$3,last_heartbeat_at=$4 WHERE gateway_pool_id=$1::uuid`, f.gateway, pool, tc.engine, f.now.Add(-tc.age))
				before := readinessFootprint(t, db)
				for i := 0; i < 3; i++ {
					got, err := reader.RouteCapacity(context.Background(), route, f.now)
					if tc.identityError {
						if !errors.Is(err, prep.ErrRouteIdentityUnavailable) {
							t.Errorf("noncanonical registration error=%v want ErrRouteIdentityUnavailable", err)
						}
						if got != (execution.PoolCapacity{}) {
							t.Errorf("noncanonical registration credited capacity: %+v", got)
						}
					} else if err != nil || got.HealthySessions != 0 || got.HealthyNodes != 0 || got.AvailableMessagesPerMinute != 0 || got.AvailableHourlyUnits != 0 || got.AvailableDailyUnits != 0 {
						t.Errorf("absent or ineligible registration measurement=%+v error=%v", got, err)
					}
					r := sqlReadinessGET(t, h, token, f.campaign.ID, 200)
					foundCapacity := false
					for _, check := range r.Checks {
						if check.Key == "capacity" {
							foundCapacity = true
						}
						if check.Key == "capacity" && (check.Status != "UNAVAILABLE" || check.Code != tc.code) {
							t.Errorf("public capacity=%s/%s want UNAVAILABLE/%s", check.Status, check.Code, tc.code)
						}
					}
					if !foundCapacity {
						t.Error("missing public capacity check")
					}
					if tc.identityError && r.Capacity != nil {
						t.Error("noncanonical identity exposed a capacity projection")
					}
					if r.ReadyForFinalReview {
						t.Error("unmeasurable registration reported ready")
					}
				}
				if !reflect.DeepEqual(before, readinessFootprint(t, db)) {
					t.Fatal("capacity measurement or readiness GET changed database footprint")
				}
				if f.objects.writes != 0 {
					t.Fatal("capacity measurement or readiness GET mutated objects")
				}
				t.Logf("%s: expected public %s; %d public table counts and row hashes unchanged; zero object writes", tc.name, tc.code, len(before))
			})
		}
	})
}
