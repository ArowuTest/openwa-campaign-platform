package dispatch

import (
	"context"
	"errors"
	"strings"
	"time"

	"campaign-platform/internal/metacloud"
)

type MetaMessageClient interface {
	SendMessage(context.Context, metacloud.MessageRequest) (metacloud.MessageResult, error)
}

type MetaGateway struct {
	Client MetaMessageClient
	Clock  func() time.Time
}

func (g *MetaGateway) now() time.Time {
	if g != nil && g.Clock != nil {
		return g.Clock().UTC()
	}
	return time.Now().UTC()
}

func (g *MetaGateway) Preflight(_ context.Context, request GatewayRequest) error {
	if err := validateMetaGatewayRequest(request); err != nil {
		return GatewayError{Code: "META_REQUEST_INVALID", Safety: FailurePermanent, Err: err}
	}
	if request.MetaRepresentation == MetaRepresentationFreeForm {
		deadline := request.MetaFreeFormEligibleUntil.UTC()
		now := g.now()
		if deadline.IsZero() || !deadline.After(now) {
			return GatewayError{Code: "META_CONVERSATION_WINDOW_EXPIRED", Safety: FailureSafeToRetry, Err: errors.New("Meta free-form conversation window is no longer eligible")}
		}
	}
	if g == nil || g.Client == nil {
		return GatewayError{Code: "META_TRANSPORT_UNAVAILABLE", Safety: FailureSafeToRetry, Err: errors.New("Meta transport client is not configured")}
	}
	if _, err := buildMetaGatewayPayload(request); err != nil {
		return GatewayError{Code: "META_PAYLOAD_INVALID", Safety: FailurePermanent, Err: err}
	}
	return nil
}

func (g *MetaGateway) Send(ctx context.Context, request GatewayRequest) (GatewayResult, error) {
	if err := g.Preflight(ctx, request); err != nil {
		return GatewayResult{}, err
	}
	return g.SendPrepared(ctx, request)
}

func (g *MetaGateway) SendPrepared(ctx context.Context, request GatewayRequest) (GatewayResult, error) {
	if err := validateMetaGatewayRequest(request); err != nil {
		return GatewayResult{}, GatewayError{Code: "META_REQUEST_INVALID", Safety: FailurePermanent, Err: err}
	}
	if g == nil || g.Client == nil {
		return GatewayResult{}, GatewayError{Code: "META_PREPARED_TRANSPORT_INVARIANT", Safety: FailurePermanent, Err: errors.New("Meta transport client disappeared after successful preflight")}
	}
	payload, err := buildMetaGatewayPayload(request)
	if err != nil {
		return GatewayResult{}, GatewayError{Code: "META_PAYLOAD_INVALID", Safety: FailurePermanent, Err: err}
	}
	result, err := g.Client.SendMessage(ctx, metacloud.MessageRequest{CredentialKey: request.MetaCredentialKey, GraphAPIVersion: request.MetaGraphAPIVersion, PhoneNumberID: request.MetaPhoneNumberID, Payload: payload})
	if err != nil {
		return GatewayResult{}, mapMetaClientError(err)
	}
	if strings.TrimSpace(result.MessageID) == "" {
		return GatewayResult{}, GatewayError{Code: "META_ACCEPTANCE_UNKNOWN", Safety: FailureOutcomeUnknown, Err: errors.New("Meta acceptance omitted message ID")}
	}
	return GatewayResult{Accepted: true, ProviderMessageID: strings.TrimSpace(result.MessageID), AcceptedAt: g.now(), RawStatusCode: "META_ACCEPTED"}, nil
}
func buildMetaGatewayPayload(request GatewayRequest) (map[string]any, error) {
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                strings.TrimPrefix(strings.TrimSpace(request.RecipientE164), "+"),
	}
	switch request.MetaRepresentation {
	case MetaRepresentationTemplate:
		components, err := renderMetaRequestComponents(request)
		if err != nil {
			return nil, err
		}
		payload["type"] = "template"
		payload["template"] = map[string]any{
			"name":       request.MetaTemplateName,
			"language":   map[string]any{"code": request.MetaTemplateLanguage},
			"components": components,
		}
	case MetaRepresentationFreeForm:
		typeName := strings.ToLower(strings.TrimSpace(request.MessageType))
		payload["type"] = typeName
		switch typeName {
		case "text":
			payload["text"] = map[string]any{"body": request.Body}
		case "image", "video", "document":
			media := map[string]any{"link": strings.TrimSpace(request.MediaURL)}
			if caption := strings.TrimSpace(request.Body); caption != "" {
				media["caption"] = caption
			}
			payload[typeName] = media
		default:
			return nil, errors.New("unsupported Meta free-form message type")
		}
	default:
		return nil, errors.New("unsupported Meta representation")
	}
	return payload, nil
}

func validateMetaGatewayRequest(request GatewayRequest) error {
	if strings.ToUpper(strings.TrimSpace(request.Provider)) != "META" || strings.ToUpper(strings.TrimSpace(request.Engine)) != "CLOUD_API" {
		return errors.New("Meta Cloud provider and engine are required")
	}
	if strings.TrimSpace(request.MetaSenderID) == "" || request.MetaSenderVersion <= 0 || strings.TrimSpace(request.MetaCredentialKey) == "" ||
		strings.TrimSpace(request.MetaGraphAPIVersion) == "" || strings.TrimSpace(request.MetaPhoneNumberID) == "" || !strings.HasPrefix(strings.TrimSpace(request.RecipientE164), "+") {
		return errors.New("Meta sender and recipient evidence are required")
	}
	if strings.TrimSpace(request.GatewayPoolID) != "" || request.GatewayPoolVersion != 0 || strings.TrimSpace(request.GatewayAdapterVersion) != "" ||
		strings.TrimSpace(request.GatewayNodeID) != "" || request.GatewayNodeVersion != 0 || strings.TrimSpace(request.SessionID) != "" ||
		request.SessionLeaseVersion != 0 || request.SessionConfigurationVersion != 0 || !request.AuthorityExpiresAt.IsZero() {
		return errors.New("Meta route cannot carry OpenWA gateway authority")
	}
	switch request.MetaRepresentation {
	case MetaRepresentationTemplate:
		if !request.MetaFreeFormEligibleUntil.IsZero() {
			return errors.New("Meta template representation cannot carry free-form eligibility")
		}
		if strings.TrimSpace(request.MetaTemplateName) == "" || strings.TrimSpace(request.MetaTemplateLanguage) == "" {
			return errors.New("Meta template representation requires template evidence")
		}
		typed := len(request.MetaComponents) > 0
		switch request.MessageType {
		case "text":
			if !typed && strings.TrimSpace(request.MediaURL) != "" {
				return errors.New("text Meta template cannot carry media URL")
			}
		case "image", "video", "document":
			if !typed && strings.TrimSpace(request.MediaURL) == "" {
				return errors.New("media Meta template requires media URL")
			}
		default:
			return errors.New("unsupported Meta message type")
		}
	case MetaRepresentationFreeForm:
		if request.MetaFreeFormEligibleUntil.IsZero() {
			return errors.New("Meta free-form representation requires conversation-window deadline")
		}
		if strings.TrimSpace(request.MetaTemplateName) != "" || strings.TrimSpace(request.MetaTemplateLanguage) != "" || len(request.MetaBodyParameters) != 0 || len(request.MetaComponents) != 0 {
			return errors.New("Meta free-form representation cannot carry template evidence")
		}
		switch request.MessageType {
		case "text":
			if strings.TrimSpace(request.Body) == "" || strings.TrimSpace(request.MediaURL) != "" {
				return errors.New("Meta free-form text requires body without media")
			}
		case "image", "video", "document":
			if strings.TrimSpace(request.MediaURL) == "" {
				return errors.New("Meta free-form media requires media URL")
			}
		default:
			return errors.New("unsupported Meta message type")
		}
	default:
		return errors.New("Meta representation must be explicitly TEMPLATE or FREE_FORM")
	}
	return nil
}
func mapMetaClientError(err error) error {
	var apiErr metacloud.APIError
	if !errors.As(err, &apiErr) {
		return GatewayError{Code: "META_OUTCOME_UNKNOWN", Safety: FailureOutcomeUnknown, Err: err}
	}
	safety := FailurePermanent
	switch apiErr.Safety {
	case metacloud.SafetySafeToRetry:
		safety = FailureSafeToRetry
	case metacloud.SafetyOutcomeUnknown:
		safety = FailureOutcomeUnknown
	case metacloud.SafetyPermanent:
		safety = FailurePermanent
	}
	return GatewayError{Code: apiErr.Code, Safety: safety, RetryAfter: apiErr.RetryAfter, Err: err}
}
