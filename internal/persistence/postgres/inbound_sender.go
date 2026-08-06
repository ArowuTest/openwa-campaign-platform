package postgres

import (
	"context"
	"database/sql"
	"errors"

	"campaign-platform/internal/consent"
	"campaign-platform/internal/delivery"
)

type InboundSenderResolver struct{ DB *sql.DB }

func (r *InboundSenderResolver) ResolveInboundSender(ctx context.Context, lookupHMAC []byte, sessionID string) (consent.InboundRecipientEvidence, error) {
	if r == nil || r.DB == nil || len(lookupHMAC) == 0 {
		return consent.InboundRecipientEvidence{}, errors.New("inbound sender resolver is not configured")
	}
	const query = `
SELECT cr.id::text,cr.contact_id::text,cr.campaign_id::text
FROM contacts c
JOIN campaign_recipients cr ON cr.contact_id=c.id
WHERE c.msisdn_lookup_hmac=$1
  AND ($2='' OR coalesce(cr.assigned_sender_id::text,'')=$2)
  AND cr.status IN ('GATEWAY_ACCEPTED','SENT','DELIVERED','READ','FAILED','UNKNOWN')
ORDER BY coalesce(cr.last_event_at,cr.submitted_at,cr.updated_at) DESC,cr.id DESC
LIMIT 1`
	var out consent.InboundRecipientEvidence
	err := r.DB.QueryRowContext(ctx, query, lookupHMAC, sessionID).Scan(&out.RecipientID, &out.ContactID, &out.CampaignID)
	if errors.Is(err, sql.ErrNoRows) {
		return consent.InboundRecipientEvidence{}, delivery.ErrRecipientNotFound
	}
	return out, err
}
