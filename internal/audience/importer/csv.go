package importer

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	sharedcrypto "campaign-platform/internal/shared/crypto"
)

const (
	DefaultMaxRows = 2_000_000
	MaxFieldLength = 4_096
)

type ColumnMapping struct {
	Worksheet string `json:"worksheet,omitempty"`
	MSISDN    string `json:"msisdn"`
	Country   string `json:"country"`
	State     string `json:"state"`
	LGA       string `json:"lga"`
	Age       string `json:"age"`
	Gender    string `json:"gender"`
}

type GeographyValidator func(countryISO2, state, lga string) error

type PreviewOptions struct {
	DefaultCountryISO2 string
	MaxRows            int
	Mapping            ColumnMapping
	Protector          *sharedcrypto.MSISDNProtector
	GeographyValidator GeographyValidator
	MaxIssues          int
	MaxCandidateSample int
}

type ContactCandidate struct {
	RowNumber         int
	E164              string
	EncryptedMSISDN   []byte
	LookupHMAC        []byte
	MaskedMSISDN      string
	Country           string
	State             string
	LGA               string
	ReportedAge       *int
	AgeRecordedAt     time.Time
	ProfileRecordedAt time.Time
	Gender            string
	SourceHash        string
}

type RowIssue struct {
	RowNumber int    `json:"rowNumber"`
	Field     string `json:"field"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

type CandidateSample struct {
	RowNumber    int    `json:"rowNumber"`
	MaskedMSISDN string `json:"maskedMsisdn"`
	Country      string `json:"country"`
	State        string `json:"state,omitempty"`
	LGA          string `json:"lga,omitempty"`
	ReportedAge  *int   `json:"reportedAge,omitempty"`
	Gender       string `json:"gender"`
	Protected    bool   `json:"protected"`
}

type PreviewResult struct {
	UploadedRows  int                `json:"uploadedRows"`
	ValidRows     int                `json:"validRows"`
	InvalidRows   int                `json:"invalidRows"`
	DuplicateRows int                `json:"duplicateRows"`
	Candidates    []ContactCandidate `json:"-"`
	Samples       []CandidateSample  `json:"samples"`
	Issues        []RowIssue         `json:"issues"`
}

// CandidateConsumer receives one syntactically and semantically valid protected
// candidate. It returns true when the candidate is an exact duplicate already
// staged for the import. Production consumers should use a durable uniqueness
// constraint on (import_id, lookup_hmac), rather than retaining millions of
// telephone numbers in application memory.
type CandidateConsumer func(context.Context, ContactCandidate) (duplicate bool, err error)

// PreviewCSV validates a bounded sample and deduplicates within the uploaded
// file. It never returns raw MSISDN values in JSON because Candidates is excluded.
func PreviewCSV(reader io.Reader, options PreviewOptions) (PreviewResult, error) {
	return scanCSV(context.Background(), reader, options, true, true, nil)
}

// ProcessCSV streams every valid row to a protected consumer. A protector is
// mandatory and the raw E.164 value is cleared before the candidate crosses the
// parser boundary. Exact deduplication is delegated to the durable consumer.
func ProcessCSV(ctx context.Context, reader io.Reader, options PreviewOptions, consumer CandidateConsumer) (PreviewResult, error) {
	if options.Protector == nil {
		return PreviewResult{}, errors.New("MSISDN protector is required for import processing")
	}
	if consumer == nil {
		return PreviewResult{}, errors.New("candidate consumer is required")
	}
	return scanCSV(ctx, reader, options, false, false, consumer)
}

func scanCSV(ctx context.Context, reader io.Reader, options PreviewOptions, deduplicateInMemory, collectSamples bool, consumer CandidateConsumer) (PreviewResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if options.MaxRows <= 0 {
		options.MaxRows = DefaultMaxRows
	}
	if options.MaxIssues <= 0 {
		options.MaxIssues = 1_000
	}
	if options.MaxCandidateSample <= 0 {
		options.MaxCandidateSample = 100
	}
	if options.Mapping.MSISDN == "" {
		options.Mapping.MSISDN = "msisdn"
	}
	csvReader := csv.NewReader(io.LimitReader(reader, 512<<20))
	csvReader.FieldsPerRecord = -1
	csvReader.ReuseRecord = true
	headers, err := csvReader.Read()
	if err != nil {
		return PreviewResult{}, fmt.Errorf("read CSV headers: %w", err)
	}
	index := headerIndex(headers)
	requiredIndex, ok := index[normaliseHeader(options.Mapping.MSISDN)]
	if !ok {
		return PreviewResult{}, fmt.Errorf("required MSISDN column %q was not found", options.Mapping.MSISDN)
	}

	result := PreviewResult{
		Issues:     make([]RowIssue, 0, min(options.MaxIssues, 100)),
		Candidates: make([]ContactCandidate, 0, min(options.MaxCandidateSample, 100)),
		Samples:    make([]CandidateSample, 0, min(options.MaxCandidateSample, 100)),
	}
	ageRecordedAt := time.Now().UTC()
	var seen map[string]struct{}
	if deduplicateInMemory {
		seen = make(map[string]struct{})
	}
	for rowNumber := 2; ; rowNumber++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		record, readErr := csvReader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return result, fmt.Errorf("read CSV row %d: %w", rowNumber, readErr)
		}
		result.UploadedRows++
		if result.UploadedRows > options.MaxRows {
			return result, fmt.Errorf("import exceeds maximum permitted row count of %d", options.MaxRows)
		}
		if requiredIndex >= len(record) {
			result.InvalidRows++
			appendIssue(&result, options.MaxIssues, RowIssue{RowNumber: rowNumber, Field: options.Mapping.MSISDN, Code: "MISSING_VALUE", Message: "MSISDN value is missing"})
			continue
		}
		if oversized(record) {
			result.InvalidRows++
			appendIssue(&result, options.MaxIssues, RowIssue{RowNumber: rowNumber, Code: "FIELD_TOO_LONG", Message: "one or more fields exceed the maximum permitted length"})
			continue
		}

		country := valueFor(record, index, options.Mapping.Country)
		if country == "" {
			country = options.DefaultCountryISO2
		}
		country = NormalizeCountryISO2(country)
		e164, normaliseErr := NormalizeMSISDN(record[requiredIndex], country)
		if normaliseErr != nil {
			result.InvalidRows++
			appendIssue(&result, options.MaxIssues, RowIssue{RowNumber: rowNumber, Field: options.Mapping.MSISDN, Code: "INVALID_MSISDN", Message: normaliseErr.Error()})
			continue
		}
		if deduplicateInMemory {
			key := e164
			if options.Protector != nil {
				key = hex.EncodeToString(options.Protector.LookupHMAC(e164))
			}
			if _, duplicate := seen[key]; duplicate {
				result.DuplicateRows++
				continue
			}
			seen[key] = struct{}{}
		}

		age, ageErr := parseAge(valueFor(record, index, options.Mapping.Age))
		if ageErr != nil {
			result.InvalidRows++
			appendIssue(&result, options.MaxIssues, RowIssue{RowNumber: rowNumber, Field: options.Mapping.Age, Code: "INVALID_AGE", Message: ageErr.Error()})
			continue
		}
		state := strings.TrimSpace(valueFor(record, index, options.Mapping.State))
		lga := strings.TrimSpace(valueFor(record, index, options.Mapping.LGA))
		if options.GeographyValidator != nil {
			if geographyErr := options.GeographyValidator(country, state, lga); geographyErr != nil {
				result.InvalidRows++
				appendIssue(&result, options.MaxIssues, RowIssue{RowNumber: rowNumber, Field: "geography", Code: "INVALID_GEOGRAPHY", Message: geographyErr.Error()})
				continue
			}
		}
		candidate := ContactCandidate{
			RowNumber: rowNumber, E164: e164, MaskedMSISDN: sharedcrypto.Mask(e164),
			Country: strings.ToUpper(strings.TrimSpace(country)), State: state, LGA: lga,
			ReportedAge: age, ProfileRecordedAt: ageRecordedAt,
			Gender:     normaliseGender(valueFor(record, index, options.Mapping.Gender)),
			SourceHash: hashRecord(record),
		}
		// Age and its as-of date are a single observation. Keeping the date nil when
		// no age was supplied satisfies the persistence invariant and prevents a
		// future import from being treated as a newer age observation when it did
		// not contain age data at all.
		if age != nil {
			candidate.AgeRecordedAt = ageRecordedAt
		}
		if options.Protector != nil {
			encrypted, encryptErr := options.Protector.Encrypt(e164)
			if encryptErr != nil {
				return result, fmt.Errorf("protect row %d MSISDN: %w", rowNumber, encryptErr)
			}
			candidate.EncryptedMSISDN = encrypted
			candidate.LookupHMAC = options.Protector.LookupHMAC(e164)
		}
		if collectSamples && len(result.Candidates) < options.MaxCandidateSample {
			result.Candidates = append(result.Candidates, cloneCandidate(candidate))
			result.Samples = append(result.Samples, CandidateSample{
				RowNumber: candidate.RowNumber, MaskedMSISDN: candidate.MaskedMSISDN,
				Country: candidate.Country, State: candidate.State, LGA: candidate.LGA,
				ReportedAge: candidate.ReportedAge, Gender: candidate.Gender,
				Protected: len(candidate.EncryptedMSISDN) > 0 && len(candidate.LookupHMAC) > 0,
			})
		}
		if consumer != nil {
			protected := cloneCandidate(candidate)
			protected.E164 = ""
			duplicate, consumeErr := consumer(ctx, protected)
			if consumeErr != nil {
				return result, fmt.Errorf("consume CSV row %d: %w", rowNumber, consumeErr)
			}
			if duplicate {
				result.DuplicateRows++
				continue
			}
		}
		result.ValidRows++
	}
	return result, nil
}

func cloneCandidate(value ContactCandidate) ContactCandidate {
	value.EncryptedMSISDN = append([]byte(nil), value.EncryptedMSISDN...)
	value.LookupHMAC = append([]byte(nil), value.LookupHMAC...)
	if value.ReportedAge != nil {
		age := *value.ReportedAge
		value.ReportedAge = &age
	}
	return value
}

func headerIndex(headers []string) map[string]int {
	result := make(map[string]int, len(headers))
	for i, header := range headers {
		result[normaliseHeader(header)] = i
	}
	return result
}

func normaliseHeader(value string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "\ufeff")))
}

func valueFor(record []string, index map[string]int, configured string) string {
	if strings.TrimSpace(configured) == "" {
		return ""
	}
	position, ok := index[normaliseHeader(configured)]
	if !ok || position >= len(record) {
		return ""
	}
	return record[position]
}

func parseAge(raw string) (*int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	age, err := strconv.Atoi(raw)
	if err != nil {
		return nil, errors.New("age must be a whole number")
	}
	if age < 0 || age > 130 {
		return nil, errors.New("age must be between 0 and 130")
	}
	return &age, nil
}

func normaliseGender(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "female", "f", "woman":
		return "FEMALE"
	case "male", "m", "man":
		return "MALE"
	case "prefer not to say", "prefer_not_to_say":
		return "PREFER_NOT_TO_SAY"
	case "":
		return "NOT_STATED"
	default:
		return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(raw), " ", "_"))
	}
}

func hashRecord(record []string) string {
	sum := sha256.Sum256([]byte(strings.Join(record, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func oversized(record []string) bool {
	for _, field := range record {
		if len(field) > MaxFieldLength {
			return true
		}
	}
	return false
}

func appendIssue(result *PreviewResult, maximum int, issue RowIssue) {
	if len(result.Issues) < maximum {
		result.Issues = append(result.Issues, issue)
	}
}
