package cohort

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/jobs"
)

const EstimateJobType = "COHORT_ESTIMATE"

var (
	ErrEstimateNotFound       = errors.New("cohort estimate job not found")
	ErrEstimateReplayConflict = errors.New("cohort estimate request key was reused with different content")
	ErrInvalidEstimateCursor  = errors.New("invalid cohort estimate pagination cursor")
)

type EstimateJobRequest struct {
	OrganisationID  string
	PurposeID       string
	Channel         string
	Definition      audiencefilter.Group
	RequestedBy     string
	ClientRequestID string
}

type EstimateGovernanceEvidence struct {
	ConsentReviewID           string `json:"consentReviewId,omitempty"`
	ConsentReviewVersion      int64  `json:"consentReviewVersion,omitempty"`
	ConsentWordingVersion     string `json:"consentWordingVersion,omitempty"`
	OrganisationPolicyID      string `json:"organisationPolicyId,omitempty"`
	OrganisationPolicyVersion int64  `json:"organisationPolicyVersion,omitempty"`
}

type EstimateJobRecord struct {
	ID                 string                     `json:"id"`
	OrganisationID     string                     `json:"organisationId"`
	PurposeID          string                     `json:"purposeId"`
	Channel            string                     `json:"channel"`
	Definition         audiencefilter.Group       `json:"definition"`
	AsOf               time.Time                  `json:"asOf"`
	RequestedBy        string                     `json:"requestedBy"`
	ClientRequestID    string                     `json:"clientRequestId"`
	RequestFingerprint string                     `json:"requestFingerprint"`
	Job                jobs.Job                   `json:"job"`
	Result             *Estimate                  `json:"result,omitempty"`
	Evidence           EstimateGovernanceEvidence `json:"evidence,omitempty"`
	CreatedAt          time.Time                  `json:"createdAt"`
	UpdatedAt          time.Time                  `json:"updatedAt"`
}

type EstimateJobPage struct {
	Items      []EstimateJobRecord `json:"items"`
	NextCursor string              `json:"nextCursor,omitempty"`
}

type estimateJobPayload struct {
	EstimateID string
}

type estimateCursor struct {
	CreatedAt time.Time
	ID        string
}

func normalizeEstimateRequest(input EstimateJobRequest) (EstimateJobRequest, error) {
	input.OrganisationID = strings.TrimSpace(input.OrganisationID)
	input.PurposeID = strings.TrimSpace(input.PurposeID)
	input.Channel = strings.ToUpper(strings.TrimSpace(input.Channel))
	input.RequestedBy = strings.TrimSpace(input.RequestedBy)
	input.ClientRequestID = strings.TrimSpace(input.ClientRequestID)
	if input.OrganisationID == "" || input.PurposeID == "" || input.RequestedBy == "" {
		return EstimateJobRequest{}, errors.New("organisation, purpose and requester are required")
	}
	if input.Channel == "" {
		input.Channel = "WHATSAPP"
	}
	if input.Channel != "WHATSAPP" {
		return EstimateJobRequest{}, errors.New("cohort estimate channel must be WHATSAPP")
	}
	if len(input.ClientRequestID) < 8 || len(input.ClientRequestID) > 200 {
		return EstimateJobRequest{}, errors.New("cohort estimate client request ID must contain 8-200 characters")
	}
	return input, nil
}

func estimateRequestFingerprint(input EstimateJobRequest) (string, error) {
	payload, err := json.Marshal(struct {
		OrganisationID string
		PurposeID      string
		Channel        string
		Definition     audiencefilter.Group
		RequestedBy    string
	}{
		OrganisationID: input.OrganisationID,
		PurposeID:      input.PurposeID,
		Channel:        input.Channel,
		Definition:     input.Definition,
		RequestedBy:    input.RequestedBy,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte("cohort-estimate-request-v1\x00"), payload...))
	return hex.EncodeToString(sum[:]), nil
}

func estimateDedupKey(input EstimateJobRequest) string {
	sum := sha256.Sum256([]byte(input.OrganisationID + "\x00" + input.ClientRequestID))
	return "cohort-estimate:" + hex.EncodeToString(sum[:])
}

type MemoryEstimateRepository struct {
	mu        sync.Mutex
	queue     *jobs.MemoryRepository
	items     map[string]EstimateJobRecord
	byRequest map[string]string
}

func NewMemoryEstimateRepository(queue *jobs.MemoryRepository) *MemoryEstimateRepository {
	if queue == nil {
		queue = jobs.NewMemoryRepository()
	}
	return &MemoryEstimateRepository{queue: queue, items: map[string]EstimateJobRecord{}, byRequest: map[string]string{}}
}

func (r *MemoryEstimateRepository) Schedule(ctx context.Context, input EstimateJobRequest, now time.Time) (EstimateJobRecord, bool, error) {
	if r == nil || r.queue == nil {
		return EstimateJobRecord{}, false, errors.New("cohort estimate repository is required")
	}
	input, err := normalizeEstimateRequest(input)
	if err != nil {
		return EstimateJobRecord{}, false, err
	}
	fingerprint, err := estimateRequestFingerprint(input)
	if err != nil {
		return EstimateJobRecord{}, false, err
	}
	requestKey := input.OrganisationID + "\x1f" + input.ClientRequestID

	r.mu.Lock()
	if identifier, ok := r.byRequest[requestKey]; ok {
		existing := r.items[identifier]
		r.mu.Unlock()
		if existing.RequestFingerprint != fingerprint {
			return EstimateJobRecord{}, false, ErrEstimateReplayConflict
		}
		job, err := r.queue.Get(ctx, identifier)
		if err != nil {
			return EstimateJobRecord{}, false, err
		}
		existing.Job = job
		return cloneEstimateRecord(existing), false, nil
	}
	r.mu.Unlock()

	job, err := jobs.NewJob(jobs.EnqueueInput{
		Type:        EstimateJobType,
		DedupKey:    estimateDedupKey(input),
		Payload:     estimateJobPayload{},
		MaxAttempts: 5,
		AvailableAt: now.UTC(),
	}, now.UTC())
	if err != nil {
		return EstimateJobRecord{}, false, err
	}
	job.Payload = json.RawMessage("{}")
	enqueued, created, err := r.queue.Enqueue(ctx, job)
	if err != nil {
		return EstimateJobRecord{}, false, err
	}
	if !created {
		r.mu.Lock()
		existing, ok := r.items[enqueued.ID]
		r.mu.Unlock()
		if !ok {
			return EstimateJobRecord{}, false, errors.New("durable cohort estimate job exists without estimate evidence")
		}
		if existing.RequestFingerprint != fingerprint {
			return EstimateJobRecord{}, false, ErrEstimateReplayConflict
		}
		existing.Job = enqueued
		return cloneEstimateRecord(existing), false, nil
	}

	record := EstimateJobRecord{
		ID:                 enqueued.ID,
		OrganisationID:     input.OrganisationID,
		PurposeID:          input.PurposeID,
		Channel:            input.Channel,
		Definition:         input.Definition,
		AsOf:               now.UTC(),
		RequestedBy:        input.RequestedBy,
		ClientRequestID:    input.ClientRequestID,
		RequestFingerprint: fingerprint,
		Job:                enqueued,
		CreatedAt:          now.UTC(),
		UpdatedAt:          now.UTC(),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[record.ID] = cloneEstimateRecord(record)
	r.byRequest[requestKey] = record.ID
	return cloneEstimateRecord(record), true, nil
}

func (r *MemoryEstimateRepository) Get(ctx context.Context, identifier string) (EstimateJobRecord, error) {
	if r == nil || r.queue == nil {
		return EstimateJobRecord{}, errors.New("cohort estimate repository is required")
	}
	r.mu.Lock()
	record, ok := r.items[strings.TrimSpace(identifier)]
	r.mu.Unlock()
	if !ok {
		return EstimateJobRecord{}, ErrEstimateNotFound
	}
	job, err := r.queue.Get(ctx, record.ID)
	if err != nil {
		return EstimateJobRecord{}, err
	}
	record.Job = job
	return cloneEstimateRecord(record), nil
}

func (r *MemoryEstimateRepository) StoreResult(ctx context.Context, identifier, owner string, leaseVersion int64, result Estimate, evidence EstimateGovernanceEvidence, now time.Time) error {
	if r == nil || r.queue == nil {
		return errors.New("cohort estimate repository is required")
	}
	job, err := r.queue.Get(ctx, identifier)
	if err != nil {
		return err
	}
	if job.Status != jobs.StatusProcessing || job.LeaseOwner != owner || job.LeaseVersion != leaseVersion ||
		job.LeaseExpiresAt == nil || !job.LeaseExpiresAt.After(now) {
		return jobs.ErrLeaseConflict
	}
	if err := validateEstimateEvidence(result, evidence); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.items[identifier]
	if !ok {
		return ErrEstimateNotFound
	}
	copied := cloneEstimate(result)
	record.Result = &copied
	record.Evidence = evidence
	record.UpdatedAt = now.UTC()
	r.items[identifier] = record
	return nil
}

func validateEstimateEvidence(result Estimate, evidence EstimateGovernanceEvidence) error {
	if result.CalculatedAt.IsZero() || result.EligibleCount < 0 || result.Breakdown == nil {
		return errors.New("complete cohort estimate result and breakdown are required")
	}
	if strings.TrimSpace(evidence.ConsentReviewID) == "" || evidence.ConsentReviewVersion <= 0 ||
		strings.TrimSpace(evidence.ConsentWordingVersion) == "" ||
		strings.TrimSpace(evidence.OrganisationPolicyID) == "" || evidence.OrganisationPolicyVersion <= 0 {
		return errors.New("cohort estimate governance evidence is incomplete")
	}
	return nil
}

func (r *MemoryEstimateRepository) ListPage(ctx context.Context, organisationID string, limit int, cursor string) (EstimateJobPage, error) {
	if r == nil || r.queue == nil {
		return EstimateJobPage{}, errors.New("cohort estimate repository is required")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	before, beforeID, err := decodeEstimateCursor(cursor)
	if err != nil {
		return EstimateJobPage{}, err
	}
	r.mu.Lock()
	items := make([]EstimateJobRecord, 0, len(r.items))
	for _, record := range r.items {
		if strings.TrimSpace(organisationID) != "" && record.OrganisationID != strings.TrimSpace(organisationID) {
			continue
		}
		if before != nil && (record.CreatedAt.After(*before) || (record.CreatedAt.Equal(*before) && record.ID >= beforeID)) {
			continue
		}
		items = append(items, cloneEstimateRecord(record))
	}
	r.mu.Unlock()
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	page := EstimateJobPage{}
	if len(items) > limit {
		page.NextCursor, err = encodeEstimateCursor(items[limit-1].CreatedAt, items[limit-1].ID)
		if err != nil {
			return EstimateJobPage{}, err
		}
		items = items[:limit]
	}
	for index := range items {
		job, getErr := r.queue.Get(ctx, items[index].ID)
		if getErr != nil {
			return EstimateJobPage{}, getErr
		}
		items[index].Job = job
	}
	page.Items = items
	return page, nil
}

func encodeEstimateCursor(createdAt time.Time, identifier string) (string, error) {
	raw, err := json.Marshal(estimateCursor{CreatedAt: createdAt.UTC(), ID: strings.TrimSpace(identifier)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeEstimateCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidEstimateCursor
	}
	var cursor estimateCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return nil, "", ErrInvalidEstimateCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || cursor.CreatedAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return nil, "", ErrInvalidEstimateCursor
	}
	at := cursor.CreatedAt.UTC()
	return &at, strings.TrimSpace(cursor.ID), nil
}

func cloneEstimate(value Estimate) Estimate {
	copied := value
	if value.Breakdown != nil {
		breakdown := *value.Breakdown
		copied.Breakdown = &breakdown
	}
	return copied
}

func cloneEstimateRecord(record EstimateJobRecord) EstimateJobRecord {
	raw, _ := json.Marshal(record.Definition)
	_ = json.Unmarshal(raw, &record.Definition)
	record.Job.Payload = append(json.RawMessage(nil), record.Job.Payload...)
	if record.Result != nil {
		value := cloneEstimate(*record.Result)
		record.Result = &value
	}
	return record
}
