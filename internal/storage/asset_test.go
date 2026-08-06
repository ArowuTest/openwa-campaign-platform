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
	a, err := svc.Upload(context.Background(), AssetPurposeMessageMedia, "note.txt", "text/plain", "actor", strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != AssetStatusClean || a.Size != 5 || len(a.SHA256) != 64 {
		t.Fatalf("unexpected asset %+v", a)
	}
	if _, err := svc.ResolveClean(context.Background(), a.ID, AssetPurposeMessageMedia, 10); err != nil {
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
