package cohort

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/jobs"
	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLEstimateRepositoryPersistsAcrossRestartAndFencesReplay(t *testing.T) {
	dsn := os.Getenv("POSTGRES_COHORT_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_COHORT_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2099, 3, 1, 12, 0, 0, 0, time.UTC)
	ids := make([]string, 5)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	actorID, orgID, purposeID, reviewID, policyID := ids[0], ids[1], ids[2], ids[3], ids[4]

	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Async Estimate Actor','DISABLED',false)`, actorID, "cohort-async-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Async Estimate "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,privacy_notice_reviewed,opt_out_process_reviewed,sample_records_reviewed,status,reviewed_by,reviewed_at,expires_at,outcome) VALUES($1::uuid,$2::uuid,'Async estimate review','WHATSAPP','DIRECT','v1',true,true,true,'APPROVED',$3::uuid,$4,$5,'APPROVED')`, reviewID, orgID, actorID, now.Add(-24*time.Hour), now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'Async estimate purpose','WHATSAPP','v1',$4::uuid)`, purposeID, orgID, "ASYNC_"+purposeID, reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO organisation_policy_versions(
 id,organisation_id,allowed_purpose_ids,prohibited_purpose_ids,frequency_caps,
 contact_retention_days,campaign_retention_days,status,effective_from,version,
 created_by,submitted_by,approved_by,reason,created_at,updated_at
) VALUES(
 $1::uuid,$2::uuid,'[]'::jsonb,'[]'::jsonb,'[]'::jsonb,
 365,365,'ACTIVE',$3,1,$4::uuid,$4::uuid,$4::uuid,'async estimate policy',$3,$3
)`, policyID, orgID, now.Add(-48*time.Hour), actorID); err != nil {
		t.Fatal(err)
	}

	queue := &jobs.PostgreSQLRepository{DB: db}
	repo := &PostgreSQLEstimateRepository{DB: db, Queue: queue}
	request := EstimateJobRequest{
		OrganisationID: orgID,
		PurposeID:      purposeID,
		Channel:        "WHATSAPP",
		Definition: audiencefilter.Group{
			Join: audiencefilter.JoinAnd,
			Rules: []audiencefilter.Rule{{
				DefinitionCode: "COUNTRY",
				Operator:       audiencefilter.OperatorIn,
				Values:         []any{"NG"},
			}},
		},
		RequestedBy:     actorID,
		ClientRequestID: "postgres-estimate-restart-0001",
	}

	first, created, err := repo.Schedule(ctx, request, now)
	if err != nil || !created {
		t.Fatalf("schedule created=%v err=%v", created, err)
	}
	if first.Job.Status != jobs.StatusPending {
		t.Fatalf("scheduled status=%s want PENDING", first.Job.Status)
	}

	replay, created, err := repo.Schedule(ctx, request, now.Add(time.Second))
	if err != nil || created || replay.ID != first.ID {
		t.Fatalf("replay id=%s created=%v err=%v", replay.ID, created, err)
	}

	conflict := request
	conflict.Definition = audiencefilter.Group{
		Join: audiencefilter.JoinAnd,
		Rules: []audiencefilter.Rule{{
			DefinitionCode: "COUNTRY",
			Operator:       audiencefilter.OperatorIn,
			Values:         []any{"GH"},
		}},
	}
	if _, _, err := repo.Schedule(ctx, conflict, now.Add(2*time.Second)); !errors.Is(err, ErrEstimateReplayConflict) {
		t.Fatalf("expected replay conflict, got %v", err)
	}

	claimed, err := queue.Claim(ctx, "restart-worker-a", now.Add(3*time.Second), 2*time.Minute, 1, []string{EstimateJobType})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim len=%d err=%v", len(claimed), err)
	}
	lease := claimed[0]
	result := Estimate{
		EligibleCount: 17,
		Breakdown: &EligibilityBreakdown{
			MatchedProfiles: 20, ConsentEligible: 19, ConsentExcluded: 1,
			Unsuppressed: 18, SuppressionExcluded: 1, Eligible: 17, FrequencyCapExcluded: 1,
		},
		CalculatedAt: now.Add(4 * time.Second),
	}
	evidence := EstimateGovernanceEvidence{
		ConsentReviewID:           reviewID,
		ConsentReviewVersion:      1,
		ConsentWordingVersion:     "v1",
		OrganisationPolicyID:      policyID,
		OrganisationPolicyVersion: 1,
	}
	if err := repo.StoreResult(ctx, first.ID, lease.LeaseOwner, lease.LeaseVersion, result, evidence, now.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}

	// Simulate a crash after the fenced result write but before durable-job
	// completion. After the original lease expires, a fresh worker must reclaim
	// the job and complete it from the persisted result without recomputing.
	restartedQueue := &jobs.PostgreSQLRepository{DB: db}
	restarted := &PostgreSQLEstimateRepository{DB: db, Queue: restartedQueue}
	restartAt := now.Add(3 * time.Minute)
	restartedWorker := &EstimateWorker{
		Queue:         restartedQueue,
		Estimates:     restarted,
		Cohorts:       &ExecutionService{},
		Evidence:      estimateEvidenceErrorResolver{},
		WorkerID:      "restart-worker-b",
		ClaimBatch:    1,
		LeaseDuration: 2 * time.Minute,
		Clock:         func() time.Time { return restartAt },
	}
	processed, err := restartedWorker.Process(ctx)
	if err != nil || processed != 1 {
		t.Fatalf("restart processed=%d err=%v", processed, err)
	}

	recovered, err := restarted.Get(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Job.Status != jobs.StatusCompleted || recovered.Result == nil || recovered.Result.EligibleCount != 17 {
		t.Fatalf("recovered=%+v", recovered)
	}
	if recovered.Evidence != evidence {
		t.Fatalf("recovered evidence=%+v want=%+v", recovered.Evidence, evidence)
	}

	page, err := restarted.ListPage(ctx, orgID, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != first.ID || page.Items[0].Job.Status != jobs.StatusCompleted {
		t.Fatalf("page=%+v", page)
	}
}

func TestPostgreSQLEstimateRepositoryConcurrentIdenticalSchedulesConverge(t *testing.T) {
	testPostgreSQLEstimateRepositoryConcurrentSchedules(t, false)
}

func TestPostgreSQLEstimateRepositoryConcurrentConflictingSchedulesFenceReplay(t *testing.T) {
	testPostgreSQLEstimateRepositoryConcurrentSchedules(t, true)
}

func testPostgreSQLEstimateRepositoryConcurrentSchedules(t *testing.T, conflicting bool) {
	dsn := os.Getenv("POSTGRES_COHORT_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_COHORT_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2099, 3, 2, 12, 0, 0, 0, time.UTC)
	ids := make([]string, 4)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	actorID, orgID, purposeID, reviewID := ids[0], ids[1], ids[2], ids[3]
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Concurrent Estimate Actor','DISABLED',false)`, actorID, "cohort-concurrent-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Concurrent Estimate "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,privacy_notice_reviewed,opt_out_process_reviewed,sample_records_reviewed,status,reviewed_by,reviewed_at,expires_at,outcome) VALUES($1::uuid,$2::uuid,'Concurrent estimate review','WHATSAPP','DIRECT','v1',true,true,true,'APPROVED',$3::uuid,$4,$5,'APPROVED')`, reviewID, orgID, actorID, now.Add(-24*time.Hour), now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'Concurrent estimate purpose','WHATSAPP','v1',$4::uuid)`, purposeID, orgID, "CONCURRENT_"+purposeID, reviewID); err != nil {
		t.Fatal(err)
	}

	queue := &jobs.PostgreSQLRepository{DB: db}
	repo := &PostgreSQLEstimateRepository{DB: db, Queue: queue}
	request := EstimateJobRequest{
		OrganisationID: orgID,
		PurposeID:      purposeID,
		Channel:        "WHATSAPP",
		Definition: audiencefilter.Group{
			Join:  audiencefilter.JoinAnd,
			Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorIn, Values: []any{"NG"}}},
		},
		RequestedBy:     actorID,
		ClientRequestID: "postgres-estimate-concurrent-0001",
	}

	requests := []EstimateJobRequest{request, request}
	if conflicting {
		requests[1].Definition = audiencefilter.Group{
			Join:  audiencefilter.JoinAnd,
			Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorIn, Values: []any{"GH"}}},
		}
	}
	type outcome struct {
		record  EstimateJobRecord
		created bool
		err     error
		request int
	}
	start := make(chan struct{})
	results := make(chan outcome, 2)
	for index, input := range requests {
		go func(index int, input EstimateJobRequest) {
			<-start
			record, created, err := repo.Schedule(ctx, input, now)
			results <- outcome{record: record, created: created, err: err, request: index}
		}(index, input)
	}
	close(start)
	first := <-results
	second := <-results
	if conflicting {
		winner, loser := first, second
		if winner.err != nil {
			winner, loser = loser, winner
		}
		if winner.err != nil || !winner.created || winner.record.ID == "" {
			t.Fatalf("conflicting schedule winner=%+v", winner)
		}
		if !errors.Is(loser.err, ErrEstimateReplayConflict) || loser.created || loser.record.ID != "" {
			t.Fatalf("conflicting schedule loser=%+v", loser)
		}
		persisted, err := repo.Get(ctx, winner.record.ID)
		if err != nil || !reflect.DeepEqual(persisted.Definition, requests[winner.request].Definition) {
			t.Fatalf("winner definition was not preserved: persisted=%+v err=%v", persisted, err)
		}
	} else {
		for index, result := range []outcome{first, second} {
			if result.err != nil {
				t.Fatalf("schedule %d err=%v", index+1, result.err)
			}
		}
		if first.record.ID == "" || first.record.ID != second.record.ID {
			t.Fatalf("concurrent schedules diverged: first=%s second=%s", first.record.ID, second.record.ID)
		}
		if first.created == second.created {
			t.Fatalf("created flags first=%v second=%v; want exactly one creator", first.created, second.created)
		}
	}
	var durableCount, evidenceCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM durable_jobs WHERE deduplication_key=$1`, estimateDedupKey(request)).Scan(&durableCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audience_cohort_estimates WHERE organisation_id=$1::uuid AND client_request_id=$2`, orgID, request.ClientRequestID).Scan(&evidenceCount); err != nil {
		t.Fatal(err)
	}
	if durableCount != 1 || evidenceCount != 1 {
		t.Fatalf("durableCount=%d evidenceCount=%d; want 1/1", durableCount, evidenceCount)
	}
}
