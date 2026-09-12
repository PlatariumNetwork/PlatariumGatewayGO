package protocol

import "testing"

func TestResolveAuthenticatedOwnerMissingAuth(t *testing.T) {
	_, err := ResolveAuthenticatedOwner("PxA", "")
	if err == nil {
		t.Fatal("expected missing authentication")
	}
}

func TestResolveAuthenticatedOwnerWrongAddress(t *testing.T) {
	_, err := ResolveAuthenticatedOwner("PxClaimed", "PxAuthenticated")
	if err == nil {
		t.Fatal("expected wrong address rejection")
	}
}

func TestResolveAuthenticatedOwnerValid(t *testing.T) {
	got, err := ResolveAuthenticatedOwner("PxAbC", "pxabc")
	if err != nil {
		t.Fatal(err)
	}
	if got != "pxabc" {
		t.Fatalf("verified=%q", got)
	}
}

func TestResolveAuthenticatedOwnerRejectsOwnedPrefix(t *testing.T) {
	if _, err := ResolveAuthenticatedOwner("owned:PxA", "PxA"); err == nil {
		t.Fatal("claimed owned: must be rejected")
	}
	if _, err := ResolveAuthenticatedOwner("PxA", "owned:PxA"); err == nil {
		t.Fatal("authenticated owned: must be rejected")
	}
	if err := RejectClientOwnedProof("owned:forged"); err == nil {
		t.Fatal("client owned: proof must be rejected")
	}
}
