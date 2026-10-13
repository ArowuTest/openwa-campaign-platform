package operations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
	"campaign-platform/internal/privacy"
	sharedcrypto "campaign-platform/internal/shared/crypto"
)

type frozenPayloadInsertCapture struct {
	statement string
	args      []any
}

func (c *frozenPayloadInsertCapture) ExecContext(_ context.Context, statement string, args ...any) (sql.Result, error) {
	c.statement, c.args = statement, args
	return nil, nil
}

// splitExportInsertValues keeps nested NULLIF argument commas separate from the
// INSERT column separators, so this contract test can follow the actual binding
// regardless of the column's ordinal.
func splitExportInsertValues(values string) []string {
	var result []string
	start, depth := 0, 0
	for i, ch := range values {
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				result = append(result, strings.TrimSpace(values[start:i]))
				start = i + 1
			}
		}
	}
	return append(result, strings.TrimSpace(values[start:]))
}

func TestInsertExportBindsFrozenPayload(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload json.RawMessage
	}{
		{name: "encrypted privacy envelope", payload: json.RawMessage(`{"caseId":"case-1","ciphertext":"encrypted-evidence","keyVersion":"v1"}`)},
		{name: "no frozen payload"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capture := &frozenPayloadInsertCapture{}
			if err := insertExport(context.Background(), capture, ExportRequest{Kind: "PRIVACY_PACKAGE", FrozenPayload: tc.payload}); err != nil {
				t.Fatal(err)
			}
			columnStart := strings.Index(capture.statement, "export_requests(")
			valuesStart := strings.Index(capture.statement, ") VALUES(")
			if columnStart < 0 || valuesStart < 0 {
				t.Fatalf("unexpected export INSERT shape: %s", capture.statement)
			}
			columns := strings.Split(capture.statement[columnStart+len("export_requests("):valuesStart], ",")
			values := splitExportInsertValues(strings.TrimSuffix(strings.TrimSpace(capture.statement[valuesStart+len(") VALUES("):]), ")"))
			column := -1
			for i, value := range columns {
				if strings.TrimSpace(value) == "frozen_payload" {
					column = i
					break
				}
			}
			if column < 0 {
				t.Fatal("export INSERT drops frozen_payload before independent approval")
			}
			if column >= len(values) || !strings.Contains(values[column], "::jsonb") {
				t.Fatal("frozen_payload is not bound as JSONB")
			}
			binding := regexp.MustCompile(`\$(\d+)`).FindStringSubmatch(values[column])
			if len(binding) != 2 {
				t.Fatal("frozen_payload does not use a parameter binding")
			}
			ordinal, err := strconv.Atoi(binding[1])
			if err != nil || ordinal < 1 || ordinal > len(capture.args) {
				t.Fatalf("invalid frozen payload binding: %s", values[column])
			}
			got := capture.args[ordinal-1]
			if len(tc.payload) == 0 {
				if got != nil {
					t.Fatalf("missing frozen payload must bind SQL NULL, got %#v", got)
				}
			} else if got != string(tc.payload) {
				t.Fatalf("encrypted payload binding changed: got %#v", got)
			}
		})
	}
}

func TestPostgreSQLPrivacyExportCreatePreservesEncryptedPayloadThroughApproval(t *testing.T) {
	dsn := os.Getenv("POSTGRES_PAGINATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_PAGINATION_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var maker, checker, caseID string
	if err := db.QueryRowContext(ctx, "SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text").Scan(&maker, &checker, &caseID); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{maker, checker} {
		if _, err := db.ExecContext(ctx, "INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Privacy export persistence','DISABLED',false)", actor, "privacy-persistence-"+actor+"@internal.invalid"); err != nil {
			t.Fatal(err)
		}
	}
	ring, err := sharedcrypto.NewSecretKeyring("v1", map[string][]byte{"v1": make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	plaintext := `{"subject":{"maskedMsisdn":"+234***1234"},"records":[{"type":"consent"}]}`
	ciphertext, keyVersion, err := ring.Seal("privacy-case:"+caseID, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	checksum := sha256.Sum256([]byte(plaintext))
	envelope, err := json.Marshal(privacy.EncryptedPackageEnvelope{CaseID: caseID, Ciphertext: ciphertext, KeyVersion: keyVersion, PlaintextSHA256: hex.EncodeToString(checksum[:])})
	if err != nil {
		t.Fatal(err)
	}
	repository := &PostgreSQLRepository{DB: db}
	now := time.Now().UTC().Truncate(time.Microsecond)
	request, err := (&Service{Repo: repository, Clock: func() time.Time { return now }}).RequestExportWithOptions(ctx, "PRIVACY_PACKAGE", caseID, "JSON", "controlled privacy evidence", maker, "privacy-create", ExportOptions{FrozenPayload: envelope, TemplateVersion: "PRIVACY-PACKAGE-V1"})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := repository.GetExport(ctx, request.ID)
	if err != nil {
		t.Fatal(err)
	}
	var loaded privacy.EncryptedPackageEnvelope
	if err := json.Unmarshal(persisted.FrozenPayload, &loaded); err != nil {
		t.Fatalf("encrypted payload was lost at create: %v", err)
	}
	if loaded.CaseID != caseID || loaded.KeyVersion != keyVersion || !bytes.Equal(loaded.Ciphertext, ciphertext) || loaded.PlaintextSHA256 != hex.EncodeToString(checksum[:]) {
		t.Fatalf("encrypted envelope changed at create: %+v", loaded)
	}
	// A new service and repository must approve from durable database evidence,
	// rather than retaining the caller's in-memory RequestExport return value.
	checkerRepository := &PostgreSQLRepository{DB: db}
	approved, err := (&Service{Repo: checkerRepository, Clock: func() time.Time { return now.Add(time.Minute) }}).DecideExport(ctx, request.ID, request.Version, true, "", checker, "privacy-approval")
	if err != nil {
		t.Fatal(err)
	}
	approved, err = checkerRepository.GetExport(ctx, approved.ID)
	if err != nil {
		t.Fatal(err)
	}
	resolver := &privacy.Service{Evidence: ring}
	resolved, err := resolver.ResolvePrivacyPackage(approved.FrozenPayload)
	if err != nil || string(resolved) != plaintext || approved.Status != ExportApproved {
		t.Fatalf("approved durable envelope failed integrity/decryption: status=%s payload=%s err=%v", approved.Status, resolved, err)
	}
	rendered, contentType, err := (&ExportWorker{PrivacyPackages: resolver}).render(ctx, approved)
	if err != nil || contentType != "application/json" {
		t.Fatalf("approved privacy package did not render: contentType=%q err=%v", contentType, err)
	}
	var expected, actual any
	if err := json.Unmarshal([]byte(plaintext), &expected); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rendered, &actual); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("rendered privacy package changed: %s", rendered)
	}
}
