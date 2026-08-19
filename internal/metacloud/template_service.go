package metacloud

import (
	"context"
	"errors"
	"strings"
	"time"

	"campaign-platform/internal/message"
)

type TemplateLister interface {
	ListTemplates(context.Context, TemplateListRequest) ([]Template, error)
}

type MessageReader interface {
	Get(context.Context, string) (message.Version, error)
}

type TemplateService struct {
	Store    TemplateStore
	Client   TemplateLister
	Messages MessageReader
	Clock    func() time.Time
}

func (s *TemplateService) now() time.Time {
	if s != nil && s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func (s *TemplateService) SyncSenderTemplates(ctx context.Context, sender Sender) error {
	if s == nil || s.Store == nil || s.Client == nil {
		return errors.New("Meta template store and client are required")
	}
	if sender.Status != StatusActive || strings.TrimSpace(sender.OrganisationID) == "" || strings.TrimSpace(sender.WABAID) == "" {
		return ErrTemplateInvalid
	}
	items, err := s.Client.ListTemplates(ctx, TemplateListRequest{CredentialKey: sender.CredentialKey, GraphAPIVersion: sender.GraphAPIVersion, WABAID: sender.WABAID})
	if err != nil {
		return err
	}
	now := s.now()
	for i := range items {
		items[i].OrganisationID = sender.OrganisationID
		items[i].WABAID = sender.WABAID
		items[i].LastSyncedAt = now
		if items[i].ComponentHash == "" {
			items[i].ComponentHash, err = CanonicalComponentHash(items[i].Components)
			if err != nil {
				return err
			}
		}
	}
	return s.Store.ReplaceWABATemplates(ctx, sender.OrganisationID, sender.WABAID, items, now)
}

func (s *TemplateService) CreateBinding(ctx context.Context, organisationID, wabaID string, value Binding) (Binding, error) {
	if s == nil || s.Store == nil || s.Messages == nil {
		return Binding{}, errors.New("Meta template store and message reader are required")
	}
	version, err := s.Messages.Get(ctx, strings.TrimSpace(value.MessageVersionID))
	if err != nil {
		return Binding{}, err
	}
	template, err := s.Store.FindApproved(ctx, strings.TrimSpace(organisationID), strings.TrimSpace(wabaID), strings.TrimSpace(value.TemplateName), strings.TrimSpace(value.Language))
	if err != nil {
		return Binding{}, err
	}
	if value.CreatedAt.IsZero() {
		value.CreatedAt = s.now()
	}
	value.CreatedAt = value.CreatedAt.UTC()
	normalized, err := CanonicalizeBinding(version, template, value)
	if err != nil {
		return Binding{}, err
	}
	return s.Store.CreateBinding(ctx, normalized)
}
