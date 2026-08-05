package provider

import (
	"errors"
	"strconv"
	"strings"
)

// VersionAtLeast compares semantic versions used by provider adapters and
// gateway pools. Build metadata is ignored and prerelease versions sort below
// the corresponding stable release. Invalid or ambiguous versions fail closed.
func VersionAtLeast(actual, minimum string) (bool, error) {
	a, err := parseVersion(actual)
	if err != nil {
		return false, errors.New("invalid actual provider version: " + err.Error())
	}
	m, err := parseVersion(minimum)
	if err != nil {
		return false, errors.New("invalid minimum provider version: " + err.Error())
	}
	return compareVersion(a, m) >= 0, nil
}

type semanticVersion struct {
	core       [3]int64
	prerelease []string
}

func parseVersion(value string) (semanticVersion, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")
	if value == "" {
		return semanticVersion{}, errors.New("version is required")
	}
	if i := strings.IndexByte(value, '+'); i >= 0 {
		value = value[:i]
	}
	var prerelease []string
	if i := strings.IndexByte(value, '-'); i >= 0 {
		if i == len(value)-1 {
			return semanticVersion{}, errors.New("empty prerelease")
		}
		prerelease = strings.Split(value[i+1:], ".")
		value = value[:i]
	}
	parts := strings.Split(value, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return semanticVersion{}, errors.New("version must contain one to three numeric components")
	}
	var result semanticVersion
	for i, part := range parts {
		if part == "" {
			return semanticVersion{}, errors.New("empty numeric component")
		}
		if len(part) > 1 && part[0] == '0' {
			return semanticVersion{}, errors.New("numeric component has a leading zero")
		}
		n, err := strconv.ParseInt(part, 10, 64)
		if err != nil || n < 0 {
			return semanticVersion{}, errors.New("non-numeric version component")
		}
		result.core[i] = n
	}
	for _, identifier := range prerelease {
		if identifier == "" {
			return semanticVersion{}, errors.New("empty prerelease identifier")
		}
		for _, r := range identifier {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-') {
				return semanticVersion{}, errors.New("invalid prerelease identifier")
			}
		}
		if isNumeric(identifier) && len(identifier) > 1 && identifier[0] == '0' {
			return semanticVersion{}, errors.New("numeric prerelease identifier has a leading zero")
		}
	}
	result.prerelease = prerelease
	return result, nil
}

func compareVersion(a, b semanticVersion) int {
	for i := range a.core {
		if a.core[i] < b.core[i] {
			return -1
		}
		if a.core[i] > b.core[i] {
			return 1
		}
	}
	if len(a.prerelease) == 0 && len(b.prerelease) == 0 {
		return 0
	}
	if len(a.prerelease) == 0 {
		return 1
	}
	if len(b.prerelease) == 0 {
		return -1
	}
	limit := len(a.prerelease)
	if len(b.prerelease) < limit {
		limit = len(b.prerelease)
	}
	for i := 0; i < limit; i++ {
		av, bv := a.prerelease[i], b.prerelease[i]
		an, bn := isNumeric(av), isNumeric(bv)
		switch {
		case an && bn:
			ai, _ := strconv.ParseInt(av, 10, 64)
			bi, _ := strconv.ParseInt(bv, 10, 64)
			if ai < bi {
				return -1
			}
			if ai > bi {
				return 1
			}
		case an && !bn:
			return -1
		case !an && bn:
			return 1
		default:
			if av < bv {
				return -1
			}
			if av > bv {
				return 1
			}
		}
	}
	if len(a.prerelease) < len(b.prerelease) {
		return -1
	}
	if len(a.prerelease) > len(b.prerelease) {
		return 1
	}
	return 0
}

func isNumeric(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
