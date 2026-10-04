package postgres

import "testing"

func TestResolveBootstrapIdentityIgnoresUnrelatedExistingUser(t *testing.T) {
	existingID, create, err := resolveBootstrapIdentity(
		"00000000-0000-4000-8000-000000000001",
		"admin@example.com",
		[]bootstrapIdentityRecord{{
			ID:    "00000000-0000-4000-8000-000000000003",
			Email: "service.sender-health@internal.invalid",
		}},
	)
	if err != nil {
		t.Fatalf("resolve bootstrap identity: %v", err)
	}
	if !create {
		t.Fatalf("unrelated existing user must not suppress bootstrap creation; existing=%q", existingID)
	}
	if existingID != "" {
		t.Fatalf("unexpected existing identity %q", existingID)
	}
}

func TestResolveBootstrapIdentityRequiresExactIdentityPair(t *testing.T) {
	tests := []struct {
		name    string
		matches []bootstrapIdentityRecord
	}{
		{
			name: "bootstrap ID belongs to another email",
			matches: []bootstrapIdentityRecord{{
				ID:    "00000000-0000-4000-8000-000000000001",
				Email: "different@example.com",
			}},
		},
		{
			name: "bootstrap email belongs to another ID",
			matches: []bootstrapIdentityRecord{{
				ID:    "00000000-0000-4000-8000-000000000004",
				Email: "admin@example.com",
			}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, create, err := resolveBootstrapIdentity(
				"00000000-0000-4000-8000-000000000001",
				"admin@example.com",
				test.matches,
			)
			if err == nil {
				t.Fatal("expected bootstrap identity collision to be rejected")
			}
			if create {
				t.Fatal("identity collision must not request creation")
			}
		})
	}
}

func TestResolveBootstrapIdentityReturnsExistingExactPair(t *testing.T) {
	existingID, create, err := resolveBootstrapIdentity(
		"00000000-0000-4000-8000-000000000001",
		"Admin@example.com",
		[]bootstrapIdentityRecord{{
			ID:    "00000000-0000-4000-8000-000000000001",
			Email: "admin@example.com",
		}},
	)
	if err != nil {
		t.Fatalf("resolve bootstrap identity: %v", err)
	}
	if create || existingID != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("expected exact identity pair to be reused; existing=%q create=%t", existingID, create)
	}
}
