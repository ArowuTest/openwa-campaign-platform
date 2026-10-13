package message

import (
	"context"
	"strings"
	"testing"
)

type mediaTypeAssetResolver struct {
	asset TrustedAsset
}

func (r mediaTypeAssetResolver) ResolveMessageAsset(context.Context, string) (TrustedAsset, error) {
	return r.asset, nil
}

func TestCreateDraftRejectsTrustedAssetWhoseDetectedMediaTypeDoesNotMatchMessageType(t *testing.T) {
	cases := []struct {
		name      string
		message   Type
		mediaType string
		wantError string
	}{
		{"image rejects text", TypeImageCaption, "text/plain", "require image media"},
		{"video rejects image", TypeVideo, "image/png", "require video media"},
		{"document rejects unknown binary", TypeDocument, "application/octet-stream", "detected media type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := NewService(NewMemoryRepository())
			service.Assets = mediaTypeAssetResolver{asset: TrustedAsset{
				ID: "asset-1", ObjectKey: "private/asset-1", SHA256: strings.Repeat("a", 64),
				MediaType: tc.mediaType, Size: 100,
			}}
			_, err := service.CreateDraft(context.Background(), Input{
				CampaignID: "campaign-1", Version: 1, Type: tc.message, Body: "body",
				MediaAssetID: "asset-1", CreatedBy: "maker",
				IdempotencyKey: "message-media-type-test-0001",
			})
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("err=%v want substring %q", err, tc.wantError)
			}
		})
	}
}

func TestCreateDraftAcceptsTrustedAssetWithMatchingDetectedMediaType(t *testing.T) {
	for _, tc := range []struct {
		message   Type
		mediaType string
	}{
		{TypeImageCaption, "image/png"},
		{TypeVideo, "video/mp4"},
		{TypeDocument, "application/pdf"},
	} {
		service := NewService(NewMemoryRepository())
		service.Assets = mediaTypeAssetResolver{asset: TrustedAsset{
			ID: "asset-1", ObjectKey: "private/asset-1", SHA256: strings.Repeat("b", 64),
			MediaType: tc.mediaType, Size: 100,
		}}
		created, err := service.CreateDraft(context.Background(), Input{
			CampaignID: "campaign-1", Version: 1, Type: tc.message, Body: "body",
			MediaAssetID: "asset-1", CreatedBy: "maker",
			IdempotencyKey: "message-media-type-test-0002",
		})
		if err != nil {
			t.Fatalf("type=%s media=%s err=%v", tc.message, tc.mediaType, err)
		}
		if created.Media == nil || created.Media.MediaType != tc.mediaType {
			t.Fatalf("unexpected media evidence: %+v", created.Media)
		}
	}
}
