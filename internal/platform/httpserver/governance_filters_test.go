package httpserver

import (
	"testing"

	"campaign-platform/internal/operations"
	"campaign-platform/internal/platformpolicy"
	"campaign-platform/internal/retention"
)

func TestGovernanceQueryFilterValidation(t *testing.T) {
	if !validPlatformScope(platformpolicy.ScopeGatewayPool) || validPlatformScope("ARBITRARY") {
		t.Fatal("platform scope validation failed")
	}
	if !validConfigurationStatus(platformpolicy.StatusActive) || validConfigurationStatus("BROKEN") {
		t.Fatal("configuration status validation failed")
	}
	if !validMaintenanceStatus(platformpolicy.MaintenanceCancelled) || validMaintenanceStatus("BROKEN") {
		t.Fatal("maintenance status validation failed")
	}
	if !validRetentionPolicyStatus(retention.StatusActive) || validRetentionPolicyStatus("BROKEN") {
		t.Fatal("retention policy status validation failed")
	}
	if !validRetentionJobStatus(retention.JobHeldReview) || validRetentionJobStatus("BROKEN") {
		t.Fatal("retention job status validation failed")
	}
	if !validAlertPolicyStatus(operations.AlertPolicyActive) || validAlertPolicyStatus("BROKEN") {
		t.Fatal("alert policy status validation failed")
	}
	if !validOperationalAlertStatus(operations.AlertResolved) || validOperationalAlertStatus("BROKEN") {
		t.Fatal("alert status validation failed")
	}
	if !validOperationalSeverity(operations.SeverityCritical) || validOperationalSeverity("BROKEN") {
		t.Fatal("severity validation failed")
	}
	if !validNotificationStatus("DELIVERED") || validNotificationStatus("BROKEN") {
		t.Fatal("notification status validation failed")
	}
}
