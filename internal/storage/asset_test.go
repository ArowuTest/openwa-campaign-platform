package storage

import (
	"campaign-platform/internal/security/malware"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type assetScanner struct {
	result malware.Result
	err    error
}

func (s assetScanner) Scan(context.Context, io.Reader) (malware.Result, error) {
	return s.result, s.err
}

func TestTrustedAssetDerivesEvidenceAndVerifiesBytes(t *testing.T) {
	root := t.TempDir()
	objects, err := NewFileSystemStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	svc := &TrustedAssetService{Objects: objects, Repository: NewMemoryAssetRepository(), Scanner: assetScanner{result: malware.Result{Clean: true}}, Clock: func() time.Time { return now }, MaximumBytes: 1024}
	a, err := svc.Upload(context.Background(), AssetPurposeConsentEvidence, "note.txt", "text/plain", "actor", strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != AssetStatusClean || a.Size != 5 || len(a.SHA256) != 64 {
		t.Fatalf("unexpected asset %+v", a)
	}
	if _, err := svc.ResolveClean(context.Background(), a.ID, AssetPurposeConsentEvidence, 10); err != nil {
		t.Fatal(err)
	}
}

func TestTrustedAssetRejectsInfected(t *testing.T) {
	objects, _ := NewFileSystemStore(t.TempDir())
	svc := &TrustedAssetService{Objects: objects, Repository: NewMemoryAssetRepository(), Scanner: assetScanner{result: malware.Result{Infected: true, Signature: "EICAR"}}, MaximumBytes: 1024}
	a, err := svc.Upload(context.Background(), AssetPurposeConsentEvidence, "evidence.txt", "text/plain", "actor", strings.NewReader("bad"))
	if !errors.Is(err, ErrAssetNotClean) || a.Status != AssetStatusInfected {
		t.Fatalf("asset=%+v err=%v", a, err)
	}
}

func TestTrustedAssetRevocationRequiresGovernedEvidence(t *testing.T) {
	objects, err := NewFileSystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := &TrustedAssetService{Objects: objects, Repository: NewMemoryAssetRepository(), Scanner: assetScanner{result: malware.Result{Clean: true}}, MaximumBytes: 1024}
	asset, err := svc.Upload(context.Background(), AssetPurposeConsentEvidence, "evidence.txt", "text/plain", "actor", strings.NewReader("evidence"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Revoke(context.Background(), asset.ID, asset.Version, "admin", "short"); err == nil {
		t.Fatal("expected meaningful revocation reason")
	}
	asset, err = svc.Revoke(context.Background(), asset.ID, asset.Version, "admin", "source evidence was withdrawn")
	if err != nil {
		t.Fatal(err)
	}
	if asset.Status != AssetStatusRevoked {
		t.Fatalf("status=%s", asset.Status)
	}
	if _, err = svc.ResolveClean(context.Background(), asset.ID, AssetPurposeConsentEvidence, 1024); !errors.Is(err, ErrAssetNotClean) {
		t.Fatalf("expected revoked asset rejection, got %v", err)
	}
}

func TestMessageMediaRequiresGovernedRightsEvidenceAndExpiresFailClosed(t *testing.T) {
	objects, err := NewFileSystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 9, 20, 0, 0, 0, time.UTC)
	svc := &TrustedAssetService{
		Objects: objects, Repository: NewMemoryAssetRepository(),
		Scanner: assetScanner{result: malware.Result{Clean: true}},
		Clock:   func() time.Time { return now }, MaximumBytes: 1024,
	}
	if _, err := svc.Upload(context.Background(), AssetPurposeMessageMedia, "creative.txt", "text/plain", "actor", strings.NewReader("hello")); !errors.Is(err, ErrAssetRightsInvalid) {
		t.Fatalf("message media without rights evidence err=%v, want ErrAssetRightsInvalid", err)
	}

	expiry := now.Add(24 * time.Hour)
	asset, err := svc.UploadWithRights(context.Background(), AssetPurposeMessageMedia, "creative.txt", "text/plain", "actor",
		RightsEvidence{Basis: RightsBasisLicensed, Reference: "licence-case/2026-001", ExpiresAt: &expiry},
		strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if asset.RightsBasis != RightsBasisLicensed || asset.RightsReference != "licence-case/2026-001" || asset.RightsExpiresAt == nil {
		t.Fatalf("rights evidence not frozen on asset: %+v", asset)
	}
	if _, err := svc.ResolveClean(context.Background(), asset.ID, AssetPurposeMessageMedia, 1024); err != nil {
		t.Fatalf("valid rights evidence rejected: %v", err)
	}

	now = expiry.Add(time.Second)
	if _, err := svc.ResolveClean(context.Background(), asset.ID, AssetPurposeMessageMedia, 1024); !errors.Is(err, ErrAssetRightsInvalid) {
		t.Fatalf("expired rights err=%v, want ErrAssetRightsInvalid", err)
	}
}

func TestMessageMediaRightsEvidenceRejectsUnsupportedBasisAndUnsafeReference(t *testing.T) {
	now := time.Date(2026, 10, 9, 20, 0, 0, 0, time.UTC)
	for name, evidence := range map[string]RightsEvidence{
		"basis":     {Basis: RightsBasis("UNKNOWN"), Reference: "rights/case"},
		"reference": {Basis: RightsBasisOwned, Reference: "x\ny"},
		"expiry":    {Basis: RightsBasisClientSupplied, Reference: "client-contract", ExpiresAt: func() *time.Time { v := now; return &v }()},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateRightsEvidence(AssetPurposeMessageMedia, evidence, now); !errors.Is(err, ErrAssetRightsInvalid) {
				t.Fatalf("err=%v, want ErrAssetRightsInvalid", err)
			}
		})
	}
	if err := validateRightsEvidence(AssetPurposeConsentEvidence, RightsEvidence{}, now); err != nil {
		t.Fatalf("non-message evidence unexpectedly requires media rights: %v", err)
	}
}
