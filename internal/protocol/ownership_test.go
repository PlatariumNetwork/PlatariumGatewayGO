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

func TestMintOwnedProofOnlyAfterResolveSuccess(t *testing.T) {
	if _, err := MintOwnedProof(""); err == nil {
		t.Fatal("empty verified owner must not mint")
	}
	if _, err := MintOwnedProof("owned:PxA"); err == nil {
		t.Fatal("owned: input must not mint")
	}
	if _, err := MintOwnedProofAfterResolve("PxA", ""); err == nil {
		t.Fatal("mint after failed resolve must error")
	}
	if _, err := MintOwnedProofAfterResolve("PxClaim", "PxOther"); err == nil {
		t.Fatal("mint after mismatch resolve must error")
	}
	verified, err := ResolveAuthenticatedOwner("PxAbC", "pxabc")
	if err != nil {
		t.Fatal(err)
	}
	proof, err := MintOwnedProof(verified)
	if err != nil {
		t.Fatal(err)
	}
	if proof != "owned:pxabc" {
		t.Fatalf("proof=%q", proof)
	}
	proof2, err := MintOwnedProofAfterResolve("PxAbC", "pxabc")
	if err != nil {
		t.Fatal(err)
	}
	if proof2 != "owned:pxabc" {
		t.Fatalf("afterResolve=%q", proof2)
	}
}
