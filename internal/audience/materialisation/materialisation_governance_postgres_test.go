package materialisation

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/audience/cohort"
	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/consent"
	"campaign-platform/internal/organisation"
	_ "campaign-platform/internal/persistence/database"
	postgresrepo "campaign-platform/internal/persistence/postgres"
	"campaign-platform/internal/segment"
)

type materialisationPolicySwapFixture struct {
	db                 *sql.DB
	worker             *MaterialisationWorker
	repo               *PostgreSQLRepository
	job                MaterialisationJob
	currentEvidence    *cohort.GovernanceEvidenceResolver
	contactA, contactB string
	policyP, policyQ   string
	now                time.Time
}

func newMaterialisationPolicySwapFixture(t *testing.T) materialisationPolicySwapFixture {
	t.Helper()
	dsn := os.Getenv("POSTGRES_COHORT_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_COHORT_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	now := time.Date(2099, 3, 1, 12, 0, 0, 0, time.UTC)
	var actor, org, purpose, review, campaign, contactA, contactB, policyP, policyQ string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&actor, &org, &purpose, &review, &campaign, &contactA, &contactB, &policyP, &policyQ); err != nil {
		t.Fatal(err)
	}
	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Materialisation governance actor','DISABLED',false)`, actor, "mat-governance-"+actor+"@internal.invalid")
	mustExec(`INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, org, "Materialisation governance "+org)
	mustExec(`INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,privacy_notice_reviewed,opt_out_process_reviewed,sample_records_reviewed,status,reviewed_by,reviewed_at,expires_at,outcome) VALUES($1::uuid,$2::uuid,'Materialisation governance review','WHATSAPP','DIRECT','wording-p',true,true,true,'APPROVED',$3::uuid,$4,$5,'APPROVED')`, review, org, actor, now.Add(-24*time.Hour), now.Add(24*time.Hour))
	mustExec(`INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'Materialisation governance purpose','WHATSAPP','wording-p',$4::uuid)`, purpose, org, "MAT_GOV_"+purpose, review)
	mustExec(`INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients,consent_review_id) VALUES($1::uuid,$2::uuid,'Materialisation governance target',$3::uuid,'DRAFT',2,$4::uuid)`, campaign, org, purpose, review)
	for _, policy := range []struct {
		id, status string
		max, hours int
		version    int64
	}{
		{policyP, "ACTIVE", 2, 24, 1},
		{policyQ, "DRAFT", 1, 1, 2},
	} {
		effectiveFrom := now.Add(-time.Hour)
		if policy.id == policyP {
			effectiveFrom = now.Add(-48 * time.Hour)
		}
		mustExec(`INSERT INTO organisation_policy_versions(id,organisation_id,allowed_purpose_ids,prohibited_purpose_ids,frequency_caps,contact_retention_days,campaign_retention_days,status,effective_from,version,created_by,submitted_by,approved_by,reason,created_at,updated_at) VALUES($1::uuid,$2::uuid,'[]'::jsonb,'[]'::jsonb,jsonb_build_array(jsonb_build_object('purposeId',$3::text,'channel','WHATSAPP','maxMessages',$4::int,'windowHours',$5::int)),365,365,$6,$7,$8,$9::uuid,$9::uuid,$9::uuid,'materialisation governance policy',$7,$7)`,
			policy.id, org, purpose, policy.max, policy.hours, policy.status, effectiveFrom, policy.version, actor)
	}
	for index, contact := range []string{contactA, contactB} {
		mustExec(`INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,$2,$3,$4,'ACTIVE',$5)`, contact, []byte("cipher-"+contact), []byte("mat-governance-"+contact), fmt.Sprintf("***800%d", index), now.Add(-24*time.Hour))
		var grant string
		if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&grant); err != nil {
			t.Fatal(err)
		}
		mustExec(`INSERT INTO consent_grants(id,contact_id,organisation_id,purpose_id,channel,wording_version,source_type,evidence_checksum,granted_at,effective_from,status,created_by,client_request_id,request_fingerprint,consent_review_id,version) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,'WHATSAPP','wording-p','IMPORT',$5,$6,$6,'ACTIVE',$7::uuid,$8,$9,$10::uuid,1)`, grant, contact, org, purpose, "checksum-"+grant, now.Add(-24*time.Hour), actor, "grant-"+grant, "fingerprint-"+grant, review)
	}
	// P excludes A (two sends two hours ago) and includes B (one send 30m ago).
	// Q max1/1h reverses those members without changing cardinality.
	for _, delivery := range []struct {
		contact string
		at      time.Time
	}{{contactA, now.Add(-2 * time.Hour)}, {contactA, now.Add(-2 * time.Hour)}, {contactB, now.Add(-30 * time.Minute)}} {
		var priorCampaign, message, snapshot, recipient string
		if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&priorCampaign, &message, &snapshot, &recipient); err != nil {
			t.Fatal(err)
		}
		mustExec(`INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients,consent_review_id,requested_start_at,completion_deadline_at,campaign_timezone) VALUES($1::uuid,$2::uuid,'Prior materialisation cap delivery',$3::uuid,'COMPLETED',1,$4::uuid,$5,$6,'UTC')`, priorCampaign, org, purpose, review, delivery.at.Add(-time.Hour), delivery.at.Add(time.Hour))
		mustExec(`INSERT INTO message_versions(id,campaign_id,version,message_type,body,content_hash,status,client_request_id) VALUES($1::uuid,$2::uuid,1,'TEXT','prior send',repeat('b',64),'APPROVED',$3)`, message, priorCampaign, "mat-message-"+message)
		mustExec(`INSERT INTO audience_snapshots(id,campaign_id,segment_definition,consent_policy_version,snapshot_hash,eligible_count) VALUES($1::uuid,$2::uuid,'{}'::jsonb,'history-v1',$3,1)`, snapshot, priorCampaign, "mat-history-"+snapshot)
		mustExec(`INSERT INTO campaign_recipients(id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,authorised_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,'DELIVERED',$7,$7)`, recipient, priorCampaign, snapshot, delivery.contact, message, "mat-recipient-"+recipient, delivery.at)
	}
	registry, err := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(audiencefilter.Definition{
		Code: "MAT_FIXTURE_ID", DisplayName: "Materialisation fixture contact",
		DataType: audiencefilter.DataTypeText, Operators: []audiencefilter.Operator{audiencefilter.OperatorIn},
		Storage: audiencefilter.StorageCoreColumn, QueryableField: "c.id", Core: true, Filterable: true, Active: true,
	}); err != nil {
		t.Fatal(err)
	}
	definition := audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{DefinitionCode: "MAT_FIXTURE_ID", Operator: audiencefilter.OperatorIn, Values: []any{contactA, contactB}}}}
	execution := cohort.NewExecutionService(cohort.NewCompiler(registry), &cohort.PostgreSQLQueryRepository{DB: db})
	eligibility := cohort.EligibilityContext{OrganisationID: org, PurposeID: purpose, Channel: "WHATSAPP", AsOf: now}
	estimate, err := execution.Estimate(ctx, definition, eligibility, nil)
	if err != nil || estimate.EligibleCount != 1 {
		t.Fatalf("policy P estimate=%+v err=%v", estimate, err)
	}
	pMembers, err := execution.Materialise(ctx, definition, eligibility, nil, 10)
	if err != nil || len(pMembers) != 1 || pMembers[0].ContactID != contactB {
		t.Fatalf("policy P must select B, members=%+v err=%v", pMembers, err)
	}
	repo := &PostgreSQLRepository{DB: db}
	service := &MaterialisationService{Repository: repo, Clock: func() time.Time { return now }}
	job, err := service.Schedule(ctx, ScheduleMaterialisation{
		CampaignID: campaign, Definition: definition, DefinitionVersion: 1, Eligibility: eligibility,
		ConsentPolicyVersion: fmt.Sprintf("consent-review/%s/v1/wording/wording-p", review),
		ConfigurationVersion: fmt.Sprintf("organisation-policy/%s/v1", policyP),
		RequestedBy:          actor, ExpectedCount: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Preserve append-only history, but never leave this fixture's job
	// claimable by later tests or repeated runs in the disposable lane DB.
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		owned, err := repo.Get(cleanupCtx, job.ID)
		if err != nil {
			t.Errorf("read owned materialisation fixture for cleanup: %v", err)
			return
		}
		if owned.CampaignID != job.CampaignID || owned.RequestedBy != actor {
			t.Errorf("refusing cleanup for a job outside this fixture: %s", job.ID)
			return
		}
		if owned.Status == MaterialisationPending || owned.Status == MaterialisationRunning {
			owned, err = repo.Cancel(cleanupCtx, job.ID, owned.Version, actor, "materialisation governance fixture teardown", now)
			if err != nil {
				t.Errorf("cancel unfinished owned materialisation fixture: %v", err)
				return
			}
		}
		if owned.Status == MaterialisationPending || owned.Status == MaterialisationRunning ||
			owned.LeaseOwner != "" || owned.LeaseExpiresAt != nil {
			t.Errorf("fixture teardown left a claimable job/lease: %+v", owned)
		}
	})
	evidence := &cohort.GovernanceEvidenceResolver{
		Purposes: &consent.PurposeService{Repository: &postgresrepo.ConsentPurposeRepository{DB: db}},
		Reviews:  consent.NewService(&postgresrepo.ConsentRepository{DB: db}),
		Policies: &organisation.PolicyAdministration{Store: &postgresrepo.OrganisationPolicyRepository{DB: db}},
	}
	worker := &MaterialisationWorker{
		Repository: repo, Cohorts: execution, Snapshots: &segment.PostgreSQLStore{DB: db},
		Evidence: &GovernanceEvidenceResolver{Evidence: evidence},
		WorkerID: "materialisation-governance-worker", BatchSize: 10, ClaimBatch: 1,
		LeaseDuration: time.Minute, Clock: func() time.Time { return now },
	}
	return materialisationPolicySwapFixture{db, worker, repo, job, evidence, contactA, contactB, policyP, policyQ, now}
}

func (f materialisationPolicySwapFixture) rotatePolicy(ctx context.Context) error {
	tx, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE organisation_policy_versions SET status='RETIRED',effective_to=$2,version=version+1 WHERE id=$1::uuid`, f.policyP, f.now.Add(-time.Hour)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE organisation_policy_versions SET status='ACTIVE' WHERE id=$1::uuid`, f.policyQ); err != nil {
		return err
	}
	return tx.Commit()
}

type materialisationPolicySwapQueries struct {
	*cohort.PostgreSQLQueryRepository
	before func(context.Context) error
}

func (q *materialisationPolicySwapQueries) MembersAfter(ctx context.Context, compiled cohort.CompiledQuery, after string, limit int) ([]string, error) {
	if q.before != nil {
		if err := q.before(ctx); err != nil {
			return nil, err
		}
	}
	return q.PostgreSQLQueryRepository.MembersAfter(ctx, compiled, after, limit)
}

func TestPostgreSQLMaterialisationRejectsSameCountPolicyMemberReplacement(t *testing.T) {
	for _, phase := range []string{"before membership", "during membership"} {
		t.Run(phase, func(t *testing.T) {
			fixture := newMaterialisationPolicySwapFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if phase == "before membership" {
				if err := fixture.rotatePolicy(ctx); err != nil {
					t.Fatal(err)
				}
			} else {
				fixture.worker.Cohorts.Repository = &materialisationPolicySwapQueries{
					PostgreSQLQueryRepository: &cohort.PostgreSQLQueryRepository{DB: fixture.db},
					before:                    fixture.rotatePolicy,
				}
			}
			claimed, err := fixture.repo.Claim(ctx, fixture.worker.WorkerID, 1, time.Minute, fixture.now)
			if err != nil || len(claimed) != 1 || claimed[0].ID != fixture.job.ID {
				t.Fatalf("claim=%+v err=%v", claimed, err)
			}
			processErr := fixture.worker.process(ctx, claimed[0])
			// If the old worker accepted the replacement page, drain its final
			// empty page to expose the completed count1/P-labelled A snapshot.
			if processErr == nil {
				reclaimed, err := fixture.repo.Claim(ctx, fixture.worker.WorkerID, 1, time.Minute, fixture.now)
				if err != nil || len(reclaimed) != 1 {
					t.Fatalf("reclaim=%+v err=%v", reclaimed, err)
				}
				if err := fixture.worker.process(ctx, reclaimed[0]); err != nil {
					t.Fatal(err)
				}
			}
			stored, err := fixture.repo.Get(ctx, fixture.job.ID)
			if err != nil {
				t.Fatal(err)
			}
			var stagedCount int
			if err := fixture.db.QueryRowContext(ctx, `SELECT count(*) FROM audience_materialisation_members WHERE job_id=$1::uuid`, fixture.job.ID).Scan(&stagedCount); err != nil {
				t.Fatal(err)
			}
			if processErr == nil || stagedCount != 0 || stored.ProcessedCount != 0 || stored.SnapshotID != "" {
				var published []segment.Member
				if stored.SnapshotID != "" {
					published, _ = fixture.worker.Snapshots.Members(ctx, stored.SnapshotID, "", 10)
				}
				t.Fatalf("same-count replacement was accepted: err=%v processed=%d staged=%d snapshot=%s frozen=%s published=%+v Pmember=%s Qmember=%s", processErr, stored.ProcessedCount, stagedCount, stored.SnapshotID, stored.ConfigurationVersion, published, fixture.contactB, fixture.contactA)
			}
			qMembers, err := fixture.worker.Cohorts.Materialise(ctx, fixture.job.Definition, fixture.job.Eligibility, nil, 10)
			if err != nil || len(qMembers) != 1 || qMembers[0].ContactID != fixture.contactA {
				t.Fatalf("policy Q must replace B with A at unchanged count1: members=%+v err=%v", qMembers, err)
			}
		})
	}
}

func TestPostgreSQLMaterialisationStableGovernanceUnderAudienceWorkerRole(t *testing.T) {
	fixture := newMaterialisationPolicySwapFixture(t)
	roleDB, err := sql.Open("postgres", os.Getenv("POSTGRES_COHORT_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	roleDB.SetMaxOpenConns(1)
	roleDB.SetMaxIdleConns(1)
	t.Cleanup(func() {
		resetCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = roleDB.ExecContext(resetCtx, "RESET ROLE")
		_ = roleDB.Close()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := roleDB.ExecContext(ctx, "SET ROLE campaign_audience_worker"); err != nil {
		t.Fatal(err)
	}
	organisations := organisation.NewService(&postgresrepo.OrganisationRepository{DB: roleDB})
	evidence := &cohort.GovernanceEvidenceResolver{
		Purposes: &consent.PurposeService{Repository: &postgresrepo.ConsentPurposeRepository{DB: roleDB}},
		Reviews:  consent.NewService(&postgresrepo.ConsentRepository{DB: roleDB}).WithOrganisationReader(organisations),
		Policies: &organisation.PolicyAdministration{
			Store: &postgresrepo.OrganisationPolicyRepository{DB: roleDB}, Organisations: organisations,
		},
	}
	resolve := &GovernanceEvidenceResolver{Evidence: evidence}
	consentVersion, configurationVersion, err := resolve.ResolveMaterialisationEvidence(ctx, fixture.job)
	if err != nil || consentVersion != fixture.job.ConsentPolicyVersion || configurationVersion != fixture.job.ConfigurationVersion {
		t.Fatalf("authoritative governance reads under audience role: consent=%s configuration=%s err=%v", consentVersion, configurationVersion, err)
	}
	fixture.repo = &PostgreSQLRepository{DB: roleDB}
	fixture.worker.Repository = fixture.repo
	fixture.worker.Cohorts.Repository = &cohort.PostgreSQLQueryRepository{DB: roleDB}
	fixture.worker.Snapshots = &segment.PostgreSQLStore{DB: roleDB}
	fixture.worker.Evidence = resolve
	for page := 0; page < 2; page++ {
		claimed, err := fixture.repo.Claim(ctx, fixture.worker.WorkerID, 1, time.Minute, fixture.now)
		if err != nil || len(claimed) != 1 || claimed[0].ID != fixture.job.ID {
			t.Fatalf("audience role claim=%+v err=%v", claimed, err)
		}
		if err := fixture.worker.process(ctx, claimed[0]); err != nil {
			t.Fatal(err)
		}
	}
	job, err := fixture.repo.Get(ctx, fixture.job.ID)
	if err != nil || job.Status != MaterialisationCompleted || job.ProcessedCount != 1 || job.SnapshotID == "" {
		t.Fatalf("audience role completed job=%+v err=%v", job, err)
	}
	snapshot, err := fixture.worker.Snapshots.Get(ctx, job.SnapshotID)
	if err != nil || snapshot.ConsentPolicyVersion != fixture.job.ConsentPolicyVersion ||
		snapshot.ConfigurationVersion != fixture.job.ConfigurationVersion || snapshot.EligibleCount != 1 {
		t.Fatalf("stable P snapshot=%+v err=%v", snapshot, err)
	}
	members, err := fixture.worker.Snapshots.Members(ctx, job.SnapshotID, "", 10)
	if err != nil || len(members) != 1 || members[0].ContactID != fixture.contactB {
		t.Fatalf("stable P snapshot must retain B: members=%+v err=%v", members, err)
	}
}
