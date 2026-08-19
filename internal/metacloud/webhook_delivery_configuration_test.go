package metacloud

import (
	"context"
	"errors"
	"testing"

	"campaign-platform/internal/delivery"
)

func TestPostgreSQLWebhookDeliveryResolverDoesNotCollapseMisconfigurationToNotFound(t *testing.T) {
	tests := []struct {
		name    string
		resolve func() error
	}{
		{name: "status resolver missing database", resolve: func() error {
			_, err := (&PostgreSQLWebhookDeliveryResolver{}).ResolveMetaWebhookRecipient(context.Background(), Sender{ID: "sender", OrganisationID: "org"}, "wamid.1")
			return err
		}},
		{name: "status resolver missing organisation authority", resolve: func() error {
			_, err := (&PostgreSQLWebhookDeliveryResolver{}).ResolveMetaWebhookRecipient(context.Background(), Sender{ID: "sender"}, "wamid.1")
			return err
		}},
		{name: "inbound resolver missing protector", resolve: func() error {
			_, err := (&PostgreSQLWebhookDeliveryResolver{}).ResolveMetaWebhookInboundRecipient(context.Background(), Sender{ID: "sender", OrganisationID: "org"}, "+2348012345678")
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.resolve()
			if err == nil || errors.Is(err, delivery.ErrRecipientNotFound) {
				t.Fatalf("resolver misconfiguration collapsed to recipient-not-found: %v", err)
			}
		})
	}
}
