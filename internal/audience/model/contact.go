package model

import "time"

type Contact struct {
	ID                    string
	EncryptedMSISDN       []byte
	MSISDNLookupHMAC      []byte
	MaskedMSISDN          string
	CountryID             *string
	StateID               *string
	LGAID                 *string
	ReportedAge           *int16
	AgeRecordedAt         *time.Time
	AgeSource             *string
	AgeVerified           bool
	GenderCode            *string
	PreferredLanguageCode *string
	Status                string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}
