package dispatch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"campaign-platform/internal/delivery"
	"campaign-platform/internal/sender"
	sharedcrypto "campaign-platform/internal/shared/crypto"
)

type ObjectResolver interface {
	Resolve(context.Context, string, time.Duration) (string, error)
}

type PostgreSQLMaterialLoader struct {
	DB          *sql.DB
	Protector   *sharedcrypto.MSISDNProtector
	Allocator   sender.Allocator
	Objects     ObjectResolver
	MediaURLTTL time.Duration
	Clock       func() time.Time
}

func (l *PostgreSQLMaterialLoader) Load(ctx context.Context, recipient delivery.Recipient) (Material, error) {
	if l == nil || l.DB == nil || l.Protector == nil || l.Allocator == nil {
		return Material{}, errors.New("material loader dependencies are required")
	}
	now := time.Now().UTC()
	if l.Clock != nil {
		now = l.Clock().UTC()
	}
	var encrypted []byte
	var messageType, body, mediaObjectKey, senderPool string
	const query = `
SELECT c.encrypted_msisdn,mv.message_type,coalesce(mv.body,''),coalesce(mv.media_object_key,''),coalesce(cp.sender_pool,'')
FROM campaign_recipients cr
JOIN contacts c ON c.id=cr.contact_id
JOIN message_versions mv ON mv.id=cr.message_version_id
JOIN campaigns cp ON cp.id=cr.campaign_id
WHERE cr.id=$1::uuid AND cr.campaign_id=$2::uuid AND cr.contact_id=$3::uuid AND cr.message_version_id=$4::uuid`
	if err := l.DB.QueryRowContext(ctx, query, recipient.ID, recipient.CampaignID, recipient.ContactID, recipient.MessageVersionID).Scan(&encrypted, &messageType, &body, &mediaObjectKey, &senderPool); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Material{}, delivery.ErrRecipientNotFound
		}
		return Material{}, fmt.Errorf("load dispatch evidence: %w", err)
	}
	sessionID, err := l.Allocator.Assign(ctx, recipient.ID, senderPool, now)
	if err != nil {
		return Material{}, fmt.Errorf("assign sender: %w", err)
	}
	var gatewayPoolID string
	if err := l.DB.QueryRowContext(ctx, `SELECT gateway_pool_id::text FROM sender_sessions WHERE id=$1::uuid`, sessionID).Scan(&gatewayPoolID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Material{}, PermanentMaterialError{Err: errors.New("assigned sender session is not governed by a gateway pool")}
		}
		return Material{}, fmt.Errorf("load gateway pool assignment: %w", err)
	}
	e164, err := l.Protector.Decrypt(encrypted)
	if err != nil {
		return Material{}, PermanentMaterialError{Err: errors.New("decrypt recipient address")}
	}
	mappedType, err := gatewayMessageType(messageType)
	if err != nil {
		return Material{}, PermanentMaterialError{Err: err}
	}
	mediaURL := ""
	if mappedType != "text" {
		if l.Objects == nil {
			return Material{}, PermanentMaterialError{Err: errors.New("media object resolver is required")}
		}
		if strings.TrimSpace(mediaObjectKey) == "" {
			return Material{}, PermanentMaterialError{Err: errors.New("approved media message has no object key")}
		}
		ttl := l.MediaURLTTL
		if ttl <= 0 || ttl > time.Hour {
			ttl = 15 * time.Minute
		}
		mediaURL, err = l.Objects.Resolve(ctx, mediaObjectKey, ttl)
		if err != nil {
			return Material{}, fmt.Errorf("resolve media object: %w", err)
		}
	}
	return Material{GatewayPoolID: gatewayPoolID, SessionID: sessionID, RecipientE164: e164, MessageType: mappedType, Body: body, MediaObjectURL: mediaURL, ClientReference: recipient.ID}, nil
}

func gatewayMessageType(value string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "TEXT":
		return "text", nil
	case "IMAGE_CAPTION", "IMAGE":
		return "image", nil
	case "VIDEO":
		return "video", nil
	case "DOCUMENT":
		return "document", nil
	default:
		return "", fmt.Errorf("unsupported approved message type %q", value)
	}
}

type PostgreSQLFinalEligibility struct{ DB *sql.DB }

func (c *PostgreSQLFinalEligibility) Check(ctx context.Context, recipient delivery.Recipient, asOf time.Time) (EligibilityDecision, error) {
	if c == nil || c.DB == nil {
		return EligibilityDecision{}, errors.New("database is required")
	}
	const query = `
WITH basis AS (
  SELECT cr.contact_id, cp.organisation_id, cp.purpose_id, cp.status AS campaign_status,
         ct.status AS contact_status, o.status AS organisation_status,
         rv.status AS review_status, rv.channel AS review_channel, rv.expires_at AS review_expires_at
  FROM campaign_recipients cr
  JOIN contacts ct ON ct.id=cr.contact_id
  JOIN campaigns cp ON cp.id=cr.campaign_id
  JOIN organisations o ON o.id=cp.organisation_id
  JOIN consent_reviews rv ON rv.id=cp.consent_review_id
  WHERE cr.id=$1::uuid
), latest_grant AS (
  SELECT cg.status, cg.expires_at
  FROM consent_grants cg, basis b
  WHERE cg.contact_id=b.contact_id
    AND cg.organisation_id=b.organisation_id
    AND cg.purpose_id=b.purpose_id
    AND upper(cg.channel)='WHATSAPP'
    AND cg.effective_from <= $2
  ORDER BY cg.effective_from DESC, cg.created_at DESC, cg.id DESC
  LIMIT 1
)
SELECT CASE
  WHEN b.contact_status <> 'ACTIVE' THEN 'CONTACT_INACTIVE'
  WHEN b.organisation_status <> 'ACTIVE' THEN 'ORGANISATION_INACTIVE'
  WHEN b.campaign_status NOT IN ('SCHEDULED','DISPATCHING') THEN 'CAMPAIGN_NOT_DISPATCHABLE'
  WHEN b.review_status <> 'APPROVED' OR b.review_expires_at <= $2 OR upper(b.review_channel) <> 'WHATSAPP' THEN 'CONSENT_REVIEW_INVALID'
  WHEN EXISTS (
    SELECT 1 FROM suppressions sp
    WHERE sp.contact_id=b.contact_id AND sp.active AND sp.effective_at<=$2
      AND (sp.expires_at IS NULL OR sp.expires_at>$2)
      AND (sp.scope='GLOBAL'
        OR (sp.scope='ORGANISATION' AND sp.organisation_id=b.organisation_id)
        OR (sp.scope='PURPOSE' AND sp.purpose_id=b.purpose_id)
        OR (sp.scope='CHANNEL' AND upper(sp.channel)='WHATSAPP')
        OR (sp.scope='TEMPORARY'
           AND (sp.organisation_id IS NULL OR sp.organisation_id=b.organisation_id)
           AND (sp.purpose_id IS NULL OR sp.purpose_id=b.purpose_id)
           AND (sp.channel IS NULL OR upper(sp.channel)='WHATSAPP')))
  ) THEN 'SUPPRESSED'
  WHEN EXISTS (
    SELECT 1
    FROM organisation_policy_versions op
    CROSS JOIN LATERAL jsonb_to_recordset(op.frequency_caps)
      AS fc("purposeId" text, "channel" text, "maxMessages" integer, "windowHours" integer)
    WHERE op.organisation_id=b.organisation_id
      AND op.status='ACTIVE'
      AND op.effective_from<=$2
      AND (op.effective_to IS NULL OR op.effective_to>$2)
      AND (coalesce(fc."purposeId", '')='' OR fc."purposeId"=b.purpose_id)
      AND upper(fc."channel")='WHATSAPP'
      AND (
        SELECT count(*)
        FROM campaign_recipients recent
        JOIN campaigns recent_campaign ON recent_campaign.id=recent.campaign_id
        WHERE recent.contact_id=b.contact_id
          AND recent_campaign.organisation_id=b.organisation_id
          AND recent_campaign.purpose_id=b.purpose_id
          AND recent.status NOT IN ('CANCELLED','SUPPRESSED_BEFORE_SEND')
          AND recent.authorised_at>$2-make_interval(hours=>fc."windowHours")
      )>=fc."maxMessages"
  ) THEN 'FREQUENCY_CAPPED'
  WHEN NOT EXISTS (SELECT 1 FROM latest_grant) THEN 'NO_ACTIVE_CONSENT'
  WHEN (SELECT status FROM latest_grant)='WITHDRAWN' THEN 'CONSENT_WITHDRAWN'
  WHEN (SELECT status FROM latest_grant)='REVOKED' THEN 'CONSENT_REVOKED'
  WHEN (SELECT status FROM latest_grant)='EXPIRED'
    OR ((SELECT status FROM latest_grant)='ACTIVE'
        AND (SELECT expires_at FROM latest_grant) IS NOT NULL
        AND (SELECT expires_at FROM latest_grant) <= $2) THEN 'CONSENT_EXPIRED'
  WHEN (SELECT status FROM latest_grant) <> 'ACTIVE' THEN 'NO_ACTIVE_CONSENT'
  ELSE ''
END AS exclusion_reason
FROM basis b`
	var reason string
	err := c.DB.QueryRowContext(ctx, query, recipient.ID, asOf.UTC()).Scan(&reason)
	if errors.Is(err, sql.ErrNoRows) {
		return EligibilityDecision{Eligible: false, Reason: "RECIPIENT_NOT_FOUND"}, nil
	}
	if err != nil {
		return EligibilityDecision{}, err
	}
	reason = strings.TrimSpace(reason)
	if reason != "" {
		return EligibilityDecision{Eligible: false, Reason: reason}, nil
	}
	return EligibilityDecision{Eligible: true}, nil
}
