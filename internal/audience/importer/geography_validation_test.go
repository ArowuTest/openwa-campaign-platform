package importer

import (
	"strings"
	"testing"

	"campaign-platform/internal/geography"
)

func TestPreviewDistinguishesMissingOptionalGeographyFromInvalidRelationship(t *testing.T) {
	data := "msisdn,country,state,lga\n08012345678,NG,Lagos,Ikeja\n08022222222,NG,Kano,Ikeja\n08033333333,NG,,\n"
	catalogue := geography.DefaultCatalogue()
	result, err := PreviewCSV(strings.NewReader(data), PreviewOptions{
		DefaultCountryISO2: "NG",
		Mapping:            ColumnMapping{MSISDN: "msisdn", Country: "country", State: "state", LGA: "lga"},
		GeographyValidator: catalogue.Validate,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.UploadedRows != 3 || result.ValidRows != 2 || result.InvalidRows != 1 {
		t.Fatalf("unexpected geography result: %+v", result)
	}
	if len(result.Issues) != 1 || result.Issues[0].Code != "INVALID_GEOGRAPHY" {
		t.Fatalf("invalid state/LGA was not quarantined distinctly: %+v", result.Issues)
	}
	if len(result.Candidates) != 2 || result.Candidates[1].State != "" || result.Candidates[1].LGA != "" {
		t.Fatalf("missing optional geography was invented or dropped: %+v", result.Candidates)
	}
}
