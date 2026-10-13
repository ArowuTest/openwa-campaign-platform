package operations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"campaign-platform/internal/identity"
)

const authenticationSnapshotVersion = "AUTHENTICATION-SNAPSHOT-V1"
const auditExportTemplateVersion = "AUDIT-EXPORT-V3"
const maxAuthenticationSnapshotBytes = 16 << 20

var errAuthenticationEventSnapshotPayloadTooLarge = errors.New("authentication event snapshot payload limit exceeded")

type frozenAuthenticationSnapshot struct {
	Version      string                         `json:"version"`
	CriteriaHash string                         `json:"criteriaHash"`
	CapturedAt   time.Time                      `json:"capturedAt"`
	Events       []AuthenticationEventExportRow `json:"events"`
}

func authenticationCriteriaHash(criteria auditExportCriteria) (string, error) {
	raw, err := json.Marshal(criteria)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:]), nil
}

func (s *Service) freezeAuthenticationEventSnapshot(ctx context.Context, criteria auditExportCriteria, asOf time.Time) (json.RawMessage, *time.Time, string, error) {
	hash, err := authenticationCriteriaHash(criteria)
	if err != nil {
		return nil, nil, "", err
	}
	query, include, err := authenticationExportQuery(criteria)
	if err != nil {
		return nil, nil, "", err
	}
	boundary := asOf.UTC().Truncate(time.Microsecond)
	to := boundary.Add(time.Microsecond)
	if query.To != nil && query.To.Before(to) {
		to = query.To.UTC()
	}
	query.To = &to
	snapshot := frozenAuthenticationSnapshot{
		Version: authenticationSnapshotVersion, CriteriaHash: hash,
		CapturedAt: boundary, Events: make([]AuthenticationEventExportRow, 0),
	}
	headID := ""
	if include && (query.From == nil || query.From.Before(to)) {
		repository, ok := s.AuthenticationEvents.(identity.AuthenticationEventSnapshotRepository)
		if !ok {
			return nil, nil, "", errors.New("authentication event snapshot repository unavailable")
		}
		events, err := repository.SnapshotAuthenticationEvents(ctx, query, maxAuthenticationExportRows)
		if err != nil {
			return nil, nil, "", err
		}
		for _, event := range events {
			snapshot.Events = append(snapshot.Events, AuthenticationEventExportRow{
				Source: "authentication", ID: event.ID, ActorID: event.UserID,
				EventType: string(event.Type), CorrelationID: event.CorrelationID,
				OccurredAt: event.OccurredAt.UTC(),
			})
		}
		if len(snapshot.Events) > 0 {
			headID = snapshot.Events[0].ID
		}
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil, nil, "", err
	}
	if len(raw) > maxAuthenticationSnapshotBytes {
		return nil, nil, "", errAuthenticationEventSnapshotPayloadTooLarge
	}
	return raw, &boundary, headID, nil
}

func loadAuthenticationEventSnapshot(job ExportRequest, criteria auditExportCriteria) ([]AuthenticationEventExportRow, error) {
	if job.TemplateVersion != auditExportTemplateVersion || len(job.FrozenPayload) == 0 {
		return nil, errors.New("authentication events were not frozen at approval; re-request export")
	}
	if len(job.FrozenPayload) > maxAuthenticationSnapshotBytes {
		return nil, errAuthenticationEventSnapshotPayloadTooLarge
	}
	var snapshot frozenAuthenticationSnapshot
	if err := json.Unmarshal(job.FrozenPayload, &snapshot); err != nil {
		return nil, errors.New("frozen authentication event snapshot is invalid")
	}
	hash, err := authenticationCriteriaHash(criteria)
	if err != nil {
		return nil, err
	}
	expectedHeadID := ""
	if len(snapshot.Events) > 0 {
		expectedHeadID = snapshot.Events[0].ID
	}
	if snapshot.Version != authenticationSnapshotVersion || snapshot.CriteriaHash != hash ||
		snapshot.Events == nil || len(snapshot.Events) > maxAuthenticationExportRows ||
		job.AuthenticationEventAsOf == nil || snapshot.CapturedAt.IsZero() ||
		!snapshot.CapturedAt.Equal(job.AuthenticationEventAsOf.UTC()) ||
		job.AuthenticationEventHeadID != expectedHeadID {
		return nil, errors.New("frozen authentication event snapshot does not match approval")
	}
	return snapshot.Events, nil
}
