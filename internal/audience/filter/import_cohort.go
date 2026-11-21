package filter

import (
	"errors"
	"regexp"
	"strconv"
)

const ImportCohortCode = "IMPORT_COHORT"

var importCohortValue = regexp.MustCompile(`^([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})/([1-9][0-9]?)/([1-9][0-9]?)$`)

// ParseImportCohort reads the immutable v1 rule: import UUID / one-based part / total.
func ParseImportCohort(rule Rule) (string, int, int, error) {
	invalid := errors.New("upload cohort requires import UUID/part/total with 1 <= part <= total <= 32")
	if rule.Operator != OperatorEquals || len(rule.Values) != 1 {
		return "", 0, 0, invalid
	}
	value, _ := rule.Values[0].(string)
	m := importCohortValue.FindStringSubmatch(value)
	if m == nil {
		return "", 0, 0, invalid
	}
	part, _ := strconv.Atoi(m[2])
	total, _ := strconv.Atoi(m[3])
	if part > total || total > 32 {
		return "", 0, 0, invalid
	}
	return m[1], part, total, nil
}
