package dispatch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"campaign-platform/internal/delivery"
	"campaign-platform/internal/message"
	"campaign-platform/internal/metacloud"
	"campaign-platform/internal/provider"
)

var (
	errMetaSenderUnavailable = errors.New("Meta sender is temporarily unavailable")
	errMetaSenderHealthStale = errors.New("Meta sender health evidence is stale or invalid")
)

func (l *PostgreSQLMaterialLoader) metaHealthStaleAfter() time.Duration {
	if l != nil && l.MetaHealthStaleAfter > 0 {
		return l.MetaHealthStaleAfter
	}
	return 5 * time.Minute
}

func (l *PostgreSQLMaterialLoader) metaConversationWindow() time.Duration {
	if l != nil && l.MetaConversationWindow > 0 && l.MetaConversationWindow <= 24*time.Hour {
		return l.MetaConversationWindow
	}
	return 24 * time.Hour
}

func retryableMetaMaterialValidation(err error) bool {
	return errors.Is(err, errMetaSenderUnavailable) || errors.Is(err, errMetaSenderHealthStale)
}

type metaMaterialEvidence struct {
	ExpectedSenderPoolID          string
	ExpectedProvider              string
	ExpectedEngine                string
	ExpectedAdapterVersion        string
	ProviderDefinitionID          string
	ProviderDefinitionVersion     int64
	ExpectedMetaSenderID          string
	ExpectedMetaSenderVersion     int64
	CampaignOrganisationID        string
	CampaignRequiredCapabilities  []string
	SenderOrganisationID          string
	SenderPoolID                  string
	SenderWABAID                  string
	SenderPhoneNumberID           string
	SenderCredentialKey           string
	SenderGraphAPIVersion         string
	SenderStatus                  string
	SenderHealth                  string
	SenderHealthObservedAt        time.Time
	SenderEffectiveFrom           time.Time
	SenderEffectiveTo             *time.Time
	SenderVersion                 int64
	DefinitionVersion             int64
	DefinitionProvider            string
	DefinitionChannel             string
	DefinitionEngine              string
	DefinitionAdapterVersion      string
	DefinitionCapabilities        []string
	DefinitionStatus              string
	DefinitionEffectiveFrom       time.Time
	DefinitionEffectiveTo         *time.Time
	BindingTemplateName           string
	BindingLanguage               string
	BindingBodyVariableNames      []string
	BindingComponentBindings      []metacloud.ComponentBinding
	BindingMediaHeaderType        string
	BindingComponentHash          string
	TemplateStatus                string
	TemplateComponentHash         string
	ConversationProviderMessageID string
	ConversationOccurredAt        time.Time
	ConversationEligibleUntil     time.Time
}

func (l *PostgreSQLMaterialLoader) loadMetaMaterial(
	ctx context.Context,
	recipient delivery.Recipient,
	selection allocationRouteSelection,
	messageType, routeReference, e164, body, mediaURL string,
	variables []message.Variable,
	values map[string]string,
	now time.Time,
) (Material, error) {
	evidence, err := l.loadMetaMaterialEvidence(ctx, recipient.ID)
	if err != nil {
		return Material{}, err
	}
	if err := validateMetaCommonMaterialEvidence(evidence, selection, messageType, now.UTC(), l.metaHealthStaleAfter()); err != nil {
		if retryableMetaMaterialValidation(err) {
			return Material{}, err
		}
		return Material{}, PermanentMaterialError{Err: err}
	}
	base := Material{
		SenderPoolID: selection.SenderPoolID, Provider: "META", Engine: "CLOUD_API",
		RouteReference: routeReference, MetaSenderID: evidence.ExpectedMetaSenderID,
		MetaSenderVersion: evidence.ExpectedMetaSenderVersion, MetaCredentialKey: evidence.SenderCredentialKey,
		MetaGraphAPIVersion: evidence.SenderGraphAPIVersion, MetaPhoneNumberID: evidence.SenderPhoneNumberID,
		RecipientE164: e164, MessageType: messageType, Body: body, MediaObjectURL: mediaURL, ClientReference: recipient.ID,
	}
	if eligibleUntil, ok := metaFreeFormEligibilityDeadline(evidence, now.UTC(), l.metaConversationWindow()); ok {
		base.MetaRepresentation = MetaRepresentationFreeForm
		base.MetaFreeFormEligibleUntil = eligibleUntil
		return base, nil
	}
	if err := validateMetaTemplateEvidence(evidence, messageType); err != nil {
		return Material{}, PermanentMaterialError{Err: err}
	}
	parameters, err := resolveMetaBindingParameters(evidence.BindingBodyVariableNames, variables, values)
	if err != nil {
		return Material{}, PermanentMaterialError{Err: err}
	}
	components, err := renderMetaBindingComponents(evidence.BindingComponentBindings, evidence.BindingBodyVariableNames, evidence.BindingMediaHeaderType, variables, values, mediaURL)
	if err != nil {
		return Material{}, PermanentMaterialError{Err: err}
	}
	base.MetaRepresentation = MetaRepresentationTemplate
	base.MetaTemplateName = evidence.BindingTemplateName
	base.MetaTemplateLanguage = evidence.BindingLanguage
	base.MetaBodyParameters = parameters
	base.MetaComponents = components
	return base, nil
}

func (l *PostgreSQLMaterialLoader) loadMetaMaterialEvidence(ctx context.Context, recipientID string) (metaMaterialEvidence, error) {
	const query = `
SELECT coalesce(sh.assigned_sender_pool_id::text,''),
       coalesce(rp.provider,''),
       coalesce(rp.engine,''),
       coalesce(rp.provider_adapter_version,''),
       coalesce(rp.provider_capability_definition_id::text,''),
       coalesce(rp.provider_capability_definition_version,0),
       coalesce(rp.meta_sender_id::text,''),
       coalesce(rp.meta_sender_version,0),
       cp.organisation_id::text,coalesce(cp.required_capabilities,'[]'::jsonb),
       ms.organisation_id::text,ms.sender_pool_id::text,ms.waba_id,ms.phone_number_id,ms.credential_key,ms.graph_api_version,
       ms.status,ms.health_status,ms.health_observed_at,ms.effective_from,ms.effective_to,ms.version,
       coalesce(pd.version,0),coalesce(pd.provider,''),coalesce(pd.channel,''),coalesce(pd.engine,''),
       coalesce(pd.adapter_version,''),coalesce(pd.capabilities,ARRAY[]::text[]),coalesce(pd.status,''),
       pd.effective_from,pd.effective_to,
       coalesce(mb.template_name,''),coalesce(mb.language,''),coalesce(mb.body_variable_names,'[]'::jsonb),mb.media_header_type,
       coalesce(mb.component_bindings,'[]'::jsonb),coalesce(mb.template_component_hash,''),
       coalesce(mt.status,''),coalesce(mt.component_hash,''),
       coalesce(cw.source_provider_message_id,''),cw.inbound_occurred_at,cw.eligible_until
FROM campaign_recipients cr
JOIN campaigns cp ON cp.id=cr.campaign_id
JOIN campaign_dispatch_shards sh ON sh.id=cr.dispatch_shard_id
JOIN campaign_routing_plan_pools rp
  ON rp.routing_plan_id=sh.routing_plan_id AND rp.sender_pool_id=sh.assigned_sender_pool_id
JOIN meta_cloud_senders ms ON ms.id=rp.meta_sender_id
LEFT JOIN provider_capability_definitions pd ON pd.id=rp.provider_capability_definition_id
LEFT JOIN meta_cloud_message_bindings mb ON mb.message_version_id=cr.message_version_id
LEFT JOIN meta_cloud_templates mt
  ON mt.organisation_id=cp.organisation_id
 AND mt.waba_id=ms.waba_id
 AND mt.name=mb.template_name
 AND mt.language=mb.language
LEFT JOIN LATERAL (
  SELECT source_provider_message_id,inbound_occurred_at,eligible_until
  FROM meta_cloud_conversation_windows window_evidence
  WHERE window_evidence.meta_sender_id=rp.meta_sender_id
    AND window_evidence.contact_id=cr.contact_id
  ORDER BY inbound_occurred_at DESC,source_provider_message_id DESC
  LIMIT 1
) cw ON true
WHERE cr.id=$1::uuid`
	var evidence metaMaterialEvidence
	var requiredJSON, namesJSON, componentBindingsJSON []byte
	var mediaHeader sql.NullString
	var senderHealthObserved, senderEffectiveFrom, senderEffectiveTo sql.NullTime
	var definitionEffectiveFrom, definitionEffectiveTo sql.NullTime
	var conversationOccurredAt, conversationEligibleUntil sql.NullTime
	err := l.DB.QueryRowContext(ctx, query, recipientID).Scan(
		&evidence.ExpectedSenderPoolID, &evidence.ExpectedProvider, &evidence.ExpectedEngine, &evidence.ExpectedAdapterVersion,
		&evidence.ProviderDefinitionID, &evidence.ProviderDefinitionVersion,
		&evidence.ExpectedMetaSenderID, &evidence.ExpectedMetaSenderVersion,
		&evidence.CampaignOrganisationID, &requiredJSON,
		&evidence.SenderOrganisationID, &evidence.SenderPoolID, &evidence.SenderWABAID, &evidence.SenderPhoneNumberID,
		&evidence.SenderCredentialKey, &evidence.SenderGraphAPIVersion, &evidence.SenderStatus, &evidence.SenderHealth,
		&senderHealthObserved, &senderEffectiveFrom, &senderEffectiveTo, &evidence.SenderVersion,
		&evidence.DefinitionVersion, &evidence.DefinitionProvider, &evidence.DefinitionChannel, &evidence.DefinitionEngine,
		&evidence.DefinitionAdapterVersion, &evidence.DefinitionCapabilities, &evidence.DefinitionStatus,
		&definitionEffectiveFrom, &definitionEffectiveTo,
		&evidence.BindingTemplateName, &evidence.BindingLanguage, &namesJSON, &mediaHeader, &componentBindingsJSON,
		&evidence.BindingComponentHash, &evidence.TemplateStatus, &evidence.TemplateComponentHash,
		&evidence.ConversationProviderMessageID, &conversationOccurredAt, &conversationEligibleUntil,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return metaMaterialEvidence{}, PermanentMaterialError{Err: errors.New("Meta route or sender evidence is unavailable")}
	}
	if err != nil {
		return metaMaterialEvidence{}, fmt.Errorf("load governed Meta route: %w", err)
	}
	if err := json.Unmarshal(requiredJSON, &evidence.CampaignRequiredCapabilities); err != nil {
		return metaMaterialEvidence{}, PermanentMaterialError{Err: errors.New("campaign required capabilities are invalid")}
	}
	if err := json.Unmarshal(namesJSON, &evidence.BindingBodyVariableNames); err != nil {
		return metaMaterialEvidence{}, PermanentMaterialError{Err: errors.New("Meta binding variables are invalid")}
	}
	if err := json.Unmarshal(componentBindingsJSON, &evidence.BindingComponentBindings); err != nil {
		return metaMaterialEvidence{}, PermanentMaterialError{Err: errors.New("Meta component bindings are invalid")}
	}
	if mediaHeader.Valid {
		evidence.BindingMediaHeaderType = strings.ToUpper(strings.TrimSpace(mediaHeader.String))
	}
	if senderHealthObserved.Valid {
		evidence.SenderHealthObservedAt = senderHealthObserved.Time.UTC()
	}
	if senderEffectiveFrom.Valid {
		evidence.SenderEffectiveFrom = senderEffectiveFrom.Time.UTC()
	}
	if senderEffectiveTo.Valid {
		value := senderEffectiveTo.Time.UTC()
		evidence.SenderEffectiveTo = &value
	}
	if definitionEffectiveFrom.Valid {
		evidence.DefinitionEffectiveFrom = definitionEffectiveFrom.Time.UTC()
	}
	if definitionEffectiveTo.Valid {
		value := definitionEffectiveTo.Time.UTC()
		evidence.DefinitionEffectiveTo = &value
	}
	if conversationOccurredAt.Valid {
		evidence.ConversationOccurredAt = conversationOccurredAt.Time.UTC()
	}
	if conversationEligibleUntil.Valid {
		evidence.ConversationEligibleUntil = conversationEligibleUntil.Time.UTC()
	}
	return evidence, nil
}

func validateMetaCommonMaterialEvidence(e metaMaterialEvidence, selection allocationRouteSelection, messageType string, at time.Time, healthStaleAfter time.Duration) error {
	if strings.ToUpper(strings.TrimSpace(e.ExpectedProvider)) != "META" ||
		strings.ToUpper(strings.TrimSpace(e.ExpectedEngine)) != "CLOUD_API" ||
		selection.Provider != "META" || selection.Engine != "CLOUD_API" {
		return errors.New("frozen route is not Meta Cloud")
	}
	if e.ExpectedSenderPoolID == "" || e.ExpectedSenderPoolID != selection.SenderPoolID ||
		e.ExpectedMetaSenderID == "" || e.ExpectedMetaSenderID != selection.MetaSenderID || e.ExpectedMetaSenderVersion <= 0 {
		return errors.New("Meta route endpoint does not match frozen allocation")
	}
	if e.SenderOrganisationID == "" || e.SenderOrganisationID != e.CampaignOrganisationID || e.SenderPoolID != selection.SenderPoolID {
		return errors.New("Meta sender ownership does not match campaign route")
	}
	if e.SenderStatus != string(metacloud.StatusActive) || e.SenderVersion < e.ExpectedMetaSenderVersion {
		return errors.New("Meta sender is not active at the frozen version")
	}
	if e.SenderEffectiveFrom.IsZero() || e.SenderEffectiveFrom.After(at) || (e.SenderEffectiveTo != nil && !e.SenderEffectiveTo.After(at)) {
		return errors.New("Meta sender is outside its effective period")
	}
	if e.SenderHealth != string(metacloud.HealthHealthy) && e.SenderHealth != string(metacloud.HealthDegraded) {
		return errMetaSenderUnavailable
	}
	if healthStaleAfter <= 0 {
		healthStaleAfter = 5 * time.Minute
	}
	if e.SenderHealthObservedAt.IsZero() || e.SenderHealthObservedAt.Before(at.Add(-healthStaleAfter)) || e.SenderHealthObservedAt.After(at.Add(time.Minute)) {
		return errMetaSenderHealthStale
	}
	if e.ProviderDefinitionID == "" || e.ProviderDefinitionVersion <= 0 || e.DefinitionVersion != e.ProviderDefinitionVersion {
		return errors.New("Meta provider capability definition changed from frozen evidence")
	}
	if e.DefinitionStatus != string(provider.StatusActive) || e.DefinitionProvider != "META" || e.DefinitionChannel != "WHATSAPP" || e.DefinitionEngine != "CLOUD_API" {
		return errors.New("Meta provider capability definition is not active for Cloud API")
	}
	if e.DefinitionAdapterVersion != e.ExpectedAdapterVersion || e.DefinitionEffectiveFrom.IsZero() || e.DefinitionEffectiveFrom.After(at) ||
		(e.DefinitionEffectiveTo != nil && !e.DefinitionEffectiveTo.After(at)) {
		return errors.New("Meta provider capability definition does not match the frozen route")
	}
	if strings.TrimSpace(e.SenderCredentialKey) == "" || strings.TrimSpace(e.SenderGraphAPIVersion) == "" || strings.TrimSpace(e.SenderPhoneNumberID) == "" {
		return errors.New("Meta sender runtime evidence is incomplete")
	}
	required := append([]string(nil), e.CampaignRequiredCapabilities...)
	switch messageType {
	case "text":
		required = append(required, string(provider.CapabilitySendText))
	case "image":
		required = append(required, string(provider.CapabilitySendImage))
	case "video":
		required = append(required, string(provider.CapabilitySendVideo))
	case "document":
		required = append(required, string(provider.CapabilitySendDocument))
	default:
		return errors.New("unsupported Meta message type")
	}
	available := normalizedCapabilitySet(e.DefinitionCapabilities)
	for _, capability := range required {
		capability = strings.ToUpper(strings.TrimSpace(capability))
		if capability != "" && !available[capability] {
			return errors.New("Meta provider definition lacks required capability: " + capability)
		}
	}
	return nil
}

func metaFreeFormEligibilityDeadline(e metaMaterialEvidence, at time.Time, configuredWindow time.Duration) (time.Time, bool) {
	if strings.TrimSpace(e.ConversationProviderMessageID) == "" || e.ConversationOccurredAt.IsZero() || e.ConversationEligibleUntil.IsZero() {
		return time.Time{}, false
	}
	if e.ConversationOccurredAt.After(at) || !e.ConversationEligibleUntil.After(e.ConversationOccurredAt) {
		return time.Time{}, false
	}
	if configuredWindow <= 0 || configuredWindow > 24*time.Hour {
		configuredWindow = 24 * time.Hour
	}
	effectiveUntil := e.ConversationEligibleUntil
	policyUntil := e.ConversationOccurredAt.Add(configuredWindow)
	if effectiveUntil.After(policyUntil) {
		effectiveUntil = policyUntil
	}
	if !at.Before(effectiveUntil) {
		return time.Time{}, false
	}
	return effectiveUntil.UTC(), true
}

func validateMetaTemplateEvidence(e metaMaterialEvidence, messageType string) error {
	available := normalizedCapabilitySet(e.DefinitionCapabilities)
	if !available[string(provider.CapabilitySendTemplate)] {
		return errors.New("Meta provider definition lacks required capability: " + string(provider.CapabilitySendTemplate))
	}
	if e.TemplateStatus != "APPROVED" || len(e.BindingComponentHash) != 64 || e.BindingComponentHash != e.TemplateComponentHash {
		return errors.New("Meta template no longer matches approved message binding")
	}
	if strings.TrimSpace(e.BindingTemplateName) == "" || strings.TrimSpace(e.BindingLanguage) == "" {
		return errors.New("Meta template binding is incomplete")
	}
	expectedHeader := ""
	switch messageType {
	case "image":
		expectedHeader = "IMAGE"
	case "video":
		expectedHeader = "VIDEO"
	case "document":
		expectedHeader = "DOCUMENT"
	}
	if strings.ToUpper(strings.TrimSpace(e.BindingMediaHeaderType)) != expectedHeader {
		return errors.New("Meta template media header does not match canonical message type")
	}
	for _, binding := range e.BindingComponentBindings {
		if strings.ToUpper(strings.TrimSpace(binding.Type)) != "HEADER" {
			continue
		}
		if strings.ToUpper(strings.TrimSpace(binding.SubType)) != expectedHeader {
			return errors.New("Meta typed template media header does not match canonical message type")
		}
	}
	return nil
}

func resolveMetaBindingParameters(names []string, variables []message.Variable, values map[string]string) ([]string, error) {
	declared := make(map[string]message.Variable, len(variables))
	for _, variable := range variables {
		name := strings.TrimSpace(variable.Name)
		if name == "" {
			return nil, errors.New("Meta binding references an invalid message variable")
		}
		if _, exists := declared[name]; exists {
			return nil, errors.New("Meta binding message variables are duplicated")
		}
		declared[name] = variable
	}
	resolved := make([]string, 0, len(names))
	for _, rawName := range names {
		name := strings.TrimSpace(rawName)
		variable, ok := declared[name]
		if !ok {
			return nil, errors.New("Meta binding references an undeclared message variable: " + name)
		}
		value := strings.TrimSpace(values[name])
		if value == "" {
			value = strings.TrimSpace(variable.Fallback)
		}
		if value == "" {
			return nil, errors.New("Meta binding value is missing for message variable: " + name)
		}
		resolved = append(resolved, value)
	}
	return resolved, nil
}

func renderMetaBindingComponents(bindings []metacloud.ComponentBinding, legacyBodyNames []string, legacyMediaHeader string, variables []message.Variable, values map[string]string, mediaURL string) ([]MetaTemplateComponent, error) {
	if len(bindings) == 0 {
		if strings.TrimSpace(legacyMediaHeader) != "" {
			bindings = append(bindings, metacloud.ComponentBinding{Type: "HEADER", SubType: strings.ToUpper(strings.TrimSpace(legacyMediaHeader))})
		}
		if len(legacyBodyNames) > 0 {
			bindings = append(bindings, metacloud.ComponentBinding{Type: "BODY", SubType: "TEXT", ParameterNames: append([]string(nil), legacyBodyNames...)})
		}
	}
	out := make([]MetaTemplateComponent, 0, len(bindings))
	var typedBodyNames []string
	typedMediaHeader := ""
	seenBody, seenHeader := false, false
	for _, binding := range bindings {
		typeName := strings.ToUpper(strings.TrimSpace(binding.Type))
		subType := strings.ToUpper(strings.TrimSpace(binding.SubType))
		if binding.Index < 0 {
			return nil, errors.New("Meta component binding index is invalid")
		}
		switch typeName {
		case "BODY":
			if seenBody || binding.Index != 0 || (subType != "" && subType != "TEXT") {
				return nil, errors.New("Meta body binding is invalid")
			}
			seenBody = true
			typedBodyNames = append([]string(nil), binding.ParameterNames...)
			parameters, err := resolveMetaBindingParameters(binding.ParameterNames, variables, values)
			if err != nil {
				return nil, err
			}
			component := MetaTemplateComponent{Type: "BODY", SubType: "TEXT", Parameters: make([]MetaTemplateParameter, 0, len(parameters))}
			for _, value := range parameters {
				component.Parameters = append(component.Parameters, MetaTemplateParameter{Type: "TEXT", Text: value})
			}
			out = append(out, component)
		case "HEADER":
			if seenHeader || binding.Index != 0 || len(binding.ParameterNames) != 0 {
				return nil, errors.New("Meta media header binding is invalid")
			}
			seenHeader = true
			switch subType {
			case "IMAGE", "VIDEO", "DOCUMENT":
			default:
				return nil, errors.New("unsupported Meta media header binding")
			}
			if strings.TrimSpace(mediaURL) == "" {
				return nil, errors.New("Meta media header requires resolved media URL")
			}
			typedMediaHeader = subType
			out = append(out, MetaTemplateComponent{Type: "HEADER", SubType: subType, Parameters: []MetaTemplateParameter{{Type: subType, Link: strings.TrimSpace(mediaURL)}}})
		default:
			return nil, errors.New("unsupported Meta component binding")
		}
	}
	if len(legacyBodyNames) > 0 && !sameMetaBindingNames(legacyBodyNames, typedBodyNames) {
		return nil, errors.New("Meta typed body binding conflicts with legacy projection")
	}
	if legacyHeader := strings.ToUpper(strings.TrimSpace(legacyMediaHeader)); legacyHeader != "" && legacyHeader != typedMediaHeader {
		return nil, errors.New("Meta typed header binding conflicts with legacy projection")
	}
	return out, nil
}

func sameMetaBindingNames(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if strings.TrimSpace(left[i]) != strings.TrimSpace(right[i]) {
			return false
		}
	}
	return true
}
