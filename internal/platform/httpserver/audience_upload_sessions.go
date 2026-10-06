package httpserver

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"campaign-platform/internal/audience/importer"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/shared/httpx"
	"campaign-platform/internal/storage"
)

const uploadPartChecksumHeader = "X-Content-SHA256"

type createAudienceUploadSessionRequest struct {
	OrganisationID      string                 `json:"organisationId"`
	ConsentReviewID     string                 `json:"consentReviewId"`
	PurposeID           string                 `json:"purposeId"`
	Channel             string                 `json:"channel"`
	WordingVersion      string                 `json:"wordingVersion"`
	SourceName          string                 `json:"sourceName"`
	SourceSystem        string                 `json:"sourceSystem,omitempty"`
	DefaultCountryISO2  string                 `json:"defaultCountryIso2,omitempty"`
	OriginalFilename    string                 `json:"originalFilename"`
	TemplateVersion     string                 `json:"templateVersion"`
	MappingDefinitionID string                 `json:"mappingDefinitionId,omitempty"`
	Mapping             importer.ColumnMapping `json:"mapping"`
	UpdatePolicy        importer.UpdatePolicy  `json:"updatePolicy"`
	ExpectedBytes       int64                  `json:"expectedBytes"`
}

type uploadSessionEnvelope struct {
	Transport          string                 `json:"transport"`
	Transports         []string               `json:"transports"`
	PreferredTransport string                 `json:"preferredTransport"`
	Session            importer.UploadSession `json:"session"`
}

type directUploadPartRequest struct {
	SHA256 string `json:"sha256"`
}

type directUploadPartTargetEnvelope struct {
	PartNumber      int                         `json:"partNumber"`
	AlreadyUploaded bool                        `json:"alreadyUploaded"`
	Target          *storage.DirectUploadTarget `json:"target,omitempty"`
}

type uploadPartEnvelope struct {
	Changed bool                   `json:"changed"`
	Session importer.UploadSession `json:"session"`
}

type uploadSessionVersionRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	Reason          string `json:"reason,omitempty"`
}

func audienceUploadEnvelope(service *importer.UploadSessionService, session importer.UploadSession) uploadSessionEnvelope {
	transports := service.SupportedTransports()
	preferred := "RELAY"
	if len(transports) > 0 {
		preferred = transports[0]
	}
	return uploadSessionEnvelope{
		Transport: preferred, Transports: transports, PreferredTransport: preferred, Session: session,
	}
}

func parseAudienceUploadPartNumber(w http.ResponseWriter, r *http.Request) (int, bool) {
	number, err := strconv.Atoi(strings.TrimSpace(r.PathValue("part")))
	if err != nil || number <= 0 {
		httpx.WriteError(w, r, http.StatusBadRequest, "AUDIENCE_UPLOAD_PART_INVALID", "The upload part number is invalid.", nil)
		return 0, false
	}
	return number, true
}

func (s *Server) createAudienceUploadSession(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceUploadSessions == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "AUDIENCE_UPLOAD_SESSION_UNAVAILABLE", "Resumable audience upload is not configured.", nil)
		return
	}
	var input createAudienceUploadSessionRequest
	if err := httpx.DecodeJSON(w, r, 128<<10, &input); err != nil {
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	session, created, err := s.deps.AudienceUploadSessions.Create(r.Context(), importer.UploadSessionInput{
		OrganisationID:      input.OrganisationID,
		ConsentReviewID:     input.ConsentReviewID,
		PurposeID:           input.PurposeID,
		Channel:             input.Channel,
		WordingVersion:      input.WordingVersion,
		SourceName:          input.SourceName,
		SourceSystem:        input.SourceSystem,
		DefaultCountryISO2:  input.DefaultCountryISO2,
		OriginalFilename:    input.OriginalFilename,
		TemplateVersion:     input.TemplateVersion,
		MappingDefinitionID: input.MappingDefinitionID,
		Mapping:             input.Mapping,
		UpdatePolicy:        input.UpdatePolicy,
		UploadedBy:          principal.User.ID,
		ClientRequestID:     strings.TrimSpace(r.Header.Get("Idempotency-Key")),
		ExpectedBytes:       input.ExpectedBytes,
	})
	if err != nil {
		s.writeAudienceUploadError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, audienceUploadEnvelope(s.deps.AudienceUploadSessions, session))
}

func (s *Server) getAudienceUploadSession(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceUploadSessions == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "AUDIENCE_UPLOAD_SESSION_UNAVAILABLE", "Resumable audience upload is not configured.", nil)
		return
	}
	session, err := s.deps.AudienceUploadSessions.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeAudienceUploadError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, audienceUploadEnvelope(s.deps.AudienceUploadSessions, session))
}

func (s *Server) createAudienceUploadPartTarget(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceUploadSessions == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "AUDIENCE_UPLOAD_SESSION_UNAVAILABLE", "Resumable audience upload is not configured.", nil)
		return
	}
	number, ok := parseAudienceUploadPartNumber(w, r)
	if !ok {
		return
	}
	var input directUploadPartRequest
	if err := httpx.DecodeJSON(w, r, 8<<10, &input); err != nil {
		return
	}
	target, alreadyUploaded, err := s.deps.AudienceUploadSessions.CreateDirectPartTarget(
		r.Context(), r.PathValue("id"), number, strings.TrimSpace(input.SHA256), 5*time.Minute,
	)
	if err != nil {
		s.writeAudienceUploadError(w, r, err)
		return
	}
	response := directUploadPartTargetEnvelope{PartNumber: number, AlreadyUploaded: alreadyUploaded}
	if !alreadyUploaded {
		response.Target = &target
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}

func (s *Server) confirmAudienceUploadPart(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceUploadSessions == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "AUDIENCE_UPLOAD_SESSION_UNAVAILABLE", "Resumable audience upload is not configured.", nil)
		return
	}
	number, ok := parseAudienceUploadPartNumber(w, r)
	if !ok {
		return
	}
	var input directUploadPartRequest
	if err := httpx.DecodeJSON(w, r, 8<<10, &input); err != nil {
		return
	}
	session, changed, err := s.deps.AudienceUploadSessions.ConfirmDirectPart(
		r.Context(), r.PathValue("id"), number, strings.TrimSpace(input.SHA256),
	)
	if err != nil {
		s.writeAudienceUploadError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, uploadPartEnvelope{Changed: changed, Session: session})
}

func (s *Server) putAudienceUploadPart(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceUploadSessions == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "AUDIENCE_UPLOAD_SESSION_UNAVAILABLE", "Resumable audience upload is not configured.", nil)
		return
	}
	number, err := strconv.Atoi(strings.TrimSpace(r.PathValue("part")))
	if err != nil || number <= 0 {
		httpx.WriteError(w, r, http.StatusBadRequest, "AUDIENCE_UPLOAD_PART_INVALID", "The upload part number is invalid.", nil)
		return
	}
	current, err := s.deps.AudienceUploadSessions.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeAudienceUploadError(w, r, err)
		return
	}
	if number > len(current.Parts) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "AUDIENCE_UPLOAD_PART_OUT_OF_RANGE", "The upload part is outside the session manifest.", nil)
		return
	}
	expectedBytes := current.Parts[number-1].ExpectedBytes
	if r.ContentLength > expectedBytes {
		httpx.WriteError(w, r, http.StatusRequestEntityTooLarge, "AUDIENCE_UPLOAD_PART_TOO_LARGE", "The upload part exceeds its expected byte range.", map[string]any{"maximumBytes": expectedBytes})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, expectedBytes+1)
	session, changed, err := s.deps.AudienceUploadSessions.PutPart(
		r.Context(),
		current.ID,
		number,
		strings.TrimSpace(r.Header.Get(uploadPartChecksumHeader)),
		r.Body,
	)
	if err != nil {
		s.writeAudienceUploadError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, uploadPartEnvelope{Changed: changed, Session: session})
}

func (s *Server) completeAudienceUploadSession(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceUploadSessions == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "AUDIENCE_UPLOAD_SESSION_UNAVAILABLE", "Resumable audience upload is not configured.", nil)
		return
	}
	var input uploadSessionVersionRequest
	if err := httpx.DecodeJSON(w, r, 16<<10, &input); err != nil {
		return
	}
	session, err := s.deps.AudienceUploadSessions.Complete(r.Context(), r.PathValue("id"), input.ExpectedVersion)
	if err != nil {
		s.writeAudienceUploadError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, audienceUploadEnvelope(s.deps.AudienceUploadSessions, session))
}

func (s *Server) abortAudienceUploadSession(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceUploadSessions == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "AUDIENCE_UPLOAD_SESSION_UNAVAILABLE", "Resumable audience upload is not configured.", nil)
		return
	}
	var input uploadSessionVersionRequest
	if err := httpx.DecodeJSON(w, r, 16<<10, &input); err != nil {
		return
	}
	session, err := s.deps.AudienceUploadSessions.Abort(r.Context(), r.PathValue("id"), input.Reason, input.ExpectedVersion)
	if err != nil {
		s.writeAudienceUploadError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, audienceUploadEnvelope(s.deps.AudienceUploadSessions, session))
}

func (s *Server) writeAudienceUploadError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, importer.ErrUploadSessionNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "AUDIENCE_UPLOAD_SESSION_NOT_FOUND", "The upload session was not found.", nil)
	case errors.Is(err, importer.ErrUploadSessionReplayConflict):
		httpx.WriteError(w, r, http.StatusConflict, "AUDIENCE_UPLOAD_REPLAY_CONFLICT", "The upload request key was already used with different source metadata.", nil)
	case errors.Is(err, importer.ErrUploadSessionConflict):
		httpx.WriteError(w, r, http.StatusConflict, "AUDIENCE_UPLOAD_VERSION_CONFLICT", "The upload session changed; refresh it before retrying the action.", nil)
	case errors.Is(err, importer.ErrDirectUploadUnavailable):
		httpx.WriteError(w, r, http.StatusConflict, "AUDIENCE_UPLOAD_DIRECT_UNAVAILABLE", "Direct object-store upload is not enabled for this environment; use the bounded relay transport.", nil)
	case errors.Is(err, storage.ErrNotFound):
		httpx.WriteError(w, r, http.StatusConflict, "AUDIENCE_UPLOAD_PART_NOT_PRESENT", "The direct-upload part is not present in quarantine storage yet.", nil)
	case errors.Is(err, importer.ErrUploadPartConflict), errors.Is(err, storage.ErrKeyConflict):
		httpx.WriteError(w, r, http.StatusConflict, "AUDIENCE_UPLOAD_PART_CONFLICT", "The upload part conflicts with immutable evidence already recorded for this session.", nil)
	case errors.Is(err, importer.ErrUploadSessionExpired):
		httpx.WriteError(w, r, http.StatusGone, "AUDIENCE_UPLOAD_SESSION_EXPIRED", "The upload session has expired.", nil)
	case errors.Is(err, storage.ErrTooLarge):
		httpx.WriteError(w, r, http.StatusRequestEntityTooLarge, "AUDIENCE_UPLOAD_PART_TOO_LARGE", "The upload part exceeded the configured size limit.", nil)
	case errors.Is(err, importer.ErrUploadPartOutOfRange), errors.Is(err, importer.ErrUploadPartInvalid),
		errors.Is(err, importer.ErrUploadPartIncomplete), errors.Is(err, importer.ErrUploadSessionState):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "AUDIENCE_UPLOAD_INVALID_STATE", "The upload action is not valid for the current session or part state.", map[string]any{"detail": err.Error()})
	default:
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			httpx.WriteError(w, r, http.StatusRequestEntityTooLarge, "AUDIENCE_UPLOAD_PART_TOO_LARGE", "The upload part exceeded the configured size limit.", nil)
			return
		}
		// Create-time metadata/limit validation is client-correctable and does
		// not expose storage or recipient data.
		httpx.WriteError(w, r, http.StatusBadRequest, "AUDIENCE_UPLOAD_INVALID_REQUEST", "The resumable audience upload request is invalid.", map[string]any{"detail": err.Error()})
	}
}
