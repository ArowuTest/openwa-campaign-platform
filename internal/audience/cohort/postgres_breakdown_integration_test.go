package cohort

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	audiencefilter "campaign-platform/internal/audience/filter"
	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLCohortEstimateReturnsGovernedEligibilityWaterfall(t *testing.T) {
	dsn := os.Getenv("POSTGRES_COHORT_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_COHORT_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2099, 2, 1, 12, 0, 0, 0, time.UTC)
	ids := make([]string, 11)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	actorID, orgID, purposeID, reviewID := ids[0], ids[1], ids[2], ids[3]
	campaignID, messageID, snapshotID := ids[4], ids[5], ids[6]
	policyID := ids[7]
	recipientID := ids[8]
	spareID1, spareID2 := ids[9], ids[10]
	_ = spareID1
	_ = spareID2

	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Cohort Breakdown Actor','DISABLED',false)`, actorID, "cohort-breakdown-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Cohort Breakdown "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,privacy_notice_reviewed,opt_out_process_reviewed,sample_records_reviewed,status,reviewed_by,reviewed_at,expires_at,outcome) VALUES($1::uuid,$2::uuid,'Cohort breakdown review','WHATSAPP','DIRECT','v1',true,true,true,'APPROVED',$3::uuid,$4,$5,'APPROVED')`, reviewID, orgID, actorID, now.Add(-24*time.Hour), now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'Cohort breakdown purpose','WHATSAPP','v1',$4::uuid)`, purposeID, orgID, "COHORT_"+purposeID, reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO organisation_policy_versions(
 id,organisation_id,allowed_purpose_ids,prohibited_purpose_ids,frequency_caps,
 contact_retention_days,campaign_retention_days,status,effective_from,version,
 created_by,submitted_by,approved_by,reason,created_at,updated_at
) VALUES(
 $1::uuid,$2::uuid,'[]'::jsonb,'[]'::jsonb,
 jsonb_build_array(jsonb_build_object('purposeId',$3::text,'channel','WHATSAPP','maxMessages',1,'windowHours',24)),
 365,365,'ACTIVE',$4,1,$5::uuid,$5::uuid,$5::uuid,'cohort breakdown policy',$4,$4
)`, policyID, orgID, purposeID, now.Add(-48*time.Hour), actorID); err != nil {
		t.Fatal(err)
	}

	contacts := make([]string, 4)
	for i := range contacts {
		if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&contacts[i]); err != nil {
			t.Fatal(err)
		}
		lookup := []byte(fmt.Sprintf("cohort-breakdown-%d-%s", i, contacts[i]))
		if _, err := db.ExecContext(ctx, `
INSERT INTO contacts(
 id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,reported_age,
 age_recorded_at,age_source,age_verified,profile_recorded_at
) VALUES($1::uuid,$2,$3,$4,'ACTIVE',25,$5::date,'SELF_DECLARED_IMPORT',false,$5)
`, contacts[i], []byte("cipher-"+contacts[i]), lookup, fmt.Sprintf("***90%02d", i), now.Add(-24*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	// Contact 0 reaches final eligibility. Contact 1 has no consent. Contacts 2
	// and 3 have consent but are excluded later by suppression and frequency cap.
	for _, index := range []int{0, 2, 3} {
		var grantID string
		if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&grantID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO consent_grants(
 id,contact_id,organisation_id,purpose_id,channel,wording_version,source_type,
 evidence_checksum,granted_at,effective_from,status,created_by,client_request_id,
 request_fingerprint,consent_review_id,version
) VALUES(
 $1::uuid,$2::uuid,$3::uuid,$4::uuid,'WHATSAPP','v1','IMPORT',$5,$6,$6,
 'ACTIVE',$7::uuid,$8,$9,$10::uuid,1
)`, grantID, contacts[index], orgID, purposeID, "checksum-"+grantID, now.Add(-2*time.Hour), actorID, "grant-"+grantID, "fingerprint-"+grantID, reviewID); err != nil {
			t.Fatal(err)
		}
	}

	var suppressionID string
	if err := db.QueryRowContext(ctx, `
INSERT INTO suppressions(
 contact_id,scope,reason,effective_at,created_by,client_request_id,request_fingerprint
) VALUES($1::uuid,'GLOBAL','cohort breakdown suppression',$2,$3::uuid,$4,$5)
RETURNING id::text
`, contacts[2], now.Add(-time.Hour), actorID, "suppression-"+contacts[2], "suppression-fingerprint-"+contacts[2]).Scan(&suppressionID); err != nil {
		t.Fatal(err)
	}

	// A prior authorised delivery for contact 3 reaches the active maxMessages=1
	// cap in the 24-hour policy window.
	if _, err := db.ExecContext(ctx, `
INSERT INTO campaigns(
 id,organisation_id,name,purpose_id,status,requested_start_at,completion_deadline_at,
 maximum_unique_recipients,consent_review_id,campaign_timezone
) VALUES($1::uuid,$2::uuid,'Prior capped campaign',$3::uuid,'COMPLETED',$4,$5,1,$6::uuid,'UTC')
`, campaignID, orgID, purposeID, now.Add(-3*time.Hour), now.Add(-time.Hour), reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO message_versions(id,campaign_id,version,message_type,body,content_hash,status,client_request_id)
VALUES($1::uuid,$2::uuid,1,'TEXT','prior send',repeat('b',64),'APPROVED',$3)
`, messageID, campaignID, "cohort-breakdown-message-"+messageID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO audience_snapshots(id,campaign_id,segment_definition,consent_policy_version,snapshot_hash,eligible_count)
VALUES($1::uuid,$2::uuid,'{}'::jsonb,'cohort-breakdown-v1',$3,1)
`, snapshotID, campaignID, "cohort-breakdown-"+snapshotID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO campaign_recipients(
 id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,authorised_at,updated_at
) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,'DELIVERED',$7,$7)
`, recipientID, campaignID, snapshotID, contacts[3], messageID, "cohort-breakdown-recipient-"+recipientID, now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}

	registry, err := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
	if err != nil {
		t.Fatal(err)
	}
	// Contacts are global, so constrain this fixture explicitly. Other tests and
	// repeated runs must not inflate its pre-consent matched-profile count.
	if err := registry.Register(audiencefilter.Definition{
		Code: "FIXTURE_CONTACT_ID", DisplayName: "Fixture contact ID",
		DataType: audiencefilter.DataTypeText, Operators: []audiencefilter.Operator{audiencefilter.OperatorIn},
		Storage: audiencefilter.StorageCoreColumn, QueryableField: "c.id",
		Core: true, Filterable: true, Active: true,
	}); err != nil {
		t.Fatal(err)
	}
	fixtureIDs := make([]any, len(contacts))
	for i, identifier := range contacts {
		fixtureIDs[i] = identifier
	}
	group := audiencefilter.Group{
		Join: audiencefilter.JoinAnd,
		Rules: []audiencefilter.Rule{
			{DefinitionCode: "REPORTED_AGE", Operator: audiencefilter.OperatorBetween, Values: []any{18, 35}},
			{DefinitionCode: "FIXTURE_CONTACT_ID", Operator: audiencefilter.OperatorIn, Values: fixtureIDs},
		},
	}
	service := NewExecutionService(NewCompiler(registry), &PostgreSQLQueryRepository{DB: db})
	estimate, err := service.Estimate(ctx, group, EligibilityContext{
		OrganisationID: orgID,
		PurposeID:      purposeID,
		Channel:        "WHATSAPP",
		AsOf:           now,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if estimate.Breakdown == nil {
		t.Fatalf("missing authoritative breakdown: %+v", estimate)
	}
	got := estimate.Breakdown
	if got.MatchedProfiles != 4 ||
		got.ConsentEligible != 3 ||
		got.ConsentExcluded != 1 ||
		got.Unsuppressed != 2 ||
		got.SuppressionExcluded != 1 ||
		got.Eligible != 1 ||
		got.FrequencyCapExcluded != 1 ||
		estimate.EligibleCount != 1 {
		t.Fatalf("unexpected eligibility waterfall: breakdown=%+v eligibleCount=%d", *got, estimate.EligibleCount)
	}

	// Estimates may execute after their durable as-of time. Authorisations newer
	// than that point must not consume its cap; the window is (asOf-24h, asOf].
	for _, test := range []struct {
		name              string
		authorisedAt      time.Time
		eligible          int64
		frequencyExcluded int64
	}{
		{"prior within window", now.Add(-2 * time.Hour), 1, 1},
		{"future authorisation", now.Add(time.Hour), 2, 0},
		{"exact lower boundary", now.Add(-24 * time.Hour), 2, 0},
		{"just inside lower boundary", now.Add(-24*time.Hour + time.Microsecond), 1, 1},
		{"before lower boundary", now.Add(-24*time.Hour - time.Microsecond), 2, 0},
		{"exact upper boundary", now, 1, 1},
		{"just after upper boundary", now.Add(time.Microsecond), 2, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := db.ExecContext(ctx, "UPDATE campaign_recipients SET authorised_at=$2 WHERE id=$1::uuid", recipientID, test.authorisedAt); err != nil {
				t.Fatal(err)
			}
			eligibility := EligibilityContext{OrganisationID: orgID, PurposeID: purposeID, Channel: "WHATSAPP", AsOf: now}
			estimate, err := service.Estimate(ctx, group, eligibility, nil)
			if err != nil {
				t.Fatal(err)
			}
			want := EligibilityBreakdown{
				MatchedProfiles: 4, ConsentEligible: 3, ConsentExcluded: 1,
				Unsuppressed: 2, SuppressionExcluded: 1,
				Eligible: test.eligible, FrequencyCapExcluded: test.frequencyExcluded,
			}
			if estimate.Breakdown == nil || *estimate.Breakdown != want || estimate.EligibleCount != test.eligible {
				t.Fatalf("as-of waterfall eligibleCount=%d breakdown=%+v want=%+v", estimate.EligibleCount, estimate.Breakdown, want)
			}
			members, err := service.Materialise(ctx, group, eligibility, nil, 10)
			if err != nil {
				t.Fatal(err)
			}
			gotIDs := make(map[string]bool, len(members))
			for _, member := range members {
				gotIDs[member.ContactID] = true
			}
			if int64(len(members)) != test.eligible || !gotIDs[contacts[0]] || gotIDs[contacts[3]] != (test.eligible == 2) {
				t.Fatalf("as-of materialised contacts=%v eligible=%d want=%d", gotIDs, len(members), test.eligible)
			}
		})
	}
}
