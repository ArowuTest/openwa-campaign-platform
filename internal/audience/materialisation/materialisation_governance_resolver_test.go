package materialisation

import (
	"context"
	"errors"
	"testing"
	"time"

	"campaign-platform/internal/audience/cohort"
	"campaign-platform/internal/consent"
	"campaign-platform/internal/organisation"
	postgresrepo "campaign-platform/internal/persistence/postgres"
)

type materialisationEstimateEvidenceFunc func(context.Context, cohort.EstimateJobRecord) (cohort.EstimateGovernanceEvidence, error)

func (f materialisationEstimateEvidenceFunc) ResolveEstimateEvidence(ctx context.Context, record cohort.EstimateJobRecord) (cohort.EstimateGovernanceEvidence, error) {
	return f(ctx, record)
}

func TestMaterialisationGovernanceResolverUsesFrozenContextAndCanonicalVersions(t *testing.T) {
	asOf := time.Date(2099, 2, 1, 12, 0, 0, 0, time.FixedZone("fixture", 3600))
	job := MaterialisationJob{Eligibility: cohort.EligibilityContext{
		OrganisationID: "org-frozen", PurposeID: "purpose-frozen", Channel: "WHATSAPP", AsOf: asOf,
	}}
	resolver := &GovernanceEvidenceResolver{Evidence: materialisationEstimateEvidenceFunc(func(ctx context.Context, record cohort.EstimateJobRecord) (cohort.EstimateGovernanceEvidence, error) {
		if record.OrganisationID != "org-frozen" || record.PurposeID != "purpose-frozen" ||
			record.Channel != "WHATSAPP" || !record.AsOf.Equal(asOf) || record.AsOf.Location() != time.UTC {
			t.Fatalf("resolver did not use frozen job context: %+v", record)
		}
		return cohort.EstimateGovernanceEvidence{
			ConsentReviewID: "review-p", ConsentReviewVersion: 7, ConsentWordingVersion: " wording-p ",
			OrganisationPolicyID: "policy-p", OrganisationPolicyVersion: 9,
		}, nil
	})}
	consentVersion, configurationVersion, err := resolver.ResolveMaterialisationEvidence(context.Background(), job)
	if err != nil || consentVersion != "consent-review/review-p/v7/wording/wording-p" ||
		configurationVersion != "organisation-policy/policy-p/v9" {
		t.Fatalf("frozen governance binding consent=%q configuration=%q err=%v", consentVersion, configurationVersion, err)
	}
}

func TestMaterialisationGovernanceResolverRejectsUnavailableAndIncompleteEvidence(t *testing.T) {
	complete := cohort.EstimateGovernanceEvidence{
		ConsentReviewID: "review-p", ConsentReviewVersion: 7, ConsentWordingVersion: "wording-p",
		OrganisationPolicyID: "policy-p", OrganisationPolicyVersion: 9,
	}
	for _, field := range []string{"review ID", "review version", "wording version", "policy ID", "policy version"} {
		t.Run(field, func(t *testing.T) {
			value := complete
			switch field {
			case "review ID":
				value.ConsentReviewID = " "
			case "review version":
				value.ConsentReviewVersion = 0
			case "wording version":
				value.ConsentWordingVersion = " "
			case "policy ID":
				value.OrganisationPolicyID = ""
			case "policy version":
				value.OrganisationPolicyVersion = -1
			}
			resolver := &GovernanceEvidenceResolver{Evidence: materialisationEstimateEvidenceFunc(func(context.Context, cohort.EstimateJobRecord) (cohort.EstimateGovernanceEvidence, error) {
				return value, nil
			})}
			if consent, config, err := resolver.ResolveMaterialisationEvidence(context.Background(), MaterialisationJob{}); err == nil || consent != "" || config != "" {
				t.Fatalf("incomplete governance accepted: consent=%q config=%q err=%v", consent, config, err)
			}
		})
	}
	unavailable := errors.New("governance read unavailable")
	resolver := &GovernanceEvidenceResolver{Evidence: materialisationEstimateEvidenceFunc(func(context.Context, cohort.EstimateJobRecord) (cohort.EstimateGovernanceEvidence, error) {
		return cohort.EstimateGovernanceEvidence{}, unavailable
	})}
	if _, _, err := resolver.ResolveMaterialisationEvidence(context.Background(), MaterialisationJob{}); !errors.Is(err, unavailable) {
		t.Fatalf("authoritative read failure was not preserved: %v", err)
	}
	for _, resolver := range []*GovernanceEvidenceResolver{nil, {}} {
		if _, _, err := resolver.ResolveMaterialisationEvidence(context.Background(), MaterialisationJob{}); err == nil {
			t.Fatal("missing authoritative resolver was accepted")
		}
	}
}

func TestMaterialisationWorkerRejectsUnconfiguredGovernanceAdapterAtStartup(t *testing.T) {
	var nilAdapter *GovernanceEvidenceResolver
	var nilAuthoritative *cohort.GovernanceEvidenceResolver
	for _, test := range []struct {
		name     string
		evidence MaterialisationEvidenceResolver
	}{
		{"typed nil adapter", nilAdapter},
		{"empty adapter", &GovernanceEvidenceResolver{}},
		{"typed nil authoritative resolver", &GovernanceEvidenceResolver{Evidence: nilAuthoritative}},
		{"incomplete authoritative resolver", &GovernanceEvidenceResolver{Evidence: &cohort.GovernanceEvidenceResolver{}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newMaterialisationGovernanceFixture(t, []string{"contact-a"}, test.evidence)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := fixture.worker.Run(ctx); err == nil || errors.Is(err, context.Canceled) {
				t.Fatalf("unconfigured production governance resolver reached worker readiness: %v", err)
			}
			fixture.assertUnpublished(t, 0)
		})
	}
}

type materialisationNilEvidence struct {
	materialisationEvidenceResolverFunc
}
type materialisationNilEstimateEvidence struct {
	materialisationEstimateEvidenceFunc
}

type materialisationValidatedEstimateEvidence struct {
	materialisationEstimateEvidenceFunc
	validationErr error
}

func (r materialisationValidatedEstimateEvidence) Validate() error { return r.validationErr }

type materialisationValidatedPurposeRepository struct {
	consent.PurposeRepository
	validationErr error
}

func (r materialisationValidatedPurposeRepository) Validate() error { return r.validationErr }

type materialisationValidatedReviewRepository struct {
	consent.Repository
	validationErr error
}

func (r materialisationValidatedReviewRepository) Validate() error { return r.validationErr }

type materialisationValidatedPolicyStore struct {
	organisation.PolicyStore
	validationErr error
}

func (r materialisationValidatedPolicyStore) Validate() error { return r.validationErr }

func configuredMaterialisationGovernanceResolver() *cohort.GovernanceEvidenceResolver {
	return &cohort.GovernanceEvidenceResolver{
		Purposes: &consent.PurposeService{Repository: consent.NewMemoryPurposeRepository()},
		Reviews:  consent.NewService(consent.NewMemoryRepository()),
		Policies: &organisation.PolicyAdministration{Store: organisation.NewMemoryPolicyStore()},
	}
}

func TestMaterialisationWorkerRejectsIncompleteGovernanceDependenciesBeforeClaim(t *testing.T) {
	var nilWorkerPointer *materialisationNilEvidence
	var nilWorkerFunction materialisationEvidenceResolverFunc
	var nilEstimatePointer *materialisationNilEstimateEvidence
	var nilEstimateFunction materialisationEstimateEvidenceFunc
	var nilPurposes *consent.MemoryPurposeRepository
	var nilReviews *consent.MemoryRepository
	var nilPolicies *organisation.MemoryPolicyStore
	unavailable := errors.New("governance dependency configuration failed")
	for _, test := range []struct {
		name      string
		evidence  MaterialisationEvidenceResolver
		configure func(*cohort.GovernanceEvidenceResolver)
		wantErr   error
	}{
		{name: "typed nil worker pointer", evidence: nilWorkerPointer},
		{name: "typed nil worker function", evidence: nilWorkerFunction},
		{name: "typed nil evidence pointer", evidence: &GovernanceEvidenceResolver{Evidence: nilEstimatePointer}},
		{name: "typed nil evidence function", evidence: &GovernanceEvidenceResolver{Evidence: nilEstimateFunction}},
		{name: "nil purpose repository", configure: func(r *cohort.GovernanceEvidenceResolver) { r.Purposes.Repository = nil }},
		{name: "typed nil purpose repository", configure: func(r *cohort.GovernanceEvidenceResolver) { r.Purposes.Repository = nilPurposes }},
		{name: "nil review repository", configure: func(r *cohort.GovernanceEvidenceResolver) { r.Reviews = consent.NewService(nil) }},
		{name: "typed nil review repository", configure: func(r *cohort.GovernanceEvidenceResolver) { r.Reviews = consent.NewService(nilReviews) }},
		{name: "typed nil policy store", configure: func(r *cohort.GovernanceEvidenceResolver) { r.Policies.Store = nilPolicies }},
		{name: "policy repository without database", configure: func(r *cohort.GovernanceEvidenceResolver) {
			r.Policies.Store = &postgresrepo.OrganisationPolicyRepository{}
		}},
		{name: "invalid evidence configuration", evidence: &GovernanceEvidenceResolver{Evidence: materialisationValidatedEstimateEvidence{validationErr: unavailable}}, wantErr: unavailable},
		{name: "invalid purpose repository configuration", configure: func(r *cohort.GovernanceEvidenceResolver) {
			r.Purposes.Repository = materialisationValidatedPurposeRepository{PurposeRepository: r.Purposes.Repository, validationErr: unavailable}
		}, wantErr: unavailable},
		{name: "invalid review repository configuration", configure: func(r *cohort.GovernanceEvidenceResolver) {
			r.Reviews = consent.NewService(materialisationValidatedReviewRepository{Repository: consent.NewMemoryRepository(), validationErr: unavailable})
		}, wantErr: unavailable},
		{name: "invalid policy store configuration", configure: func(r *cohort.GovernanceEvidenceResolver) {
			r.Policies.Store = materialisationValidatedPolicyStore{PolicyStore: r.Policies.Store, validationErr: unavailable}
		}, wantErr: unavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			evidence := test.evidence
			if test.configure != nil {
				authoritative := configuredMaterialisationGovernanceResolver()
				test.configure(authoritative)
				evidence = &GovernanceEvidenceResolver{Evidence: authoritative}
			}
			fixture := newMaterialisationGovernanceFixture(t, []string{"contact-a"}, evidence)
			if err := fixture.worker.Validate(); err == nil || (test.wantErr != nil && !errors.Is(err, test.wantErr)) {
				t.Errorf("invalid dependency reached readiness: %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := fixture.worker.Run(ctx); err == nil || errors.Is(err, context.Canceled) ||
				(test.wantErr != nil && !errors.Is(err, test.wantErr)) {
				t.Errorf("invalid dependency reached the claim loop: %v", err)
			}
			job, err := fixture.repo.Get(context.Background(), fixture.job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if job.Status != MaterialisationPending || job.AttemptCount != 0 || job.LeaseOwner != "" || job.LeaseToken != 0 {
				t.Errorf("startup failure claimed work: status=%s attempts=%d owner=%q token=%d", job.Status, job.AttemptCount, job.LeaseOwner, job.LeaseToken)
			}
			if fixture.query.calls != 0 {
				t.Errorf("startup failure queried membership: %d", fixture.query.calls)
			}
			fixture.assertUnpublished(t, 0)
		})
	}
}

func TestMaterialisationWorkerAcceptsConfiguredGovernanceWithoutOptionalValidation(t *testing.T) {
	for _, test := range []struct {
		name     string
		evidence MaterialisationEvidenceResolver
	}{
		{"plain worker provider", staticMaterialisationEvidence{materialisationTestConsent, materialisationTestConfiguration}},
		{"plain estimate provider", &GovernanceEvidenceResolver{Evidence: materialisationEstimateEvidenceFunc(func(context.Context, cohort.EstimateJobRecord) (cohort.EstimateGovernanceEvidence, error) {
			return cohort.EstimateGovernanceEvidence{}, nil
		})}},
		{"configured authoritative repositories", &GovernanceEvidenceResolver{Evidence: configuredMaterialisationGovernanceResolver()}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newMaterialisationGovernanceFixture(t, []string{"contact-a"}, test.evidence)
			if err := fixture.worker.Validate(); err != nil {
				t.Fatalf("configured provider rejected at readiness: %v", err)
			}
		})
	}
}
