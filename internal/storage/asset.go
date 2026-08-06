package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"campaign-platform/internal/security/malware"
	"campaign-platform/internal/shared/id"
)

type AssetPurpose string

const (
	AssetPurposeMessageMedia     AssetPurpose = "MESSAGE_MEDIA"
	AssetPurposeConsentEvidence  AssetPurpose = "CONSENT_EVIDENCE"
	AssetPurposeCampaignEvidence AssetPurpose = "CAMPAIGN_EVIDENCE"
)

type AssetStatus string

const (
	AssetStatusScanning   AssetStatus = "SCANNING"
	AssetStatusClean      AssetStatus = "CLEAN"
	AssetStatusInfected   AssetStatus = "INFECTED"
	AssetStatusScanFailed AssetStatus = "SCAN_FAILED"
	AssetStatusRevoked    AssetStatus = "REVOKED"
)

type Asset struct {
	ID               string       `json:"id"`
	Purpose          AssetPurpose `json:"purpose"`
	ObjectKey        string       `json:"objectKey"`
	OriginalFilename string       `json:"originalFilename"`
	MediaType        string       `json:"mediaType"`
	Size             int64        `json:"size"`
	SHA256           string       `json:"sha256"`
	Status           AssetStatus  `json:"status"`
	ScanSignature    string       `json:"scanSignature,omitempty"`
	CreatedBy        string       `json:"createdBy"`
	CreatedAt        time.Time    `json:"createdAt"`
	UpdatedAt        time.Time    `json:"updatedAt"`
	Version          int64        `json:"version"`
}

var (
	ErrAssetNotFound = errors.New("trusted asset not found")
	ErrAssetConflict = errors.New("trusted asset version conflict")
	ErrAssetNotClean = errors.New("trusted asset has not passed malware scanning")
)

type AssetRepository interface {
	Create(context.Context, Asset) (Asset, error)
	Get(context.Context, string) (Asset, error)
	UpdateScan(context.Context, string, int64, AssetStatus, string, time.Time) (Asset, error)
	Revoke(context.Context, string, int64, string, string, time.Time) (Asset, error)
}
type TrustedAssetService struct {
	Objects      ObjectStore
	Repository   AssetRepository
	Scanner      malware.Scanner
	Clock        func() time.Time
	MaximumBytes int64
}

func (s *TrustedAssetService) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}
func (s *TrustedAssetService) Upload(ctx context.Context, purpose AssetPurpose, filename, declaredType, actor string, source io.Reader) (Asset, error) {
	if s == nil || s.Objects == nil || s.Repository == nil || s.Scanner == nil {
		return Asset{}, errors.New("trusted asset intake dependencies are required")
	}
	if purpose != AssetPurposeMessageMedia && purpose != AssetPurposeConsentEvidence && purpose != AssetPurposeCampaignEvidence {
		return Asset{}, errors.New("unsupported asset purpose")
	}
	filename = strings.TrimSpace(filepath.Base(filename))
	if filename == "" || filename == "." {
		return Asset{}, errors.New("original filename is required")
	}
	if strings.TrimSpace(actor) == "" || source == nil {
		return Asset{}, errors.New("asset actor and content are required")
	}
	maximum := s.MaximumBytes
	if maximum <= 0 || maximum > 2<<30 {
		maximum = 128 << 20
	}
	prefix := make([]byte, 512)
	n, err := io.ReadFull(source, prefix)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return Asset{}, err
	}
	prefix = prefix[:n]
	if len(prefix) == 0 {
		return Asset{}, errors.New("empty assets are not permitted")
	}
	detected := http.DetectContentType(prefix)
	declaredType = strings.TrimSpace(strings.Split(declaredType, ";")[0])
	if declaredType != "" && declaredType != "application/octet-stream" && !compatibleContentType(declaredType, detected) {
		return Asset{}, fmt.Errorf("declared media type %s does not match detected type %s", declaredType, detected)
	}
	identifier, err := id.New()
	if err != nil {
		return Asset{}, err
	}
	key := fmt.Sprintf("trusted-assets/%s/%s/%s", strings.ToLower(string(purpose)), identifier, safeFilename(filename))
	meta, err := s.Objects.Put(ctx, key, io.MultiReader(bytes.NewReader(prefix), source), maximum)
	if err != nil {
		return Asset{}, err
	}
	now := s.now()
	asset := Asset{ID: identifier, Purpose: purpose, ObjectKey: meta.Key, OriginalFilename: filename, MediaType: detected, Size: meta.Size, SHA256: meta.SHA256, Status: AssetStatusScanning, CreatedBy: actor, CreatedAt: now, UpdatedAt: now, Version: 1}
	asset, err = s.Repository.Create(ctx, asset)
	if err != nil {
		if cleanupErr := s.Objects.Delete(ctx, key); cleanupErr != nil {
			return Asset{}, errors.Join(err, fmt.Errorf("remove unreferenced asset object: %w", cleanupErr))
		}
		return Asset{}, err
	}
	object, stored, err := s.Objects.Open(ctx, key)
	if err != nil {
		_, persistErr := s.Repository.UpdateScan(ctx, asset.ID, asset.Version, AssetStatusScanFailed, "OBJECT_OPEN_FAILED", s.now())
		return Asset{}, errors.Join(err, persistErr)
	}
	if stored.SHA256 != asset.SHA256 || stored.Size != asset.Size {
		closeErr := object.Close()
		_, persistErr := s.Repository.UpdateScan(ctx, asset.ID, asset.Version, AssetStatusScanFailed, "OBJECT_INTEGRITY_MISMATCH", s.now())
		return Asset{}, errors.Join(errors.New("stored asset integrity mismatch"), closeErr, persistErr)
	}
	result, scanErr := s.Scanner.Scan(ctx, object)
	closeErr := object.Close()
	if scanErr == nil && closeErr != nil {
		scanErr = closeErr
	}
	status := AssetStatusClean
	signature := ""
	if scanErr != nil {
		status = AssetStatusScanFailed
		signature = "SCAN_FAILED"
	} else if result.Infected {
		status = AssetStatusInfected
		signature = result.Signature
	} else if !result.Clean {
		status = AssetStatusScanFailed
		signature = "SCAN_INDETERMINATE"
	}
	asset, updateErr := s.Repository.UpdateScan(ctx, asset.ID, asset.Version, status, signature, s.now())
	if updateErr != nil {
		return Asset{}, updateErr
	}
	if scanErr != nil {
		return asset, fmt.Errorf("scan trusted asset: %w", scanErr)
	}
	if status != AssetStatusClean {
		return asset, ErrAssetNotClean
	}
	return asset, nil
}
func (s *TrustedAssetService) ResolveClean(ctx context.Context, id string, purpose AssetPurpose, maximum int64) (Asset, error) {
	if s == nil || s.Repository == nil || s.Objects == nil {
		return Asset{}, errors.New("trusted asset service is required")
	}
	asset, err := s.Repository.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return Asset{}, err
	}
	if asset.Purpose != purpose {
		return Asset{}, errors.New("trusted asset purpose does not match")
	}
	if asset.Status != AssetStatusClean {
		return Asset{}, ErrAssetNotClean
	}
	if maximum > 0 && asset.Size > maximum {
		return Asset{}, errors.New("trusted asset exceeds route limit")
	}
	meta, err := s.Objects.Stat(ctx, asset.ObjectKey)
	if err != nil {
		return Asset{}, err
	}
	if meta.Size != asset.Size || meta.SHA256 != asset.SHA256 {
		return Asset{}, errors.New("trusted asset bytes no longer match approved evidence")
	}
	return asset, nil
}

func (s *TrustedAssetService) Revoke(ctx context.Context, id string, expected int64, actor, reason string) (Asset, error) {
	if s == nil || s.Repository == nil {
		return Asset{}, errors.New("trusted asset service is required")
	}
	if strings.TrimSpace(id) == "" || expected <= 0 || strings.TrimSpace(actor) == "" || len(strings.TrimSpace(reason)) < 8 {
		return Asset{}, errors.New("asset id, expected version, actor and a meaningful reason are required")
	}
	return s.Repository.Revoke(ctx, id, expected, strings.TrimSpace(actor), strings.TrimSpace(reason), s.now())
}

func compatibleContentType(declared, detected string) bool {
	declared = strings.ToLower(declared)
	detected = strings.ToLower(detected)
	if declared == detected {
		return true
	}
	if strings.HasPrefix(declared, "text/") && strings.HasPrefix(detected, "text/") {
		return true
	}
	return false
}
func safeFilename(value string) string {
	value = filepath.Base(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._-", r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "._")
	if out == "" {
		out = "asset"
	}
	if len(out) > 120 {
		out = out[:120]
	}
	return out
}
