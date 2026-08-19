package httpserver

import (
	"errors"
	"net/http"
	"strings"

	"campaign-platform/internal/identity"
	"campaign-platform/internal/metacloud"
	"campaign-platform/internal/shared/httpx"
)

type metaTemplateSyncRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	Reason          string `json:"reason"`
}

type metaBindingRequest struct {
	MetaSenderID      string                       `json:"metaSenderId"`
	TemplateName      string                       `json:"templateName"`
	Language          string                       `json:"language"`
	ComponentBindings []metacloud.ComponentBinding `json:"componentBindings"`
}

func (s *Server) requireMetaTemplates(w http.ResponseWriter, r *http.Request) (*metacloud.TemplateService, bool) {
	if s.deps.MetaTemplates == nil || s.deps.MetaTemplates.Store == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "META_TEMPLATE_GOVERNANCE_UNAVAILABLE", "Meta template governance is unavailable.", nil)
		return nil, false
	}
	return s.deps.MetaTemplates, true
}
func (s *Server) syncMetaSenderTemplates(w http.ResponseWriter, r *http.Request) {
	templates, ok := s.requireMetaTemplates(w, r)
	if !ok {
		return
	}
	senders, ok := s.requireMetaSenders(w, r)
	if !ok {
		return
	}
	var input metaTemplateSyncRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The Meta template sync request is invalid.", nil)
		return
	}
	sender, err := senders.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeMetaSenderError(w, r, err)
		return
	}
	if sender.Version != input.ExpectedVersion {
		s.writeMetaSenderError(w, r, metacloud.ErrConflict)
		return
	}
	if err := templates.SyncSenderTemplates(r.Context(), sender); err != nil {
		s.writeMetaTemplateError(w, r, err)
		return
	}
	values, err := templates.Store.ListApproved(r.Context(), sender.OrganisationID, sender.WABAID)
	if err != nil {
		s.writeMetaTemplateError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"sender": sender, "templates": values})
}
func (s *Server) listMetaSenderTemplates(w http.ResponseWriter, r *http.Request) {
	templates, ok := s.requireMetaTemplates(w, r)
	if !ok {
		return
	}
	senders, ok := s.requireMetaSenders(w, r)
	if !ok {
		return
	}
	sender, err := senders.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeMetaSenderError(w, r, err)
		return
	}
	values, err := templates.Store.ListApproved(r.Context(), sender.OrganisationID, sender.WABAID)
	if err != nil {
		s.writeMetaTemplateError(w, r, err)
		return
	}
	httpx.WriteListAuto(w, http.StatusOK, values)
}

func (s *Server) createMetaMessageBinding(w http.ResponseWriter, r *http.Request) {
	templates, ok := s.requireMetaTemplates(w, r)
	if !ok {
		return
	}
	senders, ok := s.requireMetaSenders(w, r)
	if !ok {
		return
	}
	var input metaBindingRequest
	if err := httpx.DecodeJSON(w, r, 128<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The Meta message binding request is invalid.", nil)
		return
	}
	sender, err := senders.Get(r.Context(), strings.TrimSpace(input.MetaSenderID))
	if err != nil {
		s.writeMetaSenderError(w, r, err)
		return
	}
	if sender.Status != metacloud.StatusActive {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "META_SENDER_INACTIVE", "An active governed Meta sender is required for binding.", nil)
		return
	}
	template, err := templates.Store.FindApproved(r.Context(), sender.OrganisationID, sender.WABAID, strings.TrimSpace(input.TemplateName), strings.TrimSpace(input.Language))
	if err != nil {
		s.writeMetaTemplateError(w, r, err)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	binding, err := templates.CreateBinding(r.Context(), sender.OrganisationID, sender.WABAID, metacloud.Binding{
		MessageVersionID: r.PathValue("id"), TemplateName: template.Name, Language: template.Language,
		ComponentBindings: input.ComponentBindings, TemplateComponentHash: template.ComponentHash, CreatedBy: principal.User.ID,
	})
	if err != nil {
		s.writeMetaTemplateError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, binding)
}

func (s *Server) getMetaMessageBinding(w http.ResponseWriter, r *http.Request) {
	templates, ok := s.requireMetaTemplates(w, r)
	if !ok {
		return
	}
	binding, err := templates.Store.GetBinding(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeMetaTemplateError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, binding)
}

func (s *Server) writeMetaTemplateError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, metacloud.ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "META_TEMPLATE_NOT_FOUND", "The Meta template or binding was not found.", nil)
	case errors.Is(err, metacloud.ErrBindingConflict):
		httpx.WriteError(w, r, http.StatusConflict, "META_BINDING_CONFLICT", "The message version already has different immutable Meta binding evidence.", nil)
	case errors.Is(err, metacloud.ErrBindingInvalid), errors.Is(err, metacloud.ErrTemplateInvalid):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "META_TEMPLATE_INVALID", "The Meta template or binding is incompatible with the approved message.", nil)
	default:
		var apiErr metacloud.APIError
		if errors.As(err, &apiErr) {
			s.writeMetaVerificationError(w, r, err)
			return
		}
		s.internalError(w, r, err)
	}
}
