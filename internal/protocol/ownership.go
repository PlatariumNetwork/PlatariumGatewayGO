package protocol

import (
	"fmt"
	"strings"
)

// ResolveAuthenticatedOwner returns the verified owner address when claimed matches
// the authenticated (proven) address. Claimed alone is never treated as proof —
// authenticated must be supplied from session bind or mnemonic/sig verification.
// Client-supplied "owned:" is rejected as input proof on either argument.
func ResolveAuthenticatedOwner(claimed, authenticated string) (string, error) {
	claimed = strings.TrimSpace(claimed)
	authenticated = strings.TrimSpace(authenticated)
	if strings.HasPrefix(claimed, "owned:") || strings.HasPrefix(authenticated, "owned:") {
		return "", fmt.Errorf("owned: prefix is not valid ownership proof input")
	}
	if authenticated == "" {
		return "", fmt.Errorf("missing authentication")
	}
	if claimed == "" {
		return "", fmt.Errorf("address required")
	}
	if !strings.EqualFold(claimed, authenticated) {
		return "", fmt.Errorf("address does not match authenticated owner")
	}
	return authenticated, nil
}

// RejectClientOwnedProof returns an error when signature is a client-supplied owned: marker.
func RejectClientOwnedProof(signature string) error {
	if strings.HasPrefix(strings.TrimSpace(signature), "owned:") {
		return fmt.Errorf("owned: proof must be produced by Gateway after mnemonic verification")
	}
	return nil
}

// MintOwnedProof builds the Gateway-only ownership marker owned:<addr>.
// Call only after ResolveAuthenticatedOwner (or equivalent verify) succeeds —
// never concatenate "owned:" + address at call sites.
func MintOwnedProof(verifiedOwner string) (string, error) {
	verifiedOwner = strings.TrimSpace(verifiedOwner)
	if verifiedOwner == "" {
		return "", fmt.Errorf("missing verified owner for mint")
	}
	if strings.HasPrefix(verifiedOwner, "owned:") {
		return "", fmt.Errorf("owned: prefix is not valid mint input")
	}
	return "owned:" + verifiedOwner, nil
}

// MintOwnedProofAfterResolve resolves claimed against authenticated, then mints.
// This is the only supported path that produces owned: from claim + session proof.
func MintOwnedProofAfterResolve(claimed, authenticated string) (string, error) {
	verified, err := ResolveAuthenticatedOwner(claimed, authenticated)
	if err != nil {
		return "", err
	}
	return MintOwnedProof(verified)
}
