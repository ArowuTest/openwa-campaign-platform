package sender

import "testing"

func TestNewSessionCannotBecomeReadyWithoutPairing(t *testing.T) {
	if AllowedSessionTransition(StatusNew, StatusReady) {
		t.Fatal("NEW session must not transition directly to READY")
	}
	if !AllowedSessionTransition(StatusNew, StatusPairing) || !AllowedSessionTransition(StatusPairing, StatusConnecting) || !AllowedSessionTransition(StatusConnecting, StatusReady) {
		t.Fatal("expected governed pairing path to READY")
	}
}

func TestRetiredSessionIsTerminal(t *testing.T) {
	for _, target := range []Status{StatusNew, StatusPairing, StatusReady, StatusRecovering, StatusQuarantined} {
		if AllowedSessionTransition(StatusRetired, target) {
			t.Fatalf("RETIRED session unexpectedly transitions to %s", target)
		}
	}
}

func TestRecoveryFailureRequiresGovernedRecoveryOrRetirement(t *testing.T) {
	if !AllowedSessionTransition(StatusRecoveryFail, StatusRecovering) || !AllowedSessionTransition(StatusRecoveryFail, StatusRetired) {
		t.Fatal("failed recovery should support retry or retirement")
	}
	if AllowedSessionTransition(StatusRecoveryFail, StatusReady) {
		t.Fatal("failed recovery must not bypass verification and become READY")
	}
}
