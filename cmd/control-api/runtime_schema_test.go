package main

import (
	"strings"
	"testing"
)

func TestControlSchemaReadinessUsesCanonicalRelations(t *testing.T) {
	for _, required := range []string{
		"public.delivery_events",
		"public.campaign_metric_reconciliations",
	} {
		if !strings.Contains(controlSchemaReadinessQuery, required) {
			t.Fatalf("schema readiness query must contain %q", required)
		}
	}
	for _, obsolete := range []string{
		"public.delivery_provider_events",
		"public.campaign_metric_reconciliation'",
	} {
		if strings.Contains(controlSchemaReadinessQuery, obsolete) {
			t.Fatalf("schema readiness query contains obsolete relation %q", obsolete)
		}
	}
}
func TestControlSchemaReadinessRequiresAuthenticationExportSchema(t *testing.T) {
	for _, required := range []string{
		"public.authentication_events",
		"table_name='export_requests' AND column_name='authentication_event_as_of'",
		"table_name='export_requests' AND column_name='authentication_event_head_id'",
	} {
		if !strings.Contains(controlSchemaReadinessQuery, required) {
			t.Fatalf("schema readiness query does not require authentication export component %s", required)
		}
	}
}

func TestControlSchemaReadinessRequiresSenderOperationalMetadataColumns(t *testing.T) {
	for _, column := range []string{"owner_reference", "registration_country_iso2", "profile_display_name", "recovery_reference"} {
		needle := "table_name='sender_sessions' AND column_name='" + column + "'"
		if !strings.Contains(controlSchemaReadinessQuery, needle) {
			t.Fatalf("schema readiness query does not require sender metadata column %s", column)
		}
	}
}
