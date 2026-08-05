package dispatch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"campaign-platform/internal/delivery"
	"campaign-platform/internal/message"
	"campaign-platform/internal/provider"
	"campaign-platform/internal/sender"
	sharedcrypto "campaign-platform/internal/shared/crypto"
)

type ObjectResolver interface {
	Resolve(context.Context, string, time.Duration) (string, error)
}

type PostgreSQLMaterialLoader struct {
	DB          *sql.DB
	Protector   *sharedcrypto.MSISDNProtector
	Allocator   sender.Allocator
	Objects     ObjectResolver
	MediaURLTTL time.Duration
	Clock       func() time.Time
}

func (l *PostgreSQLMaterialLoader) Load(ctx context.Context, recipient delivery.Recipient) (Material, error) {
	if l == nil || l.DB == nil || l.Protector == nil || l.Allocator == nil {
		return Material{}, errors.New("material loader dependencies are required")
	}
	now := time.Now().UTC()
	if l.Clock != nil {
		now = l.Clock().UTC()
	}
	var encrypted []byte
	var messageType, body, mediaObjectKey, maskedMSISDN string
	var campaignName, organisationName, country, state, lga, gender, language string
	var reportedAge sql.NullInt64
	var variablesJSON []byte
	const query = `
SELECT c.encrypted_msisdn,c.masked_msisdn,mv.message_type,coalesce(mv.body,''),coalesce(mv.media_object_key,''),mv.variables,
       cp.name,o.legal_name,coalesce(country.name,''),coalesce(state.name,''),coalesce(lga.name,''),c.reported_age,coalesce(c.gender_code,''),coalesce(c.preferred_language_code,'')
FROM campaign_recipients cr
JOIN contacts c ON c.id=cr.contact_id
JOIN message_versions mv ON mv.id=cr.message_version_id AND mv.status='APPROVED'
JOIN campaigns cp ON cp.id=cr.campaign_id
JOIN organisations o ON o.id=cp.organisation_id
LEFT JOIN countries country ON country.id=c.country_id
LEFT JOIN administrative_areas state ON state.id=c.state_id
LEFT JOIN administrative_areas lga ON lga.id=c.lga_id
WHERE cr.id=$1::uuid AND cr.campaign_id=$2::uuid AND cr.contact_id=$3::uuid AND cr.message_version_id=$4::uuid`
	if err := l.DB.QueryRowContext(ctx, query, recipient.ID, recipient.CampaignID, recipient.ContactID, recipient.MessageVersionID).Scan(&encrypted, &maskedMSISDN, &messageType, &body, &mediaObjectKey, &variablesJSON, &campaignName, &organisationName, &country, &state, &lga, &reportedAge, &gender, &language); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Material{}, delivery.ErrRecipientNotFound
		}
		return Material{}, fmt.Errorf("load dispatch evidence: %w", err)
	}
	mappedType, err := gatewayMessageType(messageType)
	if err != nil {
		return Material{}, PermanentMaterialError{Err: err}
	}
	selection, err := l.loadAllocationRoute(ctx, recipient.ID)
	if err != nil {
		return Material{}, err
	}
	sessionID, err := l.Allocator.Assign(ctx, recipient.ID, selection.Allocation, now)
	if err != nil {
		return Material{}, fmt.Errorf("assign sender: %w", err)
	}
	route, err := l.loadGovernedRoute(ctx, recipient.ID, sessionID, mappedType, now)
	if err != nil {
		return Material{}, err
	}
	e164, err := l.Protector.Decrypt(encrypted)
	if err != nil {
		return Material{}, PermanentMaterialError{Err: errors.New("decrypt recipient address")}
	}
	var variables []message.Variable
	if len(variablesJSON) > 0 {
		if err := json.Unmarshal(variablesJSON, &variables); err != nil {
			return Material{}, PermanentMaterialError{Err: errors.New("approved message variables are invalid")}
		}
	}
	values := map[string]string{
		"campaign.name": campaignName, "organisation.name": organisationName,
		"contact.msisdn_masked": maskedMSISDN, "contact.country": country, "contact.state": state,
		"contact.lga": lga, "contact.gender": gender, "contact.preferred_language": language,
	}
	if reportedAge.Valid {
		values["contact.reported_age"] = strconv.FormatInt(reportedAge.Int64, 10)
	}
	dynamicValues, err := loadDynamicMessageValues(ctx, l.DB, recipient.ContactID)
	if err != nil {
		return Material{}, fmt.Errorf("load message attributes: %w", err)
	}
	for key, value := range dynamicValues {
		values[key] = value
	}
	rendered, err := message.Render(message.Version{Status: message.StatusApproved, Body: body, Variables: variables}, message.RenderInput{Mode: message.RenderDispatch, Values: values})
	if err != nil {
		return Material{}, PermanentMaterialError{Err: fmt.Errorf("render approved message: %w", err)}
	}
	body = rendered.Body
	mediaURL := ""
	if mappedType != "text" {
		if l.Objects == nil {
			return Material{}, PermanentMaterialError{Err: errors.New("media object resolver is required")}
		}
		if strings.TrimSpace(mediaObjectKey) == "" {
			return Material{}, PermanentMaterialError{Err: errors.New("approved media message has no object key")}
		}
		ttl := l.MediaURLTTL
		if ttl <= 0 || ttl > time.Hour {
			ttl = 15 * time.Minute
		}
		mediaURL, err = l.Objects.Resolve(ctx, mediaObjectKey, ttl)
		if err != nil {
			return Material{}, fmt.Errorf("resolve media object: %w", err)
		}
	}
	return Material{SenderPoolID: selection.SenderPoolID, Provider: route.Provider, Engine: route.Engine, GatewayPoolID: route.GatewayPoolID, SessionID: sessionID, RecipientE164: e164, MessageType: mappedType, Body: body, MediaObjectURL: mediaURL, ClientReference: recipient.ID}, nil
}

type allocationRouteSelection struct {
	Allocation   sender.AllocationRoute
	SenderPoolID string
}

func (l *PostgreSQLMaterialLoader) loadAllocationRoute(ctx context.Context, recipientID string) (allocationRouteSelection, error) {
	const query = `
SELECT coalesce(sh.assigned_sender_pool_id::text,cp.transport_sender_pool_id::text,''),
       coalesce(cp.sender_pool,''),
       CASE WHEN sh.routing_plan_id IS NULL THEN coalesce(cp.gateway_pool_id,'') ELSE coalesce(rp.gateway_pool_id::text,'') END,
       CASE WHEN sh.routing_plan_id IS NULL THEN coalesce(cp.transport_session_id::text,'') ELSE '' END,
       sh.routing_plan_id IS NOT NULL,
       rp.routing_plan_id IS NOT NULL
FROM campaign_recipients cr
JOIN campaigns cp ON cp.id=cr.campaign_id
LEFT JOIN campaign_dispatch_shards sh ON sh.id=cr.dispatch_shard_id
LEFT JOIN campaign_routing_plan_pools rp
  ON rp.routing_plan_id=sh.routing_plan_id
 AND rp.sender_pool_id=sh.assigned_sender_pool_id
WHERE cr.id=$1::uuid`
	var senderPoolID, legacyPool, gatewayPoolID, specificSessionID string
	var hasRoutingPlan, hasRoute bool
	if err := l.DB.QueryRowContext(ctx, query, recipientID).Scan(&senderPoolID, &legacyPool, &gatewayPoolID, &specificSessionID, &hasRoutingPlan, &hasRoute); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return allocationRouteSelection{}, PermanentMaterialError{Err: errors.New("campaign recipient route is unavailable")}
		}
		return allocationRouteSelection{}, fmt.Errorf("load recipient allocation route: %w", err)
	}
	if hasRoutingPlan && !hasRoute {
		return allocationRouteSelection{}, PermanentMaterialError{Err: errors.New("dispatch shard has no frozen routing-plan route")}
	}
	route := sender.AllocationRoute{
		SenderPoolID: strings.TrimSpace(senderPoolID), LegacyPool: strings.TrimSpace(legacyPool),
		GatewayPoolID: strings.TrimSpace(gatewayPoolID), SpecificSessionID: strings.TrimSpace(specificSessionID),
	}
	if route.SenderPoolID != "" {
		route.LegacyPool = ""
	}
	if err := route.Validate(); err != nil {
		return allocationRouteSelection{}, PermanentMaterialError{Err: fmt.Errorf("invalid frozen allocation route: %w", err)}
	}
	label := route.SenderPoolID
	if label == "" {
		label = route.LegacyPool
	}
	return allocationRouteSelection{Allocation: route, SenderPoolID: label}, nil
}

type governedRouteEvidence struct {
	SessionID                       string
	SessionSenderPoolID             string
	GatewayPoolID                   string
	GatewayPoolVersion              int64
	GatewayProvider                 string
	GatewayEngine                   string
	GatewayAdapterVersion           string
	GatewayStatus                   string
	GatewayCapabilities             []string
	ExpectedSessionID               string
	ExpectedSenderPoolID            string
	ExpectedGatewayPoolID           string
	ExpectedGatewayPoolVersion      int64
	ExpectedProvider                string
	ExpectedEngine                  string
	ExpectedAdapterVersion          string
	ProviderDefinitionID            string
	ProviderDefinitionVersion       int64
	CampaignRequiredCapabilities    []string
	DefinitionVersion               int64
	DefinitionProvider              string
	DefinitionChannel               string
	DefinitionEngine                string
	DefinitionAdapterVersion        string
	DefinitionMinimumGatewayVersion string
	DefinitionCapabilities          []string
	DefinitionStatus                string
	DefinitionEffectiveFrom         time.Time
	DefinitionEffectiveTo           *time.Time
}

type governedRoute struct {
	GatewayPoolID string
	Provider      string
	Engine        string
}

func (l *PostgreSQLMaterialLoader) loadGovernedRoute(ctx context.Context, recipientID, sessionID, messageType string, at time.Time) (governedRoute, error) {
	const query = `
SELECT ss.id::text,coalesce(ss.sender_pool_id::text,''),ss.gateway_pool_id::text,gp.version,gp.provider,gp.engine,gp.adapter_version,gp.status,gp.capabilities,
       CASE WHEN sh.routing_plan_id IS NULL THEN coalesce(cp.transport_session_id::text,'') ELSE '' END,
       CASE WHEN sh.routing_plan_id IS NULL THEN coalesce(cp.transport_sender_pool_id::text,'') ELSE coalesce(rp.sender_pool_id::text,'') END,
       CASE WHEN sh.routing_plan_id IS NULL THEN coalesce(cp.gateway_pool_id,'') ELSE coalesce(rp.gateway_pool_id::text,'') END,
       CASE WHEN sh.routing_plan_id IS NULL THEN coalesce(cp.gateway_pool_version,0) ELSE coalesce(rp.gateway_pool_version,0) END,
       CASE WHEN sh.routing_plan_id IS NULL THEN coalesce(cp.transport_provider,'') ELSE coalesce(rp.provider,'') END,
       CASE WHEN sh.routing_plan_id IS NULL THEN coalesce(cp.transport_engine,'') ELSE coalesce(rp.engine,'') END,
       CASE WHEN sh.routing_plan_id IS NULL THEN coalesce(cp.provider_adapter_version,'') ELSE coalesce(rp.provider_adapter_version,'') END,
       CASE WHEN sh.routing_plan_id IS NULL THEN coalesce(cp.provider_capability_definition_id::text,'') ELSE coalesce(rp.provider_capability_definition_id::text,'') END,
       CASE WHEN sh.routing_plan_id IS NULL THEN coalesce(cp.provider_capability_definition_version,0) ELSE coalesce(rp.provider_capability_definition_version,0) END,
       coalesce(cp.required_capabilities,'[]'::jsonb),
       coalesce(pd.version,0),coalesce(pd.provider,''),coalesce(pd.channel,''),coalesce(pd.engine,''),coalesce(pd.adapter_version,''),
       coalesce(pd.minimum_gateway_version,''),coalesce(pd.capabilities,ARRAY[]::text[]),coalesce(pd.status,''),
       pd.effective_from,pd.effective_to
FROM campaign_recipients cr
JOIN campaigns cp ON cp.id=cr.campaign_id
LEFT JOIN campaign_dispatch_shards sh ON sh.id=cr.dispatch_shard_id
LEFT JOIN campaign_routing_plan_pools rp
  ON rp.routing_plan_id=sh.routing_plan_id
 AND rp.sender_pool_id=sh.assigned_sender_pool_id
JOIN sender_sessions ss ON ss.id=$1::uuid
JOIN gateway_pools gp ON gp.id=ss.gateway_pool_id
LEFT JOIN provider_capability_definitions pd ON pd.id=(CASE WHEN sh.routing_plan_id IS NULL THEN cp.provider_capability_definition_id ELSE rp.provider_capability_definition_id END)
WHERE cr.id=$2::uuid`
	var evidence governedRouteEvidence
	var gatewayCapsJSON, requiredJSON []byte
	var effectiveFrom, effectiveTo sql.NullTime
	if err := l.DB.QueryRowContext(ctx, query, sessionID, recipientID).Scan(
		&evidence.SessionID, &evidence.SessionSenderPoolID, &evidence.GatewayPoolID, &evidence.GatewayPoolVersion,
		&evidence.GatewayProvider, &evidence.GatewayEngine, &evidence.GatewayAdapterVersion, &evidence.GatewayStatus, &gatewayCapsJSON,
		&evidence.ExpectedSessionID, &evidence.ExpectedSenderPoolID, &evidence.ExpectedGatewayPoolID, &evidence.ExpectedGatewayPoolVersion,
		&evidence.ExpectedProvider, &evidence.ExpectedEngine, &evidence.ExpectedAdapterVersion,
		&evidence.ProviderDefinitionID, &evidence.ProviderDefinitionVersion, &requiredJSON,
		&evidence.DefinitionVersion, &evidence.DefinitionProvider, &evidence.DefinitionChannel, &evidence.DefinitionEngine,
		&evidence.DefinitionAdapterVersion, &evidence.DefinitionMinimumGatewayVersion, &evidence.DefinitionCapabilities,
		&evidence.DefinitionStatus, &effectiveFrom, &effectiveTo,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return governedRoute{}, PermanentMaterialError{Err: errors.New("assigned sender session or frozen route is not governed")}
		}
		return governedRoute{}, fmt.Errorf("load governed gateway route: %w", err)
	}
	if err := json.Unmarshal(gatewayCapsJSON, &evidence.GatewayCapabilities); err != nil {
		return governedRoute{}, PermanentMaterialError{Err: errors.New("gateway pool capabilities are invalid")}
	}
	if err := json.Unmarshal(requiredJSON, &evidence.CampaignRequiredCapabilities); err != nil {
		return governedRoute{}, PermanentMaterialError{Err: errors.New("campaign required capabilities are invalid")}
	}
	if effectiveFrom.Valid {
		evidence.DefinitionEffectiveFrom = effectiveFrom.Time.UTC()
	}
	if effectiveTo.Valid {
		value := effectiveTo.Time.UTC()
		evidence.DefinitionEffectiveTo = &value
	}
	if err := validateGovernedRouteEvidence(evidence, messageType, at.UTC()); err != nil {
		return governedRoute{}, PermanentMaterialError{Err: err}
	}
	return governedRoute{GatewayPoolID: evidence.GatewayPoolID, Provider: evidence.GatewayProvider, Engine: evidence.GatewayEngine}, nil
}

func validateGovernedRouteEvidence(e governedRouteEvidence, messageType string, at time.Time) error {
	if e.ProviderDefinitionID == "" || e.ProviderDefinitionVersion <= 0 {
		return errors.New("route has no frozen provider capability definition")
	}
	if e.DefinitionVersion != e.ProviderDefinitionVersion {
		return errors.New("frozen provider capability definition version has changed")
	}
	if e.DefinitionStatus != string(provider.StatusActive) || e.DefinitionEffectiveFrom.IsZero() || e.DefinitionEffectiveFrom.After(at) || (e.DefinitionEffectiveTo != nil && !e.DefinitionEffectiveTo.After(at)) {
		return errors.New("frozen provider capability definition is not active for dispatch")
	}
	if e.GatewayStatus != string(sender.GatewayPoolActive) {
		return errors.New("assigned gateway pool is not active")
	}
	if e.ExpectedGatewayPoolVersion <= 0 || e.GatewayPoolVersion != e.ExpectedGatewayPoolVersion {
		return errors.New("assigned gateway pool version does not match the frozen route")
	}
	if e.ExpectedSessionID != "" && e.SessionID != e.ExpectedSessionID {
		return errors.New("assigned session does not match the frozen specific-session route")
	}
	if e.ExpectedSenderPoolID != "" && e.SessionSenderPoolID != e.ExpectedSenderPoolID {
		return errors.New("assigned session does not belong to the frozen sender pool")
	}
	if e.GatewayPoolID == "" || e.GatewayPoolID != e.ExpectedGatewayPoolID {
		return errors.New("assigned session gateway pool does not match the frozen route")
	}
	if e.GatewayProvider != e.ExpectedProvider || e.GatewayProvider != e.DefinitionProvider || e.DefinitionChannel != "WHATSAPP" {
		return errors.New("provider capability evidence is inconsistent")
	}
	if e.GatewayEngine != e.ExpectedEngine || e.GatewayEngine != e.DefinitionEngine {
		return errors.New("provider engine evidence is inconsistent")
	}
	if e.GatewayAdapterVersion != e.ExpectedAdapterVersion || e.GatewayAdapterVersion != e.DefinitionAdapterVersion {
		return errors.New("provider adapter version evidence is inconsistent")
	}
	if e.DefinitionMinimumGatewayVersion != "" {
		ok, err := provider.VersionAtLeast(e.GatewayAdapterVersion, e.DefinitionMinimumGatewayVersion)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("gateway version is below the governed provider minimum")
		}
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
		return errors.New("unsupported governed message type")
	}
	definitionCaps := normalizedCapabilitySet(e.DefinitionCapabilities)
	gatewayCaps := normalizedCapabilitySet(e.GatewayCapabilities)
	for _, capability := range required {
		capability = strings.ToUpper(strings.TrimSpace(capability))
		if capability == "" {
			continue
		}
		if !definitionCaps[capability] {
			return errors.New("frozen provider definition lacks required capability: " + capability)
		}
		if !gatewayCaps[capability] {
			return errors.New("assigned gateway pool lacks required capability: " + capability)
		}
	}
	return nil
}

func normalizedCapabilitySet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.ToUpper(strings.TrimSpace(value))
		if value != "" {
			result[value] = true
		}
	}
	return result
}

func loadDynamicMessageValues(ctx context.Context, db *sql.DB, contactID string) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `
SELECT lower(ad.code),
       coalesce(cav.value_text,cav.value_integer::text,cav.value_decimal::text,cav.value_boolean::text,cav.value_date::text,cav.value_json::text,'')
FROM contact_attribute_values cav
JOIN attribute_definitions ad ON ad.id=cav.attribute_definition_id
WHERE cav.contact_id=$1::uuid
  AND ad.active
  AND cav.recorded_at=(SELECT max(latest.recorded_at) FROM contact_attribute_values latest WHERE latest.contact_id=cav.contact_id AND latest.attribute_definition_id=cav.attribute_definition_id)`, contactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := map[string]string{}
	for rows.Next() {
		var code, value string
		if err := rows.Scan(&code, &value); err != nil {
			return nil, err
		}
		values["contact."+code] = value
		values[code] = value
	}
	return values, rows.Err()
}

func gatewayMessageType(value string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "TEXT":
		return "text", nil
	case "IMAGE_CAPTION", "IMAGE":
		return "image", nil
	case "VIDEO":
		return "video", nil
	case "DOCUMENT":
		return "document", nil
	default:
		return "", fmt.Errorf("unsupported approved message type %q", value)
	}
}

type PostgreSQLFinalEligibility struct{ DB *sql.DB }

func (c *PostgreSQLFinalEligibility) Check(ctx context.Context, recipient delivery.Recipient, asOf time.Time) (EligibilityDecision, error) {
	if c == nil || c.DB == nil {
		return EligibilityDecision{}, errors.New("database is required")
	}
	const query = `
WITH basis AS (
  SELECT cr.contact_id, cp.organisation_id, cp.purpose_id, cp.status AS campaign_status,
         cp.requested_start_at, cp.completion_deadline_at, coalesce(cp.campaign_timezone,'UTC') AS campaign_timezone,
         cp.quiet_hours_start, cp.quiet_hours_end,
         ct.status AS contact_status, o.status AS organisation_status,
         rv.status AS review_status, rv.channel AS review_channel, rv.expires_at AS review_expires_at
  FROM campaign_recipients cr
  JOIN contacts ct ON ct.id=cr.contact_id
  JOIN campaigns cp ON cp.id=cr.campaign_id
  JOIN organisations o ON o.id=cp.organisation_id
  JOIN consent_reviews rv ON rv.id=cp.consent_review_id
  WHERE cr.id=$1::uuid
), latest_grant AS (
  SELECT cg.status, cg.expires_at
  FROM consent_grants cg, basis b
  WHERE cg.contact_id=b.contact_id
    AND cg.organisation_id=b.organisation_id
    AND cg.purpose_id=b.purpose_id
    AND upper(cg.channel)='WHATSAPP'
    AND cg.effective_from <= $2
  ORDER BY cg.effective_from DESC, cg.created_at DESC, cg.id DESC
  LIMIT 1
)
SELECT CASE
  WHEN b.contact_status <> 'ACTIVE' THEN 'CONTACT_INACTIVE'
  WHEN b.organisation_status <> 'ACTIVE' THEN 'ORGANISATION_INACTIVE'
  WHEN b.campaign_status NOT IN ('SCHEDULED','DISPATCHING') THEN 'CAMPAIGN_NOT_DISPATCHABLE'
  WHEN b.requested_start_at IS NOT NULL AND $2 < b.requested_start_at THEN 'CAMPAIGN_NOT_STARTED'
  WHEN b.completion_deadline_at IS NOT NULL AND $2 >= b.completion_deadline_at THEN 'CAMPAIGN_DEADLINE_PASSED'
  WHEN b.quiet_hours_start IS NOT NULL AND (
    CASE
      WHEN b.quiet_hours_start::time < b.quiet_hours_end::time THEN
        ($2 AT TIME ZONE b.campaign_timezone)::time >= b.quiet_hours_start::time
        AND ($2 AT TIME ZONE b.campaign_timezone)::time < b.quiet_hours_end::time
      ELSE
        ($2 AT TIME ZONE b.campaign_timezone)::time >= b.quiet_hours_start::time
        OR ($2 AT TIME ZONE b.campaign_timezone)::time < b.quiet_hours_end::time
    END
  ) THEN 'CAMPAIGN_QUIET_HOURS'
  WHEN b.review_status <> 'APPROVED' OR b.review_expires_at <= $2 OR upper(b.review_channel) <> 'WHATSAPP' THEN 'CONSENT_REVIEW_INVALID'
  WHEN EXISTS (
    SELECT 1 FROM suppressions sp
    WHERE sp.contact_id=b.contact_id AND sp.active AND sp.effective_at<=$2
      AND (sp.expires_at IS NULL OR sp.expires_at>$2)
      AND (sp.scope='GLOBAL'
        OR (sp.scope='ORGANISATION' AND sp.organisation_id=b.organisation_id)
        OR (sp.scope='PURPOSE' AND sp.purpose_id=b.purpose_id)
        OR (sp.scope='CHANNEL' AND upper(sp.channel)='WHATSAPP')
        OR (sp.scope='TEMPORARY'
           AND (sp.organisation_id IS NULL OR sp.organisation_id=b.organisation_id)
           AND (sp.purpose_id IS NULL OR sp.purpose_id=b.purpose_id)
           AND (sp.channel IS NULL OR upper(sp.channel)='WHATSAPP')))
  ) THEN 'SUPPRESSED'
  WHEN EXISTS (
    SELECT 1
    FROM organisation_policy_versions op
    CROSS JOIN LATERAL jsonb_to_recordset(op.frequency_caps)
      AS fc("purposeId" text, "channel" text, "maxMessages" integer, "windowHours" integer)
    WHERE op.organisation_id=b.organisation_id
      AND op.status='ACTIVE'
      AND op.effective_from<=$2
      AND (op.effective_to IS NULL OR op.effective_to>$2)
      AND (coalesce(fc."purposeId", '')='' OR fc."purposeId"=b.purpose_id)
      AND upper(fc."channel")='WHATSAPP'
      AND (
        SELECT count(*)
        FROM campaign_recipients recent
        JOIN campaigns recent_campaign ON recent_campaign.id=recent.campaign_id
        WHERE recent.contact_id=b.contact_id
          AND recent_campaign.organisation_id=b.organisation_id
          AND recent_campaign.purpose_id=b.purpose_id
          AND recent.status NOT IN ('CANCELLED','SUPPRESSED_BEFORE_SEND')
          AND recent.authorised_at>$2-make_interval(hours=>fc."windowHours")
      )>=fc."maxMessages"
  ) THEN 'FREQUENCY_CAPPED'
  WHEN NOT EXISTS (SELECT 1 FROM latest_grant) THEN 'NO_ACTIVE_CONSENT'
  WHEN (SELECT status FROM latest_grant)='WITHDRAWN' THEN 'CONSENT_WITHDRAWN'
  WHEN (SELECT status FROM latest_grant)='REVOKED' THEN 'CONSENT_REVOKED'
  WHEN (SELECT status FROM latest_grant)='EXPIRED'
    OR ((SELECT status FROM latest_grant)='ACTIVE'
        AND (SELECT expires_at FROM latest_grant) IS NOT NULL
        AND (SELECT expires_at FROM latest_grant) <= $2) THEN 'CONSENT_EXPIRED'
  WHEN (SELECT status FROM latest_grant) <> 'ACTIVE' THEN 'NO_ACTIVE_CONSENT'
  ELSE ''
END AS exclusion_reason
FROM basis b`
	var reason string
	err := c.DB.QueryRowContext(ctx, query, recipient.ID, asOf.UTC()).Scan(&reason)
	if errors.Is(err, sql.ErrNoRows) {
		return EligibilityDecision{Eligible: false, Reason: "RECIPIENT_NOT_FOUND"}, nil
	}
	if err != nil {
		return EligibilityDecision{}, err
	}
	reason = strings.TrimSpace(reason)
	if reason != "" {
		return EligibilityDecision{Eligible: false, Reason: reason}, nil
	}
	return EligibilityDecision{Eligible: true}, nil
}
