package dispatch

import (
	"bytes"
	"encoding/json"
	"errors"
)

func decodePayload(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	payload := target.(*JobPayload)
	if payload.CampaignRecipientID == "" {
		return errors.New("campaignRecipientId is required")
	}
	return nil
}
