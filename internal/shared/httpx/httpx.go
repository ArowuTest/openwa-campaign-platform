package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
)

type ErrorResponse struct {
	Error     string         `json:"error"`
	Message   string         `json:"message"`
	RequestID string         `json:"requestId,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

func DecodeJSON(w http.ResponseWriter, r *http.Request, maxBytes int64, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain a single JSON object")
		}
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return nil
}

func WriteJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string, details map[string]any) {
	WriteJSON(w, status, ErrorResponse{
		Error: code, Message: message, RequestID: RequestID(r.Context()), Details: details,
	})
}

type ListResponse struct {
	Items      any    `json:"items"`
	Count      int    `json:"count"`
	NextCursor string `json:"nextCursor,omitempty"`
	HasMore    bool   `json:"hasMore"`
}

func WriteListAuto(w http.ResponseWriter, status int, items any) {
	count := 0
	value := reflect.ValueOf(items)
	if value.IsValid() {
		switch value.Kind() {
		case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
			count = value.Len()
		}
	}
	WriteList(w, status, items, count, "")
}

func WriteList(w http.ResponseWriter, status int, items any, count int, nextCursor string) {
	if count < 0 {
		count = 0
	}
	nextCursor = strings.TrimSpace(nextCursor)
	WriteJSON(w, status, ListResponse{Items: items, Count: count, NextCursor: nextCursor, HasMore: nextCursor != ""})
}

type PageRequest struct {
	Limit  int
	Cursor string
}

func ParsePage(r *http.Request, defaultLimit, maximumLimit int) (PageRequest, error) {
	if defaultLimit < 1 {
		defaultLimit = 100
	}
	if maximumLimit < defaultLimit || maximumLimit > 1000 {
		maximumLimit = 500
	}
	limit := defaultLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maximumLimit {
			return PageRequest{}, fmt.Errorf("limit must be between 1 and %d", maximumLimit)
		}
		limit = parsed
	}
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	if len(cursor) > 512 {
		return PageRequest{}, errors.New("cursor exceeds 512 characters")
	}
	for _, current := range cursor {
		if current < 0x21 || current > 0x7e {
			return PageRequest{}, errors.New("cursor contains unsupported characters")
		}
	}
	return PageRequest{Limit: limit, Cursor: cursor}, nil
}
