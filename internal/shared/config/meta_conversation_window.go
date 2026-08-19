package config

import (
	"fmt"
	"strings"
	"time"
)

const DefaultMetaConversationWindow = 24 * time.Hour

func ParseMetaConversationWindow(raw string) (time.Duration, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return DefaultMetaConversationWindow, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("META_CLOUD_CONVERSATION_WINDOW must be a valid duration")
	}
	if parsed <= 0 || parsed > DefaultMetaConversationWindow {
		return 0, fmt.Errorf("META_CLOUD_CONVERSATION_WINDOW must be positive and no more than 24 hours")
	}
	return parsed, nil
}
