package materialisation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/audience/cohort"
	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/segment"
)

const materialisationTestConsent = "consent-review/review-p/v7/wording/wording-p"
const materialisationTestConfiguration = "organisation-policy/policy-p/v9"

type materialisationEvidenceResolverFunc func(context.Context, MaterialisationJob) (string, string, error)

func (f materialisationEvidenceResolverFunc) ResolveMaterialisationEvidence(ctx context.Context, job MaterialisationJob) (string, string, error) {
	return f(ctx, job)
}

type staticMaterialisationEvidence struct{ consent, configuration string }

func (r staticMaterialisationEvidence) ResolveMaterialisationEvidence(context.Context, MaterialisationJob) (string, string, error) {
	return r.consent, r.configuration, nil
}

type materialisationGovernanceQuery struct {
	ids   []string
	hook  func(context.Context) error
	calls int
}

func (q *materialisationGovernanceQuery) Count(context.Context, cohort.CompiledQuery) (int64, error) {
	return int64(len(q.ids)), nil
}
func (q *materialisationGovernanceQuery) Members(ctx context.Context, compiled cohort.CompiledQuery, limit int) ([]string, error) {
	return q.MembersAfter(ctx, compiled, "", limit)
}
func (q *materialisationGovernanceQuery) MembersAfter(ctx context.Context, _ cohort.CompiledQuery, after string, limit int) ([]string, error) {
	q.calls++
	if q.hook != nil {
		if err := q.hook(ctx); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var result []string
	for _, identifier := range q.ids {
		if identifier > after {
			result = append(result, identifier)
			if len(result) == limit {
				break
			}
		}
	}
	return result, nil
}

type materialisationObservedSnapshots struct {
	*segment.MemoryRepository
	writes int
}

func (s *materialisationObservedSnapshots) EnsureWithMembers(ctx context.Context, snapshot segment.Snapshot, members []segment.Member) (segment.Snapshot, bool, error) {
	s.writes++
	return s.MemoryRepository.EnsureWithMembers(ctx, snapshot, members)
}

type materialisationGovernanceFixture struct {
	worker    *MaterialisationWorker
	repo      *MemoryMaterialisationRepository
	snapshots *materialisationObservedSnapshots
	query     *materialisationGovernanceQuery
	job       MaterialisationJob
	now       time.Time
}

func newMaterialisationGovernanceFixture(t *testing.T, ids []string, evidence MaterialisationEvidenceResolver, frozenVersions ...string) materialisationGovernanceFixture {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2099, 2, 1, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryMaterialisationRepository()
	snapshots := &materialisationObservedSnapshots{MemoryRepository: segment.NewMemoryRepository()}
	query := &materialisationGovernanceQuery{ids: ids}
	registry, err := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
	if err != nil {
		t.Fatal(err)
	}
	execution := cohort.NewExecutionService(cohort.NewCompiler(registry), query)
	service := &MaterialisationService{Repository: repo, Clock: func() time.Time { return now }}
	consentVersion, configurationVersion := materialisationTestConsent, materialisationTestConfiguration
	if len(frozenVersions) == 2 {
		consentVersion, configurationVersion = frozenVersions[0], frozenVersions[1]
	}
	job, err := service.Schedule(ctx, ScheduleMaterialisation{
		CampaignID: "campaign-governance", Definition: testDefinition(), DefinitionVersion: 1,
		Eligibility:          cohort.EligibilityContext{OrganisationID: "org-governance", PurposeID: "purpose-governance", Channel: "WHATSAPP", AsOf: now},
		ConsentPolicyVersion: consentVersion, ConfigurationVersion: configurationVersion,
		RequestedBy: "operator-governance", ExpectedCount: int64(len(ids)),
	})
	if err != nil {
		t.Fatal(err)
	}
	worker := &MaterialisationWorker{
		Repository: repo, Cohorts: execution, Snapshots: snapshots, Evidence: evidence,
		WorkerID: "governance-worker", BatchSize: 1, ClaimBatch: 1, LeaseDuration: time.Minute,
		MaxAttempts: 3, RetryBackoff: time.Second, Clock: func() time.Time { return now },
	}
	return materialisationGovernanceFixture{worker, repo, snapshots, query, job, now}
}

func (f materialisationGovernanceFixture) claim(t *testing.T) MaterialisationJob {
	t.Helper()
	claimed, err := f.repo.Claim(context.Background(), f.worker.WorkerID, 1, time.Minute, f.worker.now())
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim=%+v err=%v", claimed, err)
	}
	return claimed[0]
}

func (f materialisationGovernanceFixture) assertUnpublished(t *testing.T, stagedCount int64) {
	t.Helper()
	job, err := f.repo.Get(context.Background(), f.job.ID)
	if err != nil {
		t.Fatal(err)
	}
	members, err := f.repo.StagedMembers(context.Background(), f.job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.ProcessedCount != stagedCount || int64(len(members)) != stagedCount || job.SnapshotID != "" || f.snapshots.writes != 0 {
		t.Fatalf("stale governance persisted progress/snapshot: job=%+v staged=%+v snapshotWrites=%d", job, members, f.snapshots.writes)
	}
}

func TestMaterialisationWorkerRejectsChangedGovernanceBeforeAndAfterMembership(t *testing.T) {
	cases := []struct{ name, consent, configuration string }{
		{"review ID", "consent-review/review-q/v7/wording/wording-p", materialisationTestConfiguration},
		{"review version", "consent-review/review-p/v8/wording/wording-p", materialisationTestConfiguration},
		{"wording version", "consent-review/review-p/v7/wording/wording-q", materialisationTestConfiguration},
		{"policy ID", materialisationTestConsent, "organisation-policy/policy-q/v9"},
		{"policy version", materialisationTestConsent, "organisation-policy/policy-p/v10"},
	}
	for _, phase := range []string{"before", "after"} {
		for _, test := range cases {
			t.Run(phase+"/"+test.name, func(t *testing.T) {
				consent, configuration := materialisationTestConsent, materialisationTestConfiguration
				resolver := materialisationEvidenceResolverFunc(func(context.Context, MaterialisationJob) (string, string, error) {
					return consent, configuration, nil
				})
				fixture := newMaterialisationGovernanceFixture(t, []string{"contact-a"}, resolver)
				if phase == "before" {
					consent, configuration = test.consent, test.configuration
				} else {
					fixture.query.hook = func(context.Context) error {
						consent, configuration = test.consent, test.configuration
						return nil
					}
				}
				err := fixture.worker.process(context.Background(), fixture.claim(t))
				if err == nil {
					t.Fatal("changed governance was accepted for a membership page")
				}
				fixture.assertUnpublished(t, 0)
				if phase == "before" && fixture.query.calls != 0 {
					t.Fatalf("membership ran before frozen governance was checked: calls=%d", fixture.query.calls)
				}
			})
		}
	}
}

func TestMaterialisationWorkerRejectsUnavailableAndEmptyGovernance(t *testing.T) {
	for _, phase := range []string{"before", "after"} {
		for _, kind := range []string{"unavailable", "empty consent", "empty configuration"} {
			t.Run(phase+"/"+kind, func(t *testing.T) {
				invalid := phase == "before"
				resolver := materialisationEvidenceResolverFunc(func(context.Context, MaterialisationJob) (string, string, error) {
					if invalid {
						switch kind {
						case "unavailable":
							return "", "", errors.New("authoritative governance unavailable")
						case "empty consent":
							return "", materialisationTestConfiguration, nil
						default:
							return materialisationTestConsent, "", nil
						}
					}
					return materialisationTestConsent, materialisationTestConfiguration, nil
				})
				fixture := newMaterialisationGovernanceFixture(t, []string{"contact-a"}, resolver)
				fixture.query.hook = func(context.Context) error { invalid = true; return nil }
				if err := fixture.worker.process(context.Background(), fixture.claim(t)); err == nil {
					t.Fatal("unavailable/incomplete governance allowed progress")
				}
				fixture.assertUnpublished(t, 0)
			})
		}
	}
}

func TestMaterialisationWorkerRequiresGovernanceResolverBeforeStartupAndProcessing(t *testing.T) {
	for _, phase := range []string{"startup", "processing"} {
		t.Run(phase, func(t *testing.T) {
			fixture := newMaterialisationGovernanceFixture(t, []string{"contact-a"}, nil)
			if phase == "startup" {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				if err := fixture.worker.Run(ctx); err == nil || errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "governance") {
					t.Fatalf("worker startup did not reject missing governance resolver: %v", err)
				}
			} else if err := fixture.worker.process(context.Background(), fixture.claim(t)); err == nil {
				t.Fatal("direct processing accepted missing governance resolver")
			}
			fixture.assertUnpublished(t, 0)
		})
	}
}

type materialisationObservedCommitter struct {
	*MemoryMaterialisationRepository
	snapshots *materialisationObservedSnapshots
	commits   int
}

func (r *materialisationObservedCommitter) CommitSnapshot(ctx context.Context, identifier string, token int64, snapshot segment.Snapshot, now time.Time) (MaterialisationJob, error) {
	r.commits++
	members, err := r.StagedMembers(ctx, identifier)
	if err != nil {
		return MaterialisationJob{}, err
	}
	stored, _, err := r.snapshots.EnsureWithMembers(ctx, snapshot, members)
	if err != nil {
		return MaterialisationJob{}, err
	}
	return r.Complete(ctx, identifier, token, stored, now)
}

func TestMaterialisationWorkerRejectsGovernanceChangeOnEmptyFinalPage(t *testing.T) {
	for _, commitPath := range []string{"fallback", "atomic committer"} {
		t.Run(commitPath, func(t *testing.T) {
			configuration := materialisationTestConfiguration
			resolver := materialisationEvidenceResolverFunc(func(context.Context, MaterialisationJob) (string, string, error) {
				return materialisationTestConsent, configuration, nil
			})
			fixture := newMaterialisationGovernanceFixture(t, []string{"contact-a"}, resolver)
			if err := fixture.worker.process(context.Background(), fixture.claim(t)); err != nil {
				t.Fatal(err)
			}
			committer := &materialisationObservedCommitter{MemoryMaterialisationRepository: fixture.repo, snapshots: fixture.snapshots}
			if commitPath == "atomic committer" {
				fixture.worker.Repository = committer
			}
			fixture.query.hook = func(context.Context) error {
				configuration = "organisation-policy/policy-q/v10"
				return nil
			}
			if err := fixture.worker.process(context.Background(), fixture.claim(t)); err == nil {
				t.Fatal("empty final page published a snapshot with stale governance")
			}
			fixture.assertUnpublished(t, 1)
			if committer.commits != 0 {
				t.Fatalf("stale final page invoked snapshot commit: %d", committer.commits)
			}
		})
	}
}

func TestMaterialisationWorkerPreservesStableGovernanceAcrossResumablePages(t *testing.T) {
	fixture := newMaterialisationGovernanceFixture(t, []string{"contact-a", "contact-b"},
		staticMaterialisationEvidence{materialisationTestConsent, materialisationTestConfiguration})
	for page := 0; page < 3; page++ {
		if err := fixture.worker.process(context.Background(), fixture.claim(t)); err != nil {
			t.Fatal(err)
		}
	}
	job, err := fixture.repo.Get(context.Background(), fixture.job.ID)
	if err != nil || job.Status != MaterialisationCompleted || job.ProcessedCount != 2 {
		t.Fatalf("stable resumable materialisation=%+v err=%v", job, err)
	}
	snapshot, err := fixture.snapshots.Get(context.Background(), job.SnapshotID)
	if err != nil || snapshot.ConsentPolicyVersion != materialisationTestConsent || snapshot.ConfigurationVersion != materialisationTestConfiguration || snapshot.EligibleCount != 2 {
		t.Fatalf("stable snapshot=%+v err=%v", snapshot, err)
	}
	members, err := fixture.snapshots.Members(context.Background(), job.SnapshotID, "", 10)
	if err != nil || len(members) != 2 || members[0].ContactID != "contact-a" || members[1].ContactID != "contact-b" {
		t.Fatalf("stable snapshot members=%+v err=%v", members, err)
	}
}

func TestMaterialisationWorkerGovernanceFailureUsesExistingRetryWithoutProgress(t *testing.T) {
	fixture := newMaterialisationGovernanceFixture(t, []string{"contact-a"},
		staticMaterialisationEvidence{materialisationTestConsent, "organisation-policy/policy-q/v10"})
	if err := fixture.worker.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	job, err := fixture.repo.Get(context.Background(), fixture.job.ID)
	if err != nil || job.Status != MaterialisationPending || job.FailureCode != "MATERIALISATION_RETRY" || job.LeaseOwner != "" {
		t.Fatalf("governance failure did not retain retry path: job=%+v err=%v", job, err)
	}
	fixture.assertUnpublished(t, 0)
}

func TestMaterialisationWorkerPreservesCancellationAndLeaseFencingWithGovernance(t *testing.T) {
	for _, outcome := range []string{"cancelled", "lease takeover"} {
		t.Run(outcome, func(t *testing.T) {
			fixture := newMaterialisationGovernanceFixture(t, []string{"contact-a"},
				staticMaterialisationEvidence{materialisationTestConsent, materialisationTestConfiguration})
			claimed := fixture.claim(t)
			fixture.query.hook = func(context.Context) error {
				if outcome == "cancelled" {
					_, err := fixture.repo.Cancel(context.Background(), claimed.ID, claimed.Version, "operator", "campaign audience withdrawn", fixture.now)
					return err
				}
				reclaimed, err := fixture.repo.Claim(context.Background(), "new-worker", 1, time.Minute, fixture.now.Add(2*time.Minute))
				if err != nil || len(reclaimed) != 1 {
					return errors.New("test could not reclaim expired lease")
				}
				return nil
			}
			if err := fixture.worker.process(context.Background(), claimed); !errors.Is(err, ErrMaterialisationConflict) {
				t.Fatalf("cancelled/stale owner was not rejected: %v", err)
			}
			fixture.assertUnpublished(t, 0)
			job, err := fixture.repo.Get(context.Background(), fixture.job.ID)
			if err != nil || (outcome == "cancelled" && job.Status != MaterialisationCancelled) || (outcome == "lease takeover" && job.LeaseOwner != "new-worker") {
				t.Fatalf("cancellation/lease evidence=%+v err=%v", job, err)
			}
		})
	}
}

func TestMaterialisationWorkerRejectsLegacyPendingLabelsWithoutRelabelling(t *testing.T) {
	fixture := newMaterialisationGovernanceFixture(t, []string{"contact-a"},
		staticMaterialisationEvidence{materialisationTestConsent, materialisationTestConfiguration}, "v1", "v1")
	if err := fixture.worker.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	job, err := fixture.repo.Get(context.Background(), fixture.job.ID)
	if err != nil || job.Status != MaterialisationPending || job.FailureCode != "MATERIALISATION_RETRY" ||
		job.ConsentPolicyVersion != "v1" || job.ConfigurationVersion != "v1" {
		t.Fatalf("legacy pending job was not rejected without rewriting frozen labels: job=%+v err=%v", job, err)
	}
	fixture.assertUnpublished(t, 0)
	if fixture.query.calls != 0 {
		t.Fatal("legacy job queried membership before binding exact governance evidence")
	}
}

type materialisationReadHookRepository struct {
	*MemoryMaterialisationRepository
	beforeRead func()
}

func (r *materialisationReadHookRepository) StagedMembers(ctx context.Context, identifier string) ([]segment.Member, error) {
	r.beforeRead()
	return r.MemoryMaterialisationRepository.StagedMembers(ctx, identifier)
}

func TestMaterialisationWorkerRevalidatesAfterFallbackStagedMemberRead(t *testing.T) {
	configuration := materialisationTestConfiguration
	resolver := materialisationEvidenceResolverFunc(func(context.Context, MaterialisationJob) (string, string, error) {
		return materialisationTestConsent, configuration, nil
	})
	fixture := newMaterialisationGovernanceFixture(t, []string{"contact-a"}, resolver)
	if err := fixture.worker.process(context.Background(), fixture.claim(t)); err != nil {
		t.Fatal(err)
	}
	fixture.worker.Repository = &materialisationReadHookRepository{
		MemoryMaterialisationRepository: fixture.repo,
		beforeRead:                      func() { configuration = "organisation-policy/policy-q/v10" },
	}
	if err := fixture.worker.process(context.Background(), fixture.claim(t)); err == nil {
		t.Fatal("governance changed during staged member read but snapshot was published")
	}
	fixture.assertUnpublished(t, 1)
}
