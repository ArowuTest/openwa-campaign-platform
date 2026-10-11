package cohort

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/segment"
)

var ErrCohortTooLarge = errors.New("cohort result exceeds configured materialisation limit")

type EligibilityBreakdown struct {
	MatchedProfiles      int64 `json:"matchedProfiles"`
	ConsentEligible      int64 `json:"consentEligible"`
	ConsentExcluded      int64 `json:"consentExcluded"`
	Unsuppressed         int64 `json:"unsuppressed"`
	SuppressionExcluded  int64 `json:"suppressionExcluded"`
	Eligible             int64 `json:"eligible"`
	FrequencyCapExcluded int64 `json:"frequencyCapExcluded"`
}

type Estimate struct {
	EligibleCount int64                 `json:"eligibleCount"`
	Breakdown     *EligibilityBreakdown `json:"breakdown,omitempty"`
	CalculatedAt  time.Time             `json:"calculatedAt"`
}

type Materialisation struct {
	Definition  audiencefilter.Group `json:"definition"`
	Eligibility EligibilityContext   `json:"eligibility"`
	Limit       int                  `json:"limit,omitempty"`
}

type QueryRepository interface {
	Count(context.Context, CompiledQuery) (int64, error)
	Members(context.Context, CompiledQuery, int) ([]string, error)
}

type BreakdownQueryRepository interface {
	CountBreakdown(context.Context, CompiledQuery) (EligibilityBreakdown, error)
}

type ExecutionService struct {
	Compiler           *Compiler
	Repository         QueryRepository
	Clock              func() time.Time
	MaxMaterialisation int
}

func NewExecutionService(compiler *Compiler, repository QueryRepository) *ExecutionService {
	return &ExecutionService{Compiler: compiler, Repository: repository, Clock: time.Now, MaxMaterialisation: 5_000_000}
}

func (s *ExecutionService) Estimate(ctx context.Context, definition audiencefilter.Group, eligibility EligibilityContext, hasPermission func(string) bool) (Estimate, error) {
	if s == nil || s.Compiler == nil || s.Repository == nil {
		return Estimate{}, errors.New("cohort execution is not configured")
	}
	var count int64
	var breakdown *EligibilityBreakdown
	if repository, ok := s.Repository.(BreakdownQueryRepository); ok {
		compiled, err := s.Compiler.CompileBreakdownForPermissions(definition, eligibility, hasPermission)
		if err != nil {
			return Estimate{}, err
		}
		value, err := repository.CountBreakdown(ctx, compiled)
		if err != nil {
			return Estimate{}, err
		}
		count = value.Eligible
		breakdown = &value
	} else {
		compiled, err := s.Compiler.CompileForPermissions(definition, eligibility, hasPermission)
		if err != nil {
			return Estimate{}, err
		}
		value, err := s.Repository.Count(ctx, compiled)
		if err != nil {
			return Estimate{}, err
		}
		count = value
	}
	clock := s.Clock
	if clock == nil {
		clock = time.Now
	}
	return Estimate{EligibleCount: count, Breakdown: breakdown, CalculatedAt: clock().UTC()}, nil
}

func (s *ExecutionService) Materialise(ctx context.Context, definition audiencefilter.Group, eligibility EligibilityContext, hasPermission func(string) bool, limit int) ([]segment.Member, error) {
	if s == nil || s.Compiler == nil || s.Repository == nil {
		return nil, errors.New("cohort execution is not configured")
	}
	if limit <= 0 {
		limit = s.MaxMaterialisation
	}
	if s.MaxMaterialisation > 0 && limit > s.MaxMaterialisation {
		limit = s.MaxMaterialisation
	}
	compiled, err := s.Compiler.CompileForPermissions(definition, eligibility, hasPermission)
	if err != nil {
		return nil, err
	}
	ids, err := s.Repository.Members(ctx, compiled, limit+1)
	if err != nil {
		return nil, err
	}
	if len(ids) > limit {
		return nil, ErrCohortTooLarge
	}
	members := make([]segment.Member, 0, len(ids))
	for _, contactID := range ids {
		members = append(members, segment.Member{ContactID: contactID, EligibilityEvidenceHash: EvidenceHash(definition, eligibility, contactID)})
	}
	return members, nil
}

func EvidenceHash(definition audiencefilter.Group, eligibility EligibilityContext, contactID string) string {
	payload, _ := json.Marshal(struct {
		Definition  audiencefilter.Group `json:"definition"`
		Eligibility EligibilityContext   `json:"eligibility"`
	}{definition, eligibility})
	digest := sha256.Sum256(append(append([]byte("cohort-eligibility-v1\x00"), payload...), []byte("\x00"+strings.TrimSpace(contactID))...))
	return hex.EncodeToString(digest[:])
}

type PostgreSQLQueryRepository struct{ DB *sql.DB }

func (r *PostgreSQLQueryRepository) Count(ctx context.Context, compiled CompiledQuery) (int64, error) {
	if r == nil || r.DB == nil {
		return 0, errors.New("database is required")
	}
	query := "SELECT count(*) FROM (" + strings.TrimSpace(compiled.SQL) + ") eligible"
	var count int64
	if err := r.DB.QueryRowContext(ctx, query, compiled.Args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count eligible cohort: %w", err)
	}
	return count, nil
}

func (r *PostgreSQLQueryRepository) CountBreakdown(ctx context.Context, compiled CompiledQuery) (EligibilityBreakdown, error) {
	if r == nil || r.DB == nil {
		return EligibilityBreakdown{}, errors.New("database is required")
	}
	var value EligibilityBreakdown
	if err := r.DB.QueryRowContext(ctx, strings.TrimSpace(compiled.SQL), compiled.Args...).Scan(
		&value.MatchedProfiles,
		&value.ConsentEligible,
		&value.Unsuppressed,
		&value.Eligible,
	); err != nil {
		return EligibilityBreakdown{}, fmt.Errorf("count cohort eligibility breakdown: %w", err)
	}
	if value.MatchedProfiles < 0 ||
		value.ConsentEligible < 0 ||
		value.Unsuppressed < 0 ||
		value.Eligible < 0 ||
		value.ConsentEligible > value.MatchedProfiles ||
		value.Unsuppressed > value.ConsentEligible ||
		value.Eligible > value.Unsuppressed {
		return EligibilityBreakdown{}, errors.New("cohort eligibility breakdown is not monotonic")
	}
	value.ConsentExcluded = value.MatchedProfiles - value.ConsentEligible
	value.SuppressionExcluded = value.ConsentEligible - value.Unsuppressed
	value.FrequencyCapExcluded = value.Unsuppressed - value.Eligible
	return value, nil
}

func (r *PostgreSQLQueryRepository) Members(ctx context.Context, compiled CompiledQuery, limit int) ([]string, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	if limit <= 0 || limit > 5_000_001 {
		return nil, errors.New("invalid cohort member limit")
	}
	query := "SELECT id::text FROM (" + strings.TrimSpace(compiled.SQL) + ") eligible ORDER BY id LIMIT $" + fmt.Sprint(len(compiled.Args)+1)
	args := append(append([]any(nil), compiled.Args...), limit)
	rows, err := r.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load eligible cohort members: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *PostgreSQLQueryRepository) MembersAfter(ctx context.Context, compiled CompiledQuery, after string, limit int) ([]string, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	if limit <= 0 || limit > 10000 {
		return nil, errors.New("invalid cohort page limit")
	}
	query := "SELECT id::text FROM (" + strings.TrimSpace(compiled.SQL) + ") eligible WHERE ($" + fmt.Sprint(len(compiled.Args)+1) + "='' OR id>NULLIF($" + fmt.Sprint(len(compiled.Args)+1) + ",'')::uuid) ORDER BY id LIMIT $" + fmt.Sprint(len(compiled.Args)+2)
	args := append(append([]any(nil), compiled.Args...), after, limit)
	rows, err := r.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("page eligible cohort members: %w", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

type MemoryQueryRepository struct {
	EligibleContactIDs []string
}

func (r *MemoryQueryRepository) Count(_ context.Context, _ CompiledQuery) (int64, error) {
	if r == nil {
		return 0, errors.New("memory cohort repository is required")
	}
	return int64(len(r.EligibleContactIDs)), nil
}

func (r *MemoryQueryRepository) Members(_ context.Context, _ CompiledQuery, limit int) ([]string, error) {
	if r == nil {
		return nil, errors.New("memory cohort repository is required")
	}
	if limit <= 0 {
		return nil, errors.New("positive cohort member limit is required")
	}
	ids := append([]string(nil), r.EligibleContactIDs...)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return ids, nil
}

func (r *MemoryQueryRepository) MembersAfter(_ context.Context, _ CompiledQuery, after string, limit int) ([]string, error) {
	if r == nil {
		return nil, errors.New("memory cohort repository is required")
	}
	if limit <= 0 {
		return nil, errors.New("positive cohort member limit is required")
	}
	out := []string{}
	for _, v := range r.EligibleContactIDs {
		if v > after {
			out = append(out, v)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
