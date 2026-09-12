package websocket

import (
	"fmt"
	"testing"
)

func TestAuthenticatedOwnerDistinguishesClaimed(t *testing.T) {
	c := &Client{Address: "PxClaimed", Authenticated: false}
	if c.AuthenticatedOwner() != "" {
		t.Fatal("claimed-only session must not expose authenticated owner")
	}
	c.Authenticated = true
	if c.AuthenticatedOwner() != "PxClaimed" {
		t.Fatalf("got %q", c.AuthenticatedOwner())
	}
}

func TestResolveRegisterAuthenticationWithoutProof(t *testing.T) {
	s := &Server{}
	authed, err := s.resolveRegisterAuthentication("PxA", map[string]interface{}{"address": "PxA"})
	if err != nil {
		t.Fatal(err)
	}
	if authed {
		t.Fatal("register without mnemonic/sig must leave session unauthenticated")
	}
}

func TestResolveRegisterAuthenticationValidProof(t *testing.T) {
	s := &Server{}
	s.SetOwnershipProver(func(address, signature, mnemonic, alphanumeric, pubMain string) error {
		if address != "PxA" || mnemonic != "m" || alphanumeric != "a" {
			return fmt.Errorf("unexpected proof inputs")
		}
		return nil
	})
	authed, err := s.resolveRegisterAuthentication("PxA", map[string]interface{}{
		"mnemonic":     "m",
		"alphanumeric": "a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !authed {
		t.Fatal("valid proof must authenticate")
	}
}

func TestResolveRegisterAuthenticationInvalidProof(t *testing.T) {
	s := &Server{}
	s.SetOwnershipProver(func(address, signature, mnemonic, alphanumeric, pubMain string) error {
		return fmt.Errorf("bad proof")
	})
	authed, err := s.resolveRegisterAuthentication("PxA", map[string]interface{}{
		"mnemonic":     "m",
		"alphanumeric": "a",
	})
	if err == nil {
		t.Fatal("expected proof error")
	}
	if authed {
		t.Fatal("failed proof must not authenticate")
	}
}
