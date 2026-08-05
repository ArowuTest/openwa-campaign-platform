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
SELECT ct.status='ACTIVE'
 AND cp.status IN ('SCHEDULED','DISPATCHING')
 AND EXISTS (
   SELECT 1 FROM consent_grants cg
   WHERE cg.contact_id=cr.contact_id AND cg.organisation_id=cp.organisation_id
     AND cg.purpose_id=cp.purpose_id AND upper(cg.channel)='WHATSAPP'
     AND cg.status='ACTIVE' AND cg.granted_at<=$2
     AND (cg.expires_at IS NULL OR cg.expires_at>$2)
 )
 AND NOT EXISTS (
   SELECT 1 FROM suppressions sp
   WHERE sp.contact_id=cr.contact_id AND sp.active AND sp.effective_at<=$2
     AND (sp.expires_at IS NULL OR sp.expires_at>$2)
     AND (sp.scope='GLOBAL'
       OR (sp.scope='ORGANISATION' AND sp.organisation_id=cp.organisation_id)
       OR (sp.scope='PURPOSE' AND sp.purpose_id=cp.purpose_id)
       OR (sp.scope='CHANNEL' AND upper(sp.channel)='WHATSAPP')
       OR (sp.scope='TEMPORARY'
          AND (sp.organisation_id IS NULL OR sp.organisation_id=cp.organisation_id)
          AND (sp.purpose_id IS NULL OR sp.purpose_id=cp.purpose_id)
          AND (sp.channel IS NULL OR upper(sp.channel)='WHATSAPP')))
 ) AS eligible
FROM campaign_recipients cr
JOIN contacts ct ON ct.id=cr.contact_id
JOIN campaigns cp ON cp.id=cr.campaign_id
WHERE cr.id=$1::uuid`
	var eligible bool
	err := c.DB.QueryRowContext(ctx, query, recipient.ID, asOf.UTC()).Scan(&eligible)
	if errors.Is(err, sql.ErrNoRows) {
		return EligibilityDecision{Eligible: false, Reason: "RECIPIENT_NOT_FOUND"}, nil
	}
	if err != nil {
		return EligibilityDecision{}, err
	}
	if !eligible {
		return EligibilityDecision{Eligible: false, Reason: "INELIGIBLE_FINAL_CHECK"}, nil
	}
	return EligibilityDecision{Eligible: true}, nil
}
