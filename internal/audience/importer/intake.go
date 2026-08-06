package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"campaign-platform/internal/security/malware"
	"campaign-platform/internal/storage"
)

type IntakeInput struct {
	OrganisationID      string
	ConsentReviewID     string
	PurposeID           string
	Channel             string
	WordingVersion      string
	SourceName          string
	SourceSystem        string
	DefaultCountryISO2  string
	OriginalFilename    string
	TemplateVersion     string
	MappingDefinitionID string
	Mapping             any
	SourceExpiresAt     *time.Time
	UpdatePolicy        UpdatePolicy
	UploadedBy          string
	ClientRequestID     string
}

type IntakeService struct {
	Store                  storage.ObjectStore
	Scanner                malware.Scanner
	Imports                *ImportService
	MaxFileSize            int64
	KeyPrefix              string
	DefaultSourceRetention time.Duration
}

// Intake streams an upload into private quarantine storage, validates its actual
// signature, creates immutable import evidence and records a terminal malware
// decision. Exact request replays return the original import; a different file
// cannot reuse the same request key or object key.
func (s *IntakeService) Intake(ctx context.Context, input IntakeInput, source io.Reader) (ImportBatch, bool, error) {
	if s == nil || s.Store == nil || s.Scanner == nil || s.Imports == nil {
		return ImportBatch{}, false, errors.New("object store, malware scanner and import service are required")
	}
	if source == nil {
		return ImportBatch{}, false, errors.New("upload source is required")
	}
	if err := ValidateIdempotencyKey(input.ClientRequestID); err != nil {
		return ImportBatch{}, false, err
	}
	filename, err := safeOriginalFilename(input.OriginalFilename)
	if err != nil {
		return ImportBatch{}, false, err
	}
	maximum := s.MaxFileSize
	if maximum <= 0 {
		maximum = 512 << 20
	}
	objectKey := s.objectKey(input.OrganisationID, input.ClientRequestID)
	metadata, err := s.Store.Put(ctx, objectKey, source, maximum)
	if err != nil {
		return ImportBatch{}, false, err
	}
	if input.SourceExpiresAt == nil {
		retention := s.DefaultSourceRetention
		if retention <= 0 {
			retention = 30 * 24 * time.Hour
		}
		expires := metadata.CreatedAt.UTC().Add(retention)
		input.SourceExpiresAt = &expires
	}
	object, _, err := s.Store.Open(ctx, objectKey)
	if err != nil {
		return ImportBatch{}, false, err
	}
	inspection, inspectionErr := InspectImportFile(filename, object, metadata.Size)
	_ = object.Close()
	mediaType := "application/octet-stream"
	signatureValid := inspectionErr == nil
	if signatureValid {
		mediaType = inspection.MediaType
	}
	batch, created, createErr := s.Imports.Create(ctx, CreateImportInput{
		OrganisationID: input.OrganisationID, ConsentReviewID: input.ConsentReviewID, PurposeID: input.PurposeID,
		Channel: input.Channel, WordingVersion: input.WordingVersion, SourceName: input.SourceName,
		SourceSystem: input.SourceSystem, DefaultCountryISO2: input.DefaultCountryISO2,
		ObjectKey: objectKey, OriginalFilename: filename, DetectedMediaType: mediaType,
		FileSHA256: metadata.SHA256, ByteSize: metadata.Size, TemplateVersion: input.TemplateVersion, MappingDefinitionID: input.MappingDefinitionID,
		Mapping: input.Mapping, SourceExpiresAt: input.SourceExpiresAt, UpdatePolicy: input.UpdatePolicy, UploadedBy: input.UploadedBy,
		ClientRequestID: input.ClientRequestID, ContentSignatureValid: signatureValid,
	})
	if createErr != nil {
		if errors.Is(createErr, ErrDuplicateImportFile) && batch.ObjectKey != objectKey {
			_ = s.Store.Delete(context.Background(), objectKey)
		}
		return batch, created, createErr
	}
	if !created && batch.Status != ImportUploaded && batch.Status != ImportScanning && batch.Status != ImportFailed {
		return batch, false, nil
	}

	object, _, err = s.Store.Open(ctx, objectKey)
	if err != nil {
		return batch, created, err
	}
	scanResult, scanErr := s.Scanner.Scan(ctx, object)
	_ = object.Close()
	if scanErr != nil {
		reason := truncate(scanErr.Error(), 1000)
		updated, recordErr := s.Imports.RecordScan(ctx, batch.ID, MalwareFailed, signatureValid, reason, batch.Version)
		if recordErr != nil {
			return batch, created, errors.Join(scanErr, recordErr)
		}
		return updated, created, scanErr
	}
	malwareStatus := MalwareClean
	reason := ""
	if scanResult.Infected {
		malwareStatus = MalwareInfected
		reason = scanResult.Signature
	}
	if inspectionErr != nil && reason == "" {
		reason = truncate(inspectionErr.Error(), 1000)
	}
	updated, err := s.Imports.RecordScan(ctx, batch.ID, malwareStatus, signatureValid, reason, batch.Version)
	if err != nil {
		return batch, created, err
	}
	return updated, created, nil
}

func (s *IntakeService) objectKey(organisationID, requestID string) string {
	prefix := strings.Trim(strings.TrimSpace(s.KeyPrefix), "/")
	if prefix == "" {
		prefix = "imports/quarantine"
	}
	digest := sha256.Sum256([]byte(strings.TrimSpace(organisationID) + "\x1f" + strings.TrimSpace(requestID)))
	encoded := hex.EncodeToString(digest[:])
	return fmt.Sprintf("%s/%s/%s.bin", prefix, encoded[:2], encoded)
}

func safeOriginalFilename(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	name := filepath.Base(value)
	if name == "" || name == "." || name == ".." || len(name) > 255 || strings.ContainsRune(name, '\x00') {
		return "", errors.New("valid original filename is required")
	}
	return name, nil
}
