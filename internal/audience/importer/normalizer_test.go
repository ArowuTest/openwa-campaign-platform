package importer

import "testing"

func TestNormalizeNigerianNationalNumber(t *testing.T) {
	got, err := NormalizeMSISDN("0801 234 5678", "NG")
	if err != nil {
		t.Fatalf("normalise: %v", err)
	}
	if got != "+2348012345678" {
		t.Fatalf("unexpected result %s", got)
	}
}

func TestNormalizeInternationalNumber(t *testing.T) {
	got, err := NormalizeMSISDN("00 233 24 123 4567", "")
	if err != nil {
		t.Fatalf("normalise: %v", err)
	}
	if got != "+233241234567" {
		t.Fatalf("unexpected result %s", got)
	}
}

func TestRejectInvalidNationalLength(t *testing.T) {
	if _, err := NormalizeMSISDN("0801234", "NG"); err == nil {
		t.Fatal("expected invalid length error")
	}
}

func TestRejectsAlphabeticCharactersInsteadOfSilentlyRemovingThem(t *testing.T) {
	if _, err := NormalizeMSISDN("0801ABC45678", "NG"); err == nil {
		t.Fatal("expected unsupported character error")
	}
}

func TestRejectsNationalNumberWithoutExplicitCountry(t *testing.T) {
	if _, err := NormalizeMSISDN("08012345678", ""); err == nil {
		t.Fatal("national number without an explicit country was guessed")
	}
}
