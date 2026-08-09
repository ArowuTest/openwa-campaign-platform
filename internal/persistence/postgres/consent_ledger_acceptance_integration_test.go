package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/consent"
	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLConsentLedgerPersistsScopedEvidenceWithdrawalAndEvents(t *testing.T) {
	dsn := os.Getenv("POSTGRES_PAGINATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_PAGINATION_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	actor := complianceUUID(t, ctx, db)
	org := complianceUUID(t, ctx, db)
	contact := complianceUUID(t, ctx, db)
	review := complianceUUID(t, ctx, db)
	now := time.Now().UTC().Truncate(time.Second)
	if _, err = db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Consent Ledger Actor','DISABLED',false)`, actor, "consent-ledger-"+actor+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, org, "Consent Ledger Org "+org); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,$2,$3,'+234******001','ACTIVE',$4)`, contact, []byte("cipher-"+contact), []byte("hmac-"+contact), now); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,status,created_by,version,created_at,updated_at) VALUES($1::uuid,$2::uuid,'Consent Ledger Review','WHATSAPP','WEB_FORM','v1','DRAFT',$3::uuid,1,$4,$4)`, review, org, actor, now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	repo := &ConsentLedgerRepository{DB: db}
	svc := consent.NewLedgerService(repo)
	expiry := now.Add(2 * time.Hour)
	first, created, err := svc.CreateGrant(ctx, consent.GrantInput{
		ContactID: contact, OrganisationID: org, PurposeID: "PROMOTIONS", Channel: "WHATSAPP", ConsentReviewID: review,
		WordingVersion: "v1", SourceType: "WEB_FORM", SourceReference: "form:123", EvidenceObjectKey: "consent/evidence/123",
		EvidenceChecksum: "abc123", EffectiveFrom: ptrTime(now.Add(-time.Hour)), GrantedAt: ptrTime(now.Add(-time.Hour)), ExpiresAt: &expiry,
		Status: consent.GrantActive, CreatedBy: actor, ClientRequestID: "grant-1-" + contact,
	})
	if err != nil || !created {
		t.Fatalf("first grant created=%v err=%v", created, err)
	}
	second, created, err := svc.CreateGrant(ctx, consent.GrantInput{
		ContactID: contact, OrganisationID: org, PurposeID: "SERVICE", Channel: "WHATSAPP", ConsentReviewID: review,
		WordingVersion: "v2", SourceType: "CALL_CENTRE", SourceReference: "call:456", EvidenceChecksum: "def456",
		EffectiveFrom: ptrTime(now.Add(-30 * time.Minute)), GrantedAt: ptrTime(now.Add(-30 * time.Minute)), Status: consent.GrantActive,
		CreatedBy: actor, ClientRequestID: "grant-2-" + contact,
	})
	if err != nil || !created || second.ID == first.ID {
		t.Fatalf("second=%+v created=%v err=%v", second, created, err)
	}

	var scopes int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM consent_grants WHERE contact_id=$1::uuid AND status='ACTIVE'`, contact).Scan(&scopes); err != nil {
		t.Fatal(err)
	}
	if scopes != 2 {
		t.Fatalf("active scoped grants=%d want=2", scopes)
	}
	var wording, sourceType, sourceRef, evidenceKey, checksum string
	var effective, granted time.Time
	if err = db.QueryRowContext(ctx, `SELECT wording_version,source_type,source_reference,evidence_object_key,evidence_checksum,effective_from,granted_at FROM consent_grants WHERE id=$1::uuid`, first.ID).Scan(&wording, &sourceType, &sourceRef, &evidenceKey, &checksum, &effective, &granted); err != nil {
		t.Fatal(err)
	}
	if wording != "v1" || sourceType != "WEB_FORM" || sourceRef != "form:123" || evidenceKey != "consent/evidence/123" || checksum != "abc123" || !effective.Equal(now.Add(-time.Hour)) || !granted.Equal(now.Add(-time.Hour)) {
		t.Fatalf("stored consent evidence mismatch")
	}

	withdrawn, err := svc.Withdraw(ctx, first.ID, consent.WithdrawInput{ActorID: actor, Reason: "customer STOP", SourceReference: "inbound:stop", ExpectedVersion: first.Version})
	if err != nil || withdrawn.Status != consent.GrantWithdrawn {
		t.Fatalf("withdrawn=%+v err=%v", withdrawn, err)
	}
	blocked := consent.EvaluateEligibility(consent.EligibilityInput{
		ContactActive: true, OrganisationID: org, PurposeID: "PROMOTIONS", Channel: "WHATSAPP", AsOf: now.Add(time.Minute),
		Grants: []consent.Grant{withdrawn, second},
	})
	if blocked.Eligible || blocked.Reason != "CONSENT_WITHDRAWN" {
		t.Fatalf("withdrawal precedence=%+v", blocked)
	}

	reEffective := now.Add(2 * time.Minute)
	reconsent, created, err := svc.CreateGrant(ctx, consent.GrantInput{
		ContactID: contact, OrganisationID: org, PurposeID: "PROMOTIONS", Channel: "WHATSAPP", ConsentReviewID: review,
		WordingVersion: "v2", SourceType: "WEB_FORM", SourceReference: "form:789", EvidenceObjectKey: "consent/evidence/789",
		EvidenceChecksum: "ghi789", EffectiveFrom: &reEffective, GrantedAt: &reEffective, Status: consent.GrantActive,
		CreatedBy: actor, ClientRequestID: "grant-3-" + contact,
	})
	if err != nil || !created {
		t.Fatalf("reconsent=%+v created=%v err=%v", reconsent, created, err)
	}
	if reconsent.EvidenceChecksum == first.EvidenceChecksum || !reconsent.GrantedAt.After(first.GrantedAt) {
		t.Fatal("re-consent did not carry new evidence and timestamp")
	}
	eligible := consent.EvaluateEligibility(consent.EligibilityInput{
		ContactActive: true, OrganisationID: org, PurposeID: "PROMOTIONS", Channel: "WHATSAPP", AsOf: reEffective.Add(time.Second),
		Grants: []consent.Grant{withdrawn, reconsent},
	})
	if !eligible.Eligible || eligible.GrantID != reconsent.ID {
		t.Fatalf("later reconsent=%+v", eligible)
	}
	events, err := svc.Events(ctx, contact, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Fatalf("consent events=%d want=4: %+v", len(events), events)
	}
	seenCreated, seenWithdrawn := 0, 0
	for _, event := range events {
		switch event.EventType {
		case "GRANT_CREATED":
			seenCreated++
		case "GRANT_WITHDRAWN":
			seenWithdrawn++
		}
	}
	if seenCreated != 3 || seenWithdrawn != 1 {
		t.Fatalf("event types created=%d withdrawn=%d", seenCreated, seenWithdrawn)
	}
	if _, err = db.ExecContext(ctx, `UPDATE consent_events SET reason='tampered' WHERE id=$1::uuid`, events[0].ID); err == nil {
		t.Fatal("append-only consent event accepted UPDATE")
	}
	if _, err = db.ExecContext(ctx, `DELETE FROM consent_events WHERE id=$1::uuid`, events[0].ID); err == nil {
		t.Fatal("append-only consent event accepted DELETE")
	}

	var status string
	var version int64
	if err = db.QueryRowContext(ctx, `SELECT status,version FROM consent_grants WHERE id=$1::uuid`, first.ID).Scan(&status, &version); err != nil {
		t.Fatal(err)
	}
	if status != "WITHDRAWN" || version != 2 {
		t.Fatalf("withdrawn grant status=%s version=%d", status, version)
	}
}

func ptrTime(v time.Time) *time.Time { return &v }
