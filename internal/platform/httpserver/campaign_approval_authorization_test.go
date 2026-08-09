package httpserver

import (
	"testing"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/identity"
)

func TestCampaignFinalApprovalRequiresCampaignApprovePermission(t *testing.T) {
	writer := identity.User{Permissions: map[string]struct{}{"campaign.write": {}}}
	if authorisedForCampaignAction(writer, campaign.ActionApproveFinal) {
		t.Fatal("campaign writer without campaign.approve was authorised for final approval")
	}
	approver := identity.User{Permissions: map[string]struct{}{"campaign.write": {}, "campaign.approve": {}}}
	if !authorisedForCampaignAction(approver, campaign.ActionApproveFinal) {
		t.Fatal("campaign approver was denied final approval")
	}
	for _, action := range []campaign.Action{campaign.ActionApproveConsent, campaign.ActionApproveMessage, campaign.ActionApproveCommercial} {
		if !authorisedForCampaignAction(approver, action) {
			t.Fatalf("campaign approver denied action %s", action)
		}
	}
}
