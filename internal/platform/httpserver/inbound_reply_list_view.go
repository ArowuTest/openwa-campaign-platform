package httpserver

import (
	"time"

	"campaign-platform/internal/inbound"
)

// inboundReplyListView is the explicit metadata contract for inbound.read.
// Do not embed inbound.Reply or add free-text/identity fields here; that would
// silently expose private content and governed case evidence to list readers.
// Reply bodies/digests are available only through separately authorised and
// audited /content, not the work queue. ContactID is an internal contact
// reference required for the independently authorised consent.write workflow.
type inboundReplyListView struct {
	ID                 string                 `json:"id"`
	EventID            string                 `json:"eventId"`
	ContactID          string                 `json:"contactId"`
	CampaignID         string                 `json:"campaignId"`
	SessionID          string                 `json:"sessionId"`
	Classification     inbound.Classification `json:"classification"`
	Escalated          bool                   `json:"escalated"`
	OccurredAt         time.Time              `json:"occurredAt"`
	CreatedAt          time.Time              `json:"createdAt"`
	ContentRetainUntil time.Time              `json:"contentRetainUntil"`
	ContentRedactedAt  *time.Time             `json:"contentRedactedAt,omitempty"`
	LegalHold          bool                   `json:"legalHold"`
	ReviewedAt         *time.Time             `json:"reviewedAt,omitempty"`
	Version            int64                  `json:"version"`
}

// Every inbound.Read and mutation response that does not explicitly perform
// a governed content reveal uses this single, allowlisted representation.
func inboundReplyMetadataView(item inbound.Reply) inboundReplyListView {
	return inboundReplyListView{
		ID:                 item.ID,
		EventID:            item.EventID,
		ContactID:          item.ContactID,
		CampaignID:         item.CampaignID,
		SessionID:          item.SessionID,
		Classification:     item.Classification,
		Escalated:          item.Escalated,
		OccurredAt:         item.OccurredAt,
		CreatedAt:          item.CreatedAt,
		ContentRetainUntil: item.ContentRetainUntil,
		ContentRedactedAt:  item.ContentRedactedAt,
		LegalHold:          item.LegalHold,
		ReviewedAt:         item.ReviewedAt,
		Version:            item.Version,
	}
}

func inboundReplyListViews(items []inbound.Reply) []inboundReplyListView {
	out := make([]inboundReplyListView, 0, len(items))
	for _, item := range items {
		out = append(out, inboundReplyMetadataView(item))
	}
	return out
}
