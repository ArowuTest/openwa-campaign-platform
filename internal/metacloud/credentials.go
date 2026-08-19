package metacloud

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
)

type Credential struct {
	AccessToken string
	AppSecret   string
	VerifyToken string
}

type CredentialResolver interface {
	Resolve(string) (Credential, error)
}

type CredentialSet struct{ byKey map[string]Credential }

var ErrCredentialNotFound = errors.New("Meta Cloud credential not found")
var ErrCredentialSetInvalid = errors.New("Meta Cloud credential set is invalid")

type credentialEntry struct {
	Key         string `json:"key"`
	AccessToken string `json:"accessToken"`
	AppSecret   string `json:"appSecret"`
	VerifyToken string `json:"verifyToken"`
}

func ParseCredentialSet(raw string) (*CredentialSet, error) {
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	decoder.DisallowUnknownFields()
	var entries []credentialEntry
	if err := decoder.Decode(&entries); err != nil || len(entries) == 0 {
		return nil, ErrCredentialSetInvalid
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, ErrCredentialSetInvalid
	}
	set := &CredentialSet{byKey: make(map[string]Credential, len(entries))}
	for _, entry := range entries {
		key := strings.TrimSpace(entry.Key)
		access := strings.TrimSpace(entry.AccessToken)
		app := strings.TrimSpace(entry.AppSecret)
		verify := strings.TrimSpace(entry.VerifyToken)
		if !credentialKeyPattern.MatchString(key) || len(access) < 20 || len(app) < 16 || len(verify) < 12 {
			return nil, ErrCredentialSetInvalid
		}
		if _, exists := set.byKey[key]; exists {
			return nil, ErrCredentialSetInvalid
		}
		set.byKey[key] = Credential{AccessToken: access, AppSecret: app, VerifyToken: verify}
	}
	return set, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	}
	return ErrCredentialSetInvalid
}

func (s *CredentialSet) Resolve(key string) (Credential, error) {
	if s == nil {
		return Credential{}, ErrCredentialNotFound
	}
	value, ok := s.byKey[strings.TrimSpace(key)]
	if !ok {
		return Credential{}, ErrCredentialNotFound
	}
	return value, nil
}
