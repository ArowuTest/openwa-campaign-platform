package inbound

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	sharedcrypto "campaign-platform/internal/shared/crypto"
)

type PostgreSQLRepository struct {
	DB         *sql.DB
	Secrets    *sharedcrypto.SecretBox // legacy single-key compatibility
	KeyVersion string                  // legacy single-key compatibility
	Keyring    *sharedcrypto.SecretKeyring
}

func (r *PostgreSQLRepository) Create(ctx context.Context, v Reply) (Reply, bool, error) {
	if r == nil || r.DB == nil || (r.Secrets == nil && r.Keyring == nil) {
		return Reply{}, false, errors.New("database and inbound content encryption are required")
	}
	var cipher []byte
	var keyVersion string
	var err error
	if r.Keyring != nil {
		cipher, keyVersion, err = r.Keyring.Seal("inbound-reply:"+v.ID, v.MessageText)
	} else {
		cipher, err = r.Secrets.Seal("inbound-reply:"+v.ID, v.MessageText)
		keyVersion = r.KeyVersion
		if keyVersion == "" {
			keyVersion = "v1"
		}
	}
	if err != nil {
		return Reply{}, false, fmt.Errorf("encrypt inbound content: %w", err)
	}
	result, err := r.DB.ExecContext(ctx, `INSERT INTO inbound_replies (id,event_id,recipient_id,contact_id,campaign_id,session_id,provider_message_id,message_text,message_text_cipher,content_key_version,message_fingerprint,classification,occurred_at,created_at,content_retain_until,version) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),'',$8,$9,$10,$11,$12,$13,$14,1) ON CONFLICT (event_id) DO NOTHING`, v.ID, v.EventID, v.RecipientID, v.ContactID, v.CampaignID, v.SessionID, v.ProviderMessageID, cipher, keyVersion, v.MessageFingerprint, v.Classification, v.OccurredAt, v.CreatedAt, v.ContentRetainUntil)
	if err != nil {
		return Reply{}, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return Reply{}, false, err
	}
	if rows == 1 {
		return v, true, nil
	}
	existing, err := r.getByEvent(ctx, v.EventID)
	if err != nil {
		return Reply{}, false, err
	}
	if existing.MessageFingerprint != v.MessageFingerprint || existing.RecipientID != v.RecipientID {
		return Reply{}, false, ErrReplayConflict
	}
	return existing, false, nil
}
func (r *PostgreSQLRepository) Get(ctx context.Context, id string) (Reply, error) {
	return r.scan(r.DB.QueryRowContext(ctx, selectReply+` WHERE id=$1`, id))
}
func (r *PostgreSQLRepository) getByEvent(ctx context.Context, event string) (Reply, error) {
	return r.scan(r.DB.QueryRowContext(ctx, selectReply+` WHERE event_id=$1`, event))
}
func (r *PostgreSQLRepository) List(ctx context.Context, limit int) ([]Reply, error) {
	rows, err := r.DB.QueryContext(ctx, selectReply+` ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Reply{}
	for rows.Next() {
		v, err := r.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *PostgreSQLRepository) UpdateReview(ctx context.Context, id string, expected int64, c Classification, e bool, reason, actor string, now time.Time) (Reply, error) {
	v, err := r.scan(r.DB.QueryRowContext(ctx, `UPDATE inbound_replies SET classification=$3,escalated=$4,escalation_reason=NULLIF($5,''),reviewed_by=$6,reviewed_at=$7,version=version+1 WHERE id=$1 AND version=$2 RETURNING `+replyColumns, id, expected, c, e, reason, actor, now))
	if errors.Is(err, sql.ErrNoRows) {
		var exists bool
		if er := r.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM inbound_replies WHERE id=$1)`, id).Scan(&exists); er != nil {
			return Reply{}, er
		}
		if exists {
			return Reply{}, ErrConflict
		}
		return Reply{}, ErrNotFound
	}
	return v, err
}

const replyColumns = `id,event_id,recipient_id,contact_id,campaign_id,session_id,COALESCE(provider_message_id,''),COALESCE(message_text,''),COALESCE(message_text_cipher,'\\x'::bytea),COALESCE(content_key_version,''),message_fingerprint,classification,escalated,COALESCE(escalation_reason,''),occurred_at,created_at,content_retain_until,content_redacted_at,legal_hold,COALESCE(legal_hold_reason,''),COALESCE(legal_hold_applied_by::text,''),legal_hold_applied_at,COALESCE(legal_hold_released_by::text,''),legal_hold_released_at,reviewed_at,COALESCE(reviewed_by,''),version`
const selectReply = `SELECT ` + replyColumns + ` FROM inbound_replies`

type scanner interface{ Scan(...any) error }

func (r *PostgreSQLRepository) scan(s scanner) (Reply, error) {
	var v Reply
	var legacy string
	var cipher []byte
	var keyVersion string
	err := s.Scan(&v.ID, &v.EventID, &v.RecipientID, &v.ContactID, &v.CampaignID, &v.SessionID, &v.ProviderMessageID, &legacy, &cipher, &keyVersion, &v.MessageFingerprint, &v.Classification, &v.Escalated, &v.EscalationReason, &v.OccurredAt, &v.CreatedAt, &v.ContentRetainUntil, &v.ContentRedactedAt, &v.LegalHold, &v.LegalHoldReason, &v.LegalHoldAppliedBy, &v.LegalHoldAppliedAt, &v.LegalHoldReleasedBy, &v.LegalHoldReleasedAt, &v.ReviewedAt, &v.ReviewedBy, &v.Version)
	if err != nil {
		return v, err
	}
	if v.ContentRedactedAt != nil {
		return v, nil
	}
	if len(cipher) > 0 {
		if r.Secrets == nil && r.Keyring == nil {
			return Reply{}, errors.New("inbound content encryption is not configured")
		}
		var plain string
		var err error
		if r.Keyring != nil {
			plain, err = r.Keyring.Open(keyVersion, "inbound-reply:"+v.ID, cipher)
		} else {
			plain, err = r.Secrets.Open("inbound-reply:"+v.ID, cipher)
		}
		if err != nil {
			return Reply{}, fmt.Errorf("decrypt inbound content: %w", err)
		}
		v.MessageText = plain
	} else {
		v.MessageText = legacy
	}
	return v, nil
}
func (r *PostgreSQLRepository) Summary(ctx context.Context, campaignID string) (Summary, error) {
	var s Summary
	err := r.DB.QueryRowContext(ctx, `SELECT count(*), count(*) FILTER (WHERE classification='UNREVIEWED'), count(*) FILTER (WHERE classification='OPT_OUT'), count(*) FILTER (WHERE classification='QUESTION'), count(*) FILTER (WHERE classification='COMPLAINT'), count(*) FILTER (WHERE classification='OTHER'), count(*) FILTER (WHERE escalated) FROM inbound_replies WHERE campaign_id=$1`, campaignID).Scan(&s.Total, &s.Unreviewed, &s.OptOut, &s.Questions, &s.Complaints, &s.Other, &s.Escalated)
	return s, err
}
func (r *PostgreSQLRepository) RedactExpired(ctx context.Context, now time.Time) (int64, error) {
	res, err := r.DB.ExecContext(ctx, `UPDATE inbound_replies SET message_text='',message_text_cipher=NULL,content_redacted_at=$1,version=version+1 WHERE legal_hold=false AND content_redacted_at IS NULL AND content_retain_until<=$1`, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *PostgreSQLRepository) SetLegalHold(ctx context.Context, id string, expected int64, hold bool, reason, actor string, now time.Time) (Reply, error) {
	var row *sql.Row
	if hold {
		row = r.DB.QueryRowContext(ctx, `UPDATE inbound_replies SET legal_hold=true,legal_hold_reason=$3,legal_hold_applied_by=$4::uuid,legal_hold_applied_at=$5,legal_hold_released_by=NULL,legal_hold_released_at=NULL,version=version+1 WHERE id=$1 AND version=$2 RETURNING `+replyColumns, id, expected, reason, actor, now)
	} else {
		row = r.DB.QueryRowContext(ctx, `UPDATE inbound_replies SET legal_hold=false,legal_hold_released_by=$3::uuid,legal_hold_released_at=$4,version=version+1 WHERE id=$1 AND version=$2 RETURNING `+replyColumns, id, expected, actor, now)
	}
	v, err := r.scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		var exists bool
		if er := r.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM inbound_replies WHERE id=$1)`, id).Scan(&exists); er != nil {
			return Reply{}, er
		}
		if exists {
			return Reply{}, ErrConflict
		}
		return Reply{}, ErrNotFound
	}
	return v, err
}

// ReencryptContent migrates a bounded batch to the active key version. Each
// row is updated only if its key version and record version remain unchanged,
// preventing rotation from overwriting concurrent review or legal-hold work.
func (r *PostgreSQLRepository) ReencryptContent(ctx context.Context, limit int) (int64, error) {
	if r == nil || r.DB == nil || r.Keyring == nil {
		return 0, errors.New("versioned inbound keyring is required")
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT id,message_text_cipher,content_key_version,version FROM inbound_replies WHERE content_redacted_at IS NULL AND message_text_cipher IS NOT NULL AND content_key_version<>$1 ORDER BY created_at LIMIT $2`, r.Keyring.ActiveVersion(), limit)
	if err != nil {
		return 0, err
	}
	type candidate struct {
		id      string
		cipher  []byte
		key     string
		version int64
	}
	var items []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.cipher, &c.key, &c.version); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, c)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	var count int64
	for _, c := range items {
		plain, err := r.Keyring.Open(c.key, "inbound-reply:"+c.id, c.cipher)
		if err != nil {
			return count, err
		}
		cipher, key, err := r.Keyring.Seal("inbound-reply:"+c.id, plain)
		if err != nil {
			return count, err
		}
		res, err := r.DB.ExecContext(ctx, `UPDATE inbound_replies SET message_text_cipher=$1,content_key_version=$2,version=version+1 WHERE id=$3 AND version=$4 AND content_key_version=$5`, cipher, key, c.id, c.version, c.key)
		if err != nil {
			return count, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return count, err
		}
		count += n
	}
	return count, nil
}
