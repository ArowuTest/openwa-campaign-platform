package httpserver

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"campaign-platform/internal/commercial"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/shared/httpx"
)

type commercialDraftRequest struct {
	QuotationReference string     `json:"quotationReference"`
	InvoiceReference   string     `json:"invoiceReference"`
	Currency           string     `json:"currency"`
	ApprovedRecipients int64      `json:"approvedRecipients"`
	UnitPriceMinor     int64      `json:"unitPriceMinor"`
	ManagementFeeMinor int64      `json:"managementFeeMinor"`
	PaymentReference   string     `json:"paymentReference,omitempty"`
	PaymentReceivedAt  *time.Time `json:"paymentReceivedAt,omitempty"`
	Reason             string     `json:"reason"`
}

type commercialDraftUpdateRequest struct {
	ExpectedVersion    int64      `json:"expectedVersion"`
	QuotationReference string     `json:"quotationReference"`
	InvoiceReference   string     `json:"invoiceReference"`
	Currency           string     `json:"currency"`
	ApprovedRecipients int64      `json:"approvedRecipients"`
	UnitPriceMinor     int64      `json:"unitPriceMinor"`
	ManagementFeeMinor int64      `json:"managementFeeMinor"`
	PaymentReference   string     `json:"paymentReference,omitempty"`
	PaymentReceivedAt  *time.Time `json:"paymentReceivedAt,omitempty"`
	Reason             string     `json:"reason"`
}

func (input commercialDraftUpdateRequest) draft() commercialDraftRequest {
	return commercialDraftRequest{
		QuotationReference: input.QuotationReference,
		InvoiceReference:   input.InvoiceReference,
		Currency:           input.Currency,
		ApprovedRecipients: input.ApprovedRecipients,
		UnitPriceMinor:     input.UnitPriceMinor,
		ManagementFeeMinor: input.ManagementFeeMinor,
		PaymentReference:   input.PaymentReference,
		PaymentReceivedAt:  input.PaymentReceivedAt,
		Reason:             input.Reason,
	}
}

func (input commercialDraftRequest) record() (commercial.Record, error) {
	total, err := commercial.DeriveTotal(input.ApprovedRecipients, input.UnitPriceMinor, input.ManagementFeeMinor)
	if err != nil {
		return commercial.Record{}, err
	}
	return commercial.Record{
		QuotationReference: strings.TrimSpace(input.QuotationReference),
		InvoiceReference:   strings.TrimSpace(input.InvoiceReference),
		Currency:           strings.ToUpper(strings.TrimSpace(input.Currency)),
		ApprovedRecipients: input.ApprovedRecipients,
		UnitPriceMinor:     input.UnitPriceMinor,
		ManagementFeeMinor: input.ManagementFeeMinor,
		TotalAmountMinor:   total,
		PaymentReference:   strings.TrimSpace(input.PaymentReference),
		PaymentReceivedAt:  input.PaymentReceivedAt,
	}, nil
}

func (s *Server) getCampaignCommercialRecord(w http.ResponseWriter, r *http.Request) {
	if s.deps.Commercial == nil || s.deps.Campaigns == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "COMMERCIAL_UNAVAILABLE", "Commercial governance is not configured.", nil)
		return
	}
	campaignValue, err := s.deps.Campaigns.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, r, http.StatusNotFound, "CAMPAIGN_NOT_FOUND", "The campaign was not found.", nil)
		return
	}
	value, err := s.deps.Commercial.GetByCampaign(r.Context(), campaignValue.ID)
	if errors.Is(err, commercial.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "COMMERCIAL_NOT_FOUND", "No commercial record exists for this campaign.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}

func (s *Server) updateCommercialRecordDraft(w http.ResponseWriter, r *http.Request) {
	if s.deps.Commercial == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "COMMERCIAL_UNAVAILABLE", "Commercial governance is not configured.", nil)
		return
	}
	var input commercialDraftUpdateRequest
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The commercial draft correction is invalid.", nil)
		return
	}
	record, recordErr := input.draft().record()
	if recordErr != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "COMMERCIAL_INVALID", "The commercial correction contains invalid derived values.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	value, err := s.deps.Commercial.UpdateDraft(
		r.Context(), r.PathValue("id"), input.ExpectedVersion, record, principal.User.ID, input.Reason,
	)
	switch {
	case errors.Is(err, commercial.ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "COMMERCIAL_NOT_FOUND", "The commercial record was not found.", nil)
	case errors.Is(err, commercial.ErrConflict):
		httpx.WriteError(w, r, http.StatusConflict, "COMMERCIAL_CONFLICT", "The commercial record changed; reload before correcting it.", nil)
	case err != nil:
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "COMMERCIAL_INVALID", "The commercial correction was rejected.", nil)
	default:
		httpx.WriteJSON(w, http.StatusOK, value)
	}
}

func (s *Server) listCommercialRecordEvents(w http.ResponseWriter, r *http.Request) {
	if s.deps.Commercial == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "COMMERCIAL_UNAVAILABLE", "Commercial governance is not configured.", nil)
		return
	}
	page, err := httpx.ParsePage(r, 100, 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE", "The commercial-event page request is invalid.", nil)
		return
	}
	pageResult, err := s.deps.Commercial.EventsPage(r.Context(), r.PathValue("id"), page.Limit, page.Cursor)
	if errors.Is(err, commercial.ErrInvalidEventCursor) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_CURSOR", "The commercial-event page cursor is invalid.", nil)
		return
	}
	if errors.Is(err, commercial.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "COMMERCIAL_NOT_FOUND", "The commercial record was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteList(w, http.StatusOK, pageResult.Items, len(pageResult.Items), pageResult.NextCursor)
}
