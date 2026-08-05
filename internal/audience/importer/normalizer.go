package importer

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

type CountryDiallingRule struct {
	ISO2             string
	CallingCode      string
	NationalPrefixes []string
	MinNationalLen   int
	MaxNationalLen   int
}

var defaultDiallingRules = map[string]CountryDiallingRule{
	"NG": {ISO2: "NG", CallingCode: "234", NationalPrefixes: []string{"0"}, MinNationalLen: 10, MaxNationalLen: 10},
	"GH": {ISO2: "GH", CallingCode: "233", NationalPrefixes: []string{"0"}, MinNationalLen: 9, MaxNationalLen: 9},
	"GB": {ISO2: "GB", CallingCode: "44", NationalPrefixes: []string{"0"}, MinNationalLen: 10, MaxNationalLen: 10},
}

func NormalizeCountryISO2(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "NG", "NGA", "NIGERIA":
		return "NG"
	case "GH", "GHA", "GHANA":
		return "GH"
	case "GB", "GBR", "UK", "UNITED KINGDOM", "GREAT BRITAIN":
		return "GB"
	default:
		return strings.ToUpper(strings.TrimSpace(raw))
	}
}

func NormalizeMSISDN(raw, countryISO2 string) (string, error) {
	cleaned, err := cleanPhone(raw)
	if err != nil {
		return "", err
	}
	if cleaned == "" {
		return "", errors.New("MSISDN is empty")
	}
	if strings.HasPrefix(cleaned, "00") {
		cleaned = "+" + strings.TrimPrefix(cleaned, "00")
	}
	if strings.HasPrefix(cleaned, "+") {
		digits := strings.TrimPrefix(cleaned, "+")
		if err := validateInternationalDigits(digits); err != nil {
			return "", err
		}
		return "+" + digits, nil
	}

	countryISO2 = NormalizeCountryISO2(countryISO2)
	rule, ok := defaultDiallingRules[countryISO2]
	if !ok {
		return "", fmt.Errorf("country %q requires an explicit international MSISDN", countryISO2)
	}
	national := cleaned
	for _, prefix := range rule.NationalPrefixes {
		if strings.HasPrefix(national, prefix) {
			national = strings.TrimPrefix(national, prefix)
			break
		}
	}
	if len(national) < rule.MinNationalLen || len(national) > rule.MaxNationalLen {
		return "", fmt.Errorf("national number length %d is invalid for %s", len(national), rule.ISO2)
	}
	if !allDigits(national) {
		return "", errors.New("MSISDN contains invalid characters")
	}
	return "+" + rule.CallingCode + national, nil
}

func cleanPhone(value string) (string, error) {
	var out strings.Builder
	trimmed := strings.TrimSpace(value)
	for i, r := range trimmed {
		switch {
		case unicode.IsDigit(r):
			out.WriteRune(r)
		case r == '+' && i == 0:
			out.WriteRune(r)
		case unicode.IsSpace(r) || r == '-' || r == '(' || r == ')' || r == '.':
			// Recognised visual separator.
		default:
			return "", fmt.Errorf("MSISDN contains unsupported character %q", r)
		}
	}
	return out.String(), nil
}

func validateInternationalDigits(digits string) error {
	if !allDigits(digits) {
		return errors.New("international MSISDN contains invalid characters")
	}
	if len(digits) < 8 || len(digits) > 15 {
		return fmt.Errorf("international MSISDN must contain 8 to 15 digits; got %d", len(digits))
	}
	if strings.HasPrefix(digits, "0") {
		return errors.New("international MSISDN cannot begin with zero")
	}
	return nil
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
