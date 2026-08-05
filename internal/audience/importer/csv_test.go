package importer

import (
	"bytes"
	"context"
	"encoding/hex"
	"strings"
	"testing"

	sharedcrypto "campaign-platform/internal/shared/crypto"
)

func TestPreviewCSVValidatesAndDeduplicates(t *testing.T) {
	data := `phone,country,state,lga,age,gender
08012345678,NG,Lagos,Ikeja,27,Female
+2348012345678,NG,Lagos,Ikeja,27,Female
08022222222,NG,Lagos,Surulere,abc,Male
08033333333,NG,Lagos,Eti-Osa,34,Male
`
	result, err := PreviewCSV(strings.NewReader(data), PreviewOptions{
		DefaultCountryISO2: "NG",
		Mapping:            ColumnMapping{MSISDN: "phone", Country: "country", State: "state", LGA: "lga", Age: "age", Gender: "gender"},
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if result.UploadedRows != 4 || result.ValidRows != 2 || result.DuplicateRows != 1 || result.InvalidRows != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Candidates[0].MaskedMSISDN == result.Candidates[0].E164 {
		t.Fatal("MSISDN was not masked")
	}
}

func TestPreviewCapsCandidateSamplesAndIssues(t *testing.T) {
	data := "msisdn,country,age\n08012345678,NG,27\n08022222222,NG,28\n08033333333,NG,29\ninvalid,NG,30\n"
	result, err := PreviewCSV(strings.NewReader(data), PreviewOptions{
		DefaultCountryISO2: "NG", MaxCandidateSample: 2, MaxIssues: 1,
		Mapping: ColumnMapping{MSISDN: "msisdn", Country: "country", Age: "age"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 2 {
		t.Fatalf("expected two samples, got %d", len(result.Candidates))
	}
	if len(result.Issues) != 1 {
		t.Fatalf("expected one retained issue, got %d", len(result.Issues))
	}
	if result.ValidRows != 3 || result.InvalidRows != 1 {
		t.Fatalf("aggregate counts are wrong: %+v", result)
	}
}

func TestProcessCSVRequiresProtectionAndDoesNotExposeRawMSISDN(t *testing.T) {
	data := "msisdn,country,age\n08012345678,NG,27\n"
	if _, err := ProcessCSV(context.Background(), strings.NewReader(data), PreviewOptions{Mapping: ColumnMapping{MSISDN: "msisdn"}}, func(context.Context, ContactCandidate) (bool, error) { return false, nil }); err == nil {
		t.Fatal("expected protector requirement")
	}
	protector, err := sharedcrypto.NewMSISDNProtector(bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var received ContactCandidate
	result, err := ProcessCSV(context.Background(), strings.NewReader(data), PreviewOptions{DefaultCountryISO2: "NG", Protector: protector, Mapping: ColumnMapping{MSISDN: "msisdn", Country: "country", Age: "age"}}, func(_ context.Context, candidate ContactCandidate) (bool, error) {
		received = candidate
		return false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ValidRows != 1 || received.E164 != "" || len(received.EncryptedMSISDN) == 0 || len(received.LookupHMAC) == 0 {
		t.Fatalf("unsafe processed candidate: result=%+v candidate=%+v", result, received)
	}
}

func TestProcessCSVDelegatesExactDeduplicationToConsumer(t *testing.T) {
	data := "msisdn,country\n08012345678,NG\n+2348012345678,NG\n"
	protector, _ := sharedcrypto.NewMSISDNProtector(bytes.Repeat([]byte{3}, 32), bytes.Repeat([]byte{4}, 32))
	seen := map[string]struct{}{}
	result, err := ProcessCSV(context.Background(), strings.NewReader(data), PreviewOptions{DefaultCountryISO2: "NG", Protector: protector, Mapping: ColumnMapping{MSISDN: "msisdn", Country: "country"}}, func(_ context.Context, candidate ContactCandidate) (bool, error) {
		key := hex.EncodeToString(candidate.LookupHMAC)
		if _, ok := seen[key]; ok {
			return true, nil
		}
		seen[key] = struct{}{}
		return false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ValidRows != 1 || result.DuplicateRows != 1 {
		t.Fatalf("unexpected counts: %+v", result)
	}
}

func TestProcessCSVLeavesAgeDateEmptyWhenAgeIsNotProvided(t *testing.T) {
	data := "msisdn,country,age\n08012345678,NG,\n"
	protector := importProtectorForCSV(t)
	var received ContactCandidate
	_, err := ProcessCSV(context.Background(), strings.NewReader(data), PreviewOptions{
		DefaultCountryISO2: "NG", Protector: protector,
		Mapping: ColumnMapping{MSISDN: "msisdn", Country: "country", Age: "age"},
	}, func(_ context.Context, candidate ContactCandidate) (bool, error) {
		received = candidate
		return false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if received.ReportedAge != nil || !received.AgeRecordedAt.IsZero() {
		t.Fatalf("age observation invariant violated: age=%v date=%v", received.ReportedAge, received.AgeRecordedAt)
	}
}

func importProtectorForCSV(t *testing.T) *sharedcrypto.MSISDNProtector {
	t.Helper()
	protector, err := sharedcrypto.NewMSISDNProtector(bytes.Repeat([]byte{9}, 32), bytes.Repeat([]byte{10}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return protector
}
