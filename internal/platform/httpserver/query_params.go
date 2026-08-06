package httpserver

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func optionalPositiveIntQuery(r *http.Request, name string, maximum int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || (maximum > 0 && value > maximum) {
		return 0, fmt.Errorf("%s must be an integer between 1 and %d", name, maximum)
	}
	return value, nil
}

func optionalUint64Query(r *http.Request, name string) (uint64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return value, nil
}

func optionalBoolQuery(r *http.Request, name string) (bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return value, nil
}
