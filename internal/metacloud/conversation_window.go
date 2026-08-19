package metacloud

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

var (
	ErrConversationWindowConflict = errors.New("Meta conversation-window provider evidence conflicts with an existing observation")
	ErrConversationWindowInvalid  = errors.New("Meta conversation-window observation is invalid")
)

type ConversationWindowObservation struct {
	MetaSenderID      string
	ContactID         string
	ProviderMessageID string
	OccurredAt        time.Time
}

type ConversationWindowEvidence struct {
	MetaSenderID      string
	ContactID         string
	ProviderMessageID string
	OccurredAt        time.Time
	EligibleUntil     time.Time
	RecordedAt        time.Time
}

type PostgreSQLConversationWindowStore struct{ DB *sql.DB }

func (s *PostgreSQLConversationWindowStore) Current(ctx context.Context, metaSenderID, contactID string) (ConversationWindowEvidence, bool, error) {
	if s == nil || s.DB == nil || strings.TrimSpace(metaSenderID) == "" || strings.TrimSpace(contactID) == "" {
		return ConversationWindowEvidence{}, false, ErrConversationWindowInvalid
	}
	const query = `
SELECT meta_sender_id::text,contact_id::text,source_provider_message_id,
       inbound_occurred_at,eligible_until,recorded_at
FROM meta_cloud_conversation_windows
WHERE meta_sender_id=$1::uuid AND contact_id=$2::uuid
ORDER BY inbound_occurred_at DESC,source_provider_message_id DESC
LIMIT 1`
	var out ConversationWindowEvidence
	err := s.DB.QueryRowContext(ctx, query, strings.TrimSpace(metaSenderID), strings.TrimSpace(contactID)).Scan(
		&out.MetaSenderID, &out.ContactID, &out.ProviderMessageID,
		&out.OccurredAt, &out.EligibleUntil, &out.RecordedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ConversationWindowEvidence{}, false, nil
	}
	if err != nil {
		return ConversationWindowEvidence{}, false, err
	}
	return normalizeConversationWindowEvidence(out), true, nil
}

func (s *PostgreSQLConversationWindowStore) ObserveInbound(ctx context.Context, observation ConversationWindowObservation, window time.Duration) (ConversationWindowEvidence, bool, error) {
	if s == nil || s.DB == nil {
		return ConversationWindowEvidence{}, false, ErrConversationWindowInvalid
	}
	senderID := strings.TrimSpace(observation.MetaSenderID)
	contactID := strings.TrimSpace(observation.ContactID)
	providerMessageID := strings.TrimSpace(observation.ProviderMessageID)
	occurredAt := observation.OccurredAt.UTC().Round(time.Microsecond)
	if senderID == "" || contactID == "" || providerMessageID == "" || len(providerMessageID) > 512 || occurredAt.IsZero() || window <= 0 {
		return ConversationWindowEvidence{}, false, ErrConversationWindowInvalid
	}
	eligibleUntil := occurredAt.Add(window).Round(time.Microsecond)
	if !eligibleUntil.After(occurredAt) {
		return ConversationWindowEvidence{}, false, ErrConversationWindowInvalid
	}
	if replayed, retained, err := s.retainedReplay(ctx, senderID, contactID, providerMessageID, occurredAt); err != nil {
		return ConversationWindowEvidence{}, false, err
	} else if retained {
		return replayed, false, nil
	}

	const insert = `
INSERT INTO meta_cloud_conversation_windows(
    meta_sender_id,contact_id,source_provider_message_id,inbound_occurred_at,eligible_until
) VALUES($1::uuid,$2::uuid,$3,$4,$5)
ON CONFLICT (meta_sender_id,source_provider_message_id) DO NOTHING`
	result, err := s.DB.ExecContext(ctx, insert, senderID, contactID, providerMessageID, occurredAt, eligibleUntil)
	if err != nil {
		if replayed, retained, lookupErr := s.retainedReplay(ctx, senderID, contactID, providerMessageID, occurredAt); lookupErr != nil {
			return ConversationWindowEvidence{}, false, lookupErr
		} else if retained {
			return replayed, false, nil
		}
		return ConversationWindowEvidence{}, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return ConversationWindowEvidence{}, false, err
	}
	inserted := rows == 1
	if !inserted {
		if replayed, retained, lookupErr := s.retainedReplay(ctx, senderID, contactID, providerMessageID, occurredAt); lookupErr != nil {
			return ConversationWindowEvidence{}, false, lookupErr
		} else if retained {
			return replayed, false, nil
		}
		return ConversationWindowEvidence{}, false, ErrConversationWindowConflict
	}
	current, ok, err := s.Current(ctx, senderID, contactID)
	if err != nil {
		return ConversationWindowEvidence{}, false, err
	}
	if !ok {
		return ConversationWindowEvidence{}, false, ErrConversationWindowInvalid
	}
	changed := current.ProviderMessageID == providerMessageID && current.OccurredAt.Equal(occurredAt)
	return current, changed, nil
}

func (s *PostgreSQLConversationWindowStore) retainedReplay(ctx context.Context, senderID, contactID, providerMessageID string, occurredAt time.Time) (ConversationWindowEvidence, bool, error) {
	existing, ok, err := s.byRetainedSource(ctx, senderID, providerMessageID)
	if err != nil {
		return ConversationWindowEvidence{}, false, err
	}
	if !ok {
		return ConversationWindowEvidence{}, false, nil
	}
	if !strings.EqualFold(existing.ContactID, contactID) || !existing.OccurredAt.Equal(occurredAt) {
		return ConversationWindowEvidence{}, false, ErrConversationWindowConflict
	}
	current, currentOK, currentErr := s.Current(ctx, senderID, contactID)
	if currentErr != nil {
		return ConversationWindowEvidence{}, false, currentErr
	}
	if currentOK {
		return current, true, nil
	}
	return existing, true, nil
}
func (s *PostgreSQLConversationWindowStore) byRetainedSource(ctx context.Context, metaSenderID, providerMessageID string) (ConversationWindowEvidence, bool, error) {
	const query = `
SELECT meta_sender_id::text,contact_id::text,source_provider_message_id,
       inbound_occurred_at,eligible_until,recorded_at
FROM (
    SELECT meta_sender_id,contact_id,source_provider_message_id,
           inbound_occurred_at,eligible_until,recorded_at,0 AS retained_priority
    FROM meta_cloud_conversation_windows
    UNION ALL
    SELECT meta_sender_id,contact_id,source_provider_message_id,
           inbound_occurred_at,eligible_until,recorded_at,1 AS retained_priority
    FROM meta_cloud_conversation_window_tombstones
    UNION ALL
    SELECT meta_sender_id,contact_id,source_provider_message_id,
           inbound_occurred_at,eligible_until,recorded_at,2 AS retained_priority
    FROM meta_cloud_conversation_window_quarantine
) retained
WHERE meta_sender_id=$1::uuid AND source_provider_message_id=$2
ORDER BY retained_priority
LIMIT 1`
	var out ConversationWindowEvidence
	err := s.DB.QueryRowContext(ctx, query, metaSenderID, providerMessageID).Scan(
		&out.MetaSenderID, &out.ContactID, &out.ProviderMessageID,
		&out.OccurredAt, &out.EligibleUntil, &out.RecordedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ConversationWindowEvidence{}, false, nil
	}
	if err != nil {
		return ConversationWindowEvidence{}, false, err
	}
	return normalizeConversationWindowEvidence(out), true, nil
}
func normalizeConversationWindowEvidence(value ConversationWindowEvidence) ConversationWindowEvidence {
	value.MetaSenderID = strings.TrimSpace(value.MetaSenderID)
	value.ContactID = strings.TrimSpace(value.ContactID)
	value.ProviderMessageID = strings.TrimSpace(value.ProviderMessageID)
	value.OccurredAt = value.OccurredAt.UTC()
	value.EligibleUntil = value.EligibleUntil.UTC()
	value.RecordedAt = value.RecordedAt.UTC()
	return value
}
