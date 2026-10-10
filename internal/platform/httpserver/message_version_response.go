package httpserver

import (
	"time"

	"campaign-platform/internal/message"
)

type messageMediaResponse struct {
	AssetID    string `json:"assetId"`
	SHA256     string `json:"sha256"`
	MediaType  string `json:"mediaType"`
	Size       int64  `json:"size"`
	ScanStatus string `json:"scanStatus"`
}

type messageVersionResponse struct {
	ID          string                 `json:"id"`
	CampaignID  string                 `json:"campaignId"`
	Version     int                    `json:"version"`
	Type        message.Type           `json:"type"`
	Body        string                 `json:"body,omitempty"`
	Media       *messageMediaResponse  `json:"media,omitempty"`
	Links       []message.Link         `json:"links"`
	Variables   []message.Variable     `json:"variables"`
	ContentHash string                 `json:"contentHash"`
	Status      message.Status         `json:"status"`
	CreatedBy   string                 `json:"createdBy"`
	ApprovedBy  string                 `json:"approvedBy,omitempty"`
	CreatedAt   time.Time              `json:"createdAt"`
	ApprovedAt  *time.Time             `json:"approvedAt,omitempty"`
}

func publicMessageVersion(value message.Version) messageVersionResponse {
	response := messageVersionResponse{
		ID: value.ID, CampaignID: value.CampaignID, Version: value.Version, Type: value.Type,
		Body: value.Body, Links: append([]message.Link(nil), value.Links...),
		Variables: append([]message.Variable(nil), value.Variables...),
		ContentHash: value.ContentHash, Status: value.Status, CreatedBy: value.CreatedBy,
		ApprovedBy: value.ApprovedBy, CreatedAt: value.CreatedAt, ApprovedAt: value.ApprovedAt,
	}
	if value.Media != nil {
		response.Media = &messageMediaResponse{
			AssetID: value.Media.AssetID, SHA256: value.Media.SHA256, MediaType: value.Media.MediaType,
			Size: value.Media.Size, ScanStatus: value.Media.ScanStatus,
		}
	}
	return response
}

func publicMessageVersions(values []message.Version) []messageVersionResponse {
	out := make([]messageVersionResponse, 0, len(values))
	for _, value := range values {
		out = append(out, publicMessageVersion(value))
	}
	return out
}
