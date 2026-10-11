package cohort

import (
	"errors"
	"fmt"
	"strings"
	"time"

	audiencefilter "campaign-platform/internal/audience/filter"
)

type EligibilityContext struct {
	OrganisationID string    `json:"organisationId"`
	PurposeID      string    `json:"purposeId"`
	Channel        string    `json:"channel"`
	AsOf           time.Time `json:"asOf"`
}

type CompiledQuery struct {
	SQL  string `json:"sql"`
	Args []any  `json:"args"`
}

type Compiler struct {
	registry *audiencefilter.Registry
}

func NewCompiler(registry *audiencefilter.Registry) *Compiler {
	return &Compiler{registry: registry}
}

func (c *Compiler) Compile(group audiencefilter.Group, eligibility EligibilityContext) (CompiledQuery, error) {
	return c.CompileForPermissions(group, eligibility, nil)
}

// CompileForPermissions is the untrusted-user entry point. It prevents callers
// from bypassing hidden sensitive filters by submitting their codes directly.
func (c *Compiler) CompileForPermissions(group audiencefilter.Group, eligibility EligibilityContext, hasPermission func(string) bool) (CompiledQuery, error) {
	builder, bindings, segmentSQL, err := c.compileEligibilityContext(group, eligibility, hasPermission)
	if err != nil {
		return CompiledQuery{}, err
	}
	sql := `SELECT c.id
FROM contacts c
WHERE c.status = 'ACTIVE'
  AND ` + activeOrganisationPredicate(bindings) + `
  AND ` + currentConsentPredicate("c.id", bindings) + `
  AND ` + notSuppressedPredicate("c.id", bindings) + `
  AND ` + notFrequencyCappedPredicate("c.id", bindings) + `
  AND (` + segmentSQL + `)`
	return CompiledQuery{SQL: sql, Args: builder.args}, nil
}

// CompileBreakdownForPermissions returns one authoritative query whose staged
// counts use the exact same eligibility predicates as CompileForPermissions.
// The stages are monotonic: matched profiles -> current consent -> unsuppressed
// -> final eligible after frequency caps.
func (c *Compiler) CompileBreakdownForPermissions(group audiencefilter.Group, eligibility EligibilityContext, hasPermission func(string) bool) (CompiledQuery, error) {
	builder, bindings, segmentSQL, err := c.compileEligibilityContext(group, eligibility, hasPermission)
	if err != nil {
		return CompiledQuery{}, err
	}
	sql := `WITH matched_profiles AS (
  SELECT c.id
  FROM contacts c
  WHERE c.status = 'ACTIVE'
    AND ` + activeOrganisationPredicate(bindings) + `
    AND (` + segmentSQL + `)
),
consent_eligible AS (
  SELECT m.id
  FROM matched_profiles m
  WHERE ` + currentConsentPredicate("m.id", bindings) + `
),
unsuppressed AS (
  SELECT c.id
  FROM consent_eligible c
  WHERE ` + notSuppressedPredicate("c.id", bindings) + `
),
final_eligible AS (
  SELECT c.id
  FROM unsuppressed c
  WHERE ` + notFrequencyCappedPredicate("c.id", bindings) + `
)
SELECT
  (SELECT count(*) FROM matched_profiles) AS matched_profiles,
  (SELECT count(*) FROM consent_eligible) AS consent_eligible,
  (SELECT count(*) FROM unsuppressed) AS unsuppressed,
  (SELECT count(*) FROM final_eligible) AS final_eligible`
	return CompiledQuery{SQL: sql, Args: builder.args}, nil
}

type eligibilityBindings struct {
	organisation string
	purpose      string
	channel      string
	asOf         string
}

func (c *Compiler) compileEligibilityContext(group audiencefilter.Group, eligibility EligibilityContext, hasPermission func(string) bool) (*sqlBuilder, eligibilityBindings, string, error) {
	if err := group.ValidateForPermissions(c.registry, hasPermission); err != nil {
		return nil, eligibilityBindings{}, "", err
	}
	if strings.TrimSpace(eligibility.OrganisationID) == "" {
		return nil, eligibilityBindings{}, "", errors.New("organisation ID is required")
	}
	if strings.TrimSpace(eligibility.PurposeID) == "" {
		return nil, eligibilityBindings{}, "", errors.New("purpose ID is required")
	}
	if strings.TrimSpace(eligibility.Channel) == "" {
		eligibility.Channel = "WHATSAPP"
	}
	if eligibility.AsOf.IsZero() {
		eligibility.AsOf = time.Now().UTC()
	}
	builder := &sqlBuilder{}
	bindings := eligibilityBindings{
		organisation: builder.add(eligibility.OrganisationID),
		purpose:      builder.add(eligibility.PurposeID),
		channel:      builder.add(strings.ToUpper(strings.TrimSpace(eligibility.Channel))),
		asOf:         builder.add(eligibility.AsOf.UTC()),
	}
	segmentSQL, err := c.compileGroup(group, builder)
	if err != nil {
		return nil, eligibilityBindings{}, "", err
	}
	return builder, bindings, segmentSQL, nil
}

func activeOrganisationPredicate(b eligibilityBindings) string {
	return `EXISTS (
    SELECT 1 FROM organisations o
    WHERE o.id = ` + b.organisation + `::uuid
      AND o.status = 'ACTIVE'
  )`
}

func currentConsentPredicate(contactID string, b eligibilityBindings) string {
	return `EXISTS (
    SELECT 1
    FROM consent_grants cg
    WHERE cg.contact_id = ` + contactID + `
      AND cg.organisation_id = ` + b.organisation + `
      AND cg.purpose_id = ` + b.purpose + `
      AND cg.channel = ` + b.channel + `
      AND cg.status = 'ACTIVE'
      AND cg.granted_at <= ` + b.asOf + `
      AND (cg.expires_at IS NULL OR cg.expires_at > ` + b.asOf + `)
      AND NOT EXISTS (
        SELECT 1
        FROM consent_grants newer
        WHERE newer.contact_id = cg.contact_id
          AND newer.organisation_id = cg.organisation_id
          AND newer.purpose_id = cg.purpose_id
          AND newer.channel = cg.channel
          AND newer.granted_at <= ` + b.asOf + `
          AND (
            newer.granted_at > cg.granted_at
            OR (newer.granted_at = cg.granted_at AND newer.created_at > cg.created_at)
            OR (newer.granted_at = cg.granted_at AND newer.created_at = cg.created_at AND newer.id > cg.id)
          )
      )
  )`
}

func notSuppressedPredicate(contactID string, b eligibilityBindings) string {
	return `NOT EXISTS (
    SELECT 1
    FROM suppressions s
    WHERE s.contact_id = ` + contactID + `
      AND s.active = true
      AND s.effective_at <= ` + b.asOf + `
      AND (s.expires_at IS NULL OR s.expires_at > ` + b.asOf + `)
      AND (
        s.scope = 'GLOBAL'
        OR (s.scope = 'ORGANISATION' AND s.organisation_id = ` + b.organisation + `)
        OR (s.scope = 'PURPOSE' AND s.purpose_id = ` + b.purpose + `)
        OR (s.scope = 'CHANNEL' AND s.channel = ` + b.channel + `)
        OR (s.scope = 'TEMPORARY' AND (
          s.organisation_id IS NULL OR s.organisation_id = ` + b.organisation + `
        ))
      )
  )`
}

func notFrequencyCappedPredicate(contactID string, b eligibilityBindings) string {
	return `NOT EXISTS (
    SELECT 1
    FROM organisation_policy_versions op
    CROSS JOIN LATERAL jsonb_to_recordset(op.frequency_caps)
      AS fc("purposeId" text, "channel" text, "maxMessages" integer, "windowHours" integer)
    WHERE op.organisation_id = ` + b.organisation + `::uuid
      AND op.status = 'ACTIVE'
      AND op.effective_from <= ` + b.asOf + `
      AND (op.effective_to IS NULL OR op.effective_to > ` + b.asOf + `)
      AND (coalesce(fc."purposeId", '') = '' OR fc."purposeId" = ` + b.purpose + `)
      AND upper(fc."channel") = upper(` + b.channel + `)
      AND (
        SELECT count(*)
        FROM campaign_recipients recent
        JOIN campaigns recent_campaign ON recent_campaign.id = recent.campaign_id
        WHERE recent.contact_id = ` + contactID + `
          AND recent_campaign.organisation_id = ` + b.organisation + `::uuid
          AND recent_campaign.purpose_id::text = ` + b.purpose + `
          AND recent.status NOT IN ('CANCELLED','SUPPRESSED_BEFORE_SEND')
          AND recent.authorised_at > ` + b.asOf + ` - make_interval(hours => fc."windowHours")
          AND recent.authorised_at <= ` + b.asOf + `
      ) >= fc."maxMessages"
  )`
}

func (c *Compiler) compileGroup(group audiencefilter.Group, builder *sqlBuilder) (string, error) {
	parts := make([]string, 0, len(group.Rules)+len(group.Children))
	for _, rule := range group.Rules {
		part, err := c.compileRule(rule, builder)
		if err != nil {
			return "", err
		}
		parts = append(parts, part)
	}
	for _, child := range group.Children {
		part, err := c.compileGroup(child, builder)
		if err != nil {
			return "", err
		}
		parts = append(parts, "("+part+")")
	}
	return strings.Join(parts, " "+string(group.Join)+" "), nil
}

func (c *Compiler) compileRule(rule audiencefilter.Rule, builder *sqlBuilder) (string, error) {
	definition, ok := c.registry.Get(strings.ToUpper(strings.TrimSpace(rule.DefinitionCode)))
	if !ok {
		return "", fmt.Errorf("unknown filter definition %s", rule.DefinitionCode)
	}
	if definition.Code == "CONSENT_PURPOSE" {
		return c.compileConsentPurposeRule(rule, builder)
	}
	if definition.Code == "COUNTRY" || definition.Code == "STATE" || definition.Code == "LGA" {
		return c.compileGeographyRule(definition.Code, rule, builder)
	}
	if definition.Storage == audiencefilter.StorageContactAttribute {
		return c.compileAttributeRule(definition, rule, builder)
	}
	field := definition.QueryableField
	switch rule.Operator {
	case audiencefilter.OperatorIsKnown:
		return field + " IS NOT NULL", nil
	case audiencefilter.OperatorIsUnknown:
		return field + " IS NULL", nil
	case audiencefilter.OperatorEquals:
		return field + " = " + builder.add(rule.Values[0]), nil
	case audiencefilter.OperatorNotEquals:
		return field + " <> " + builder.add(rule.Values[0]), nil
	case audiencefilter.OperatorContains:
		return field + " ILIKE " + builder.add("%"+escapeLikeLiteral(fmt.Sprint(rule.Values[0]))+"%") + " ESCAPE '\\'", nil
	case audiencefilter.OperatorGreaterThan:
		return field + " > " + builder.add(rule.Values[0]), nil
	case audiencefilter.OperatorLessThan:
		return field + " < " + builder.add(rule.Values[0]), nil
	case audiencefilter.OperatorBetween:
		return field + " BETWEEN " + builder.add(rule.Values[0]) + " AND " + builder.add(rule.Values[1]), nil
	case audiencefilter.OperatorIn, audiencefilter.OperatorNotIn:
		placeholders := make([]string, 0, len(rule.Values))
		for _, value := range rule.Values {
			placeholders = append(placeholders, builder.add(value))
		}
		operator := "IN"
		if rule.Operator == audiencefilter.OperatorNotIn {
			operator = "NOT IN"
		}
		return field + " " + operator + " (" + strings.Join(placeholders, ", ") + ")", nil
	default:
		return "", fmt.Errorf("operator %s is not supported by the SQL compiler", rule.Operator)
	}
}

func (c *Compiler) compileGeographyRule(code string, rule audiencefilter.Rule, builder *sqlBuilder) (string, error) {
	var idField, tableAlias, referenceTable, referenceCode string
	switch code {
	case "COUNTRY":
		idField, tableAlias, referenceTable, referenceCode = "c.country_id", "geo", "countries", "iso2"
	case "STATE":
		idField, tableAlias, referenceTable, referenceCode = "c.state_id", "geo", "administrative_areas", "code"
	case "LGA":
		idField, tableAlias, referenceTable, referenceCode = "c.lga_id", "geo", "administrative_areas", "code"
	default:
		return "", fmt.Errorf("unsupported geography definition %s", code)
	}
	switch rule.Operator {
	case audiencefilter.OperatorIsKnown:
		return idField + " IS NOT NULL", nil
	case audiencefilter.OperatorIsUnknown:
		return idField + " IS NULL", nil
	case audiencefilter.OperatorIn, audiencefilter.OperatorNotIn:
		placeholders := make([]string, 0, len(rule.Values))
		for _, value := range rule.Values {
			placeholders = append(placeholders, "upper("+builder.add(value)+"::text)")
		}
		predicate := "upper(" + tableAlias + "." + referenceCode + ") IN (" + strings.Join(placeholders, ", ") + ")"
		if rule.Operator == audiencefilter.OperatorNotIn {
			predicate = "upper(" + tableAlias + "." + referenceCode + ") NOT IN (" + strings.Join(placeholders, ", ") + ")"
		}
		return `EXISTS (
    SELECT 1 FROM ` + referenceTable + ` ` + tableAlias + `
    WHERE ` + tableAlias + `.id = ` + idField + `
      AND ` + tableAlias + `.active = true
      AND ` + predicate + `
  )`, nil
	default:
		return "", fmt.Errorf("operator %s is not supported for geography", rule.Operator)
	}
}

func (c *Compiler) compileConsentPurposeRule(rule audiencefilter.Rule, builder *sqlBuilder) (string, error) {
	placeholders := make([]string, 0, len(rule.Values))
	for _, value := range rule.Values {
		placeholders = append(placeholders, builder.add(value))
	}
	operator := "IN"
	if rule.Operator == audiencefilter.OperatorNotIn {
		operator = "NOT IN"
	}
	return `EXISTS (
    SELECT 1 FROM consent_grants cgp
    WHERE cgp.contact_id = c.id
      AND cgp.status = 'ACTIVE'
      AND cgp.purpose_id ` + operator + ` (` + strings.Join(placeholders, ", ") + `)
  )`, nil
}

func (c *Compiler) compileAttributeRule(definition audiencefilter.Definition, rule audiencefilter.Rule, builder *sqlBuilder) (string, error) {
	codePlaceholder := builder.add(definition.Code)
	field := "cav." + definition.AttributeValueColumn
	var predicate string
	switch rule.Operator {
	case audiencefilter.OperatorIsKnown:
		predicate = field + " IS NOT NULL"
	case audiencefilter.OperatorIsUnknown:
		return `NOT EXISTS (
    SELECT 1
    FROM contact_attribute_values cav
    JOIN attribute_definitions ad ON ad.id = cav.attribute_definition_id
    WHERE cav.contact_id = c.id AND ad.code = ` + codePlaceholder + `
  )`, nil
	case audiencefilter.OperatorEquals:
		predicate = field + " = " + builder.add(rule.Values[0])
	case audiencefilter.OperatorNotEquals:
		return c.compileAttributeNegativeMatch(definition, field+" = "+builder.add(rule.Values[0]), codePlaceholder), nil
	case audiencefilter.OperatorContains:
		predicate = field + " ILIKE " + builder.add("%"+escapeLikeLiteral(fmt.Sprint(rule.Values[0]))+"%") + " ESCAPE '\\'"
	case audiencefilter.OperatorGreaterThan:
		predicate = field + " > " + builder.add(rule.Values[0])
	case audiencefilter.OperatorLessThan:
		predicate = field + " < " + builder.add(rule.Values[0])
	case audiencefilter.OperatorBetween:
		predicate = field + " BETWEEN " + builder.add(rule.Values[0]) + " AND " + builder.add(rule.Values[1])
	case audiencefilter.OperatorIn, audiencefilter.OperatorNotIn:
		placeholders := make([]string, 0, len(rule.Values))
		for _, value := range rule.Values {
			placeholders = append(placeholders, builder.add(value))
		}
		match := field + " IN (" + strings.Join(placeholders, ", ") + ")"
		if rule.Operator == audiencefilter.OperatorNotIn {
			return c.compileAttributeNegativeMatch(definition, match, codePlaceholder), nil
		}
		predicate = match
	default:
		return "", fmt.Errorf("operator %s is not supported for dynamic attributes", rule.Operator)
	}
	return `EXISTS (
    SELECT 1
    FROM contact_attribute_values cav
    JOIN attribute_definitions ad ON ad.id = cav.attribute_definition_id
    WHERE cav.contact_id = c.id
      AND ad.code = ` + codePlaceholder + `
      AND ` + predicate + `
  )`, nil
}

type sqlBuilder struct {
	args []any
}

func (b *sqlBuilder) add(value any) string {
	b.args = append(b.args, value)
	return fmt.Sprintf("$%d", len(b.args))
}

func (c *Compiler) compileAttributeNegativeMatch(definition audiencefilter.Definition, matchPredicate, codePlaceholder string) string {
	return `NOT EXISTS (
    SELECT 1
    FROM contact_attribute_values cav
    JOIN attribute_definitions ad ON ad.id = cav.attribute_definition_id
    WHERE cav.contact_id = c.id
      AND ad.code = ` + codePlaceholder + `
      AND ` + matchPredicate + `
  )`
}

func escapeLikeLiteral(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}
