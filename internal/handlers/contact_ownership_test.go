package handlers

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyContactRespondRejectsForgedOwned(t *testing.T) {
	h := &Handler{}
	_, _, err := h.verifyContactRespondOwnership("PxActor", "req1", "accepted", "owned:forged", "", "", "")
	if err == nil {
		t.Fatal("expected rejection of client-supplied owned:")
	}
}

func TestVerifyContactPricingRejectsForgedOwned(t *testing.T) {
	h := &Handler{}
	_, err := h.verifyContactPricingOwnership("PxActor", "owned:forged", "", "", "")
	if err == nil {
		t.Fatal("expected rejection of client-supplied owned:")
	}
}

func TestContactOwnershipUsesResolveAuthenticatedOwner(t *testing.T) {
	// Missing auth path: no mnemonic and no sig → error.
	h := &Handler{}
	if _, err := h.verifyContactPricingOwnership("PxA", "", "", "", ""); err == nil {
		t.Fatal("expected missing proof error")
	}
	if _, _, err := h.verifyContactRespondOwnership("PxA", "r1", "accepted", "", "", "", ""); err == nil {
		t.Fatal("expected missing proof error on respond")
	}
}

// TestRESTContactOwnershipMintSitesOnlySharedHelpers documents that REST contact
// ownership mints solely via protocol.MintOwnedProof / MintOwnedProofAfterResolve
// (issue #38 / TASK-009).
func TestRESTContactOwnershipMintSitesOnlySharedHelpers(t *testing.T) {
	srcPath := filepath.Join("handlers_contact_economy.go")
	if _, err := os.Stat(srcPath); err != nil {
		srcPath = filepath.Join("internal", "handlers", "handlers_contact_economy.go")
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, srcPath, nil, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var mintCalls []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "protocol" {
			return true
		}
		if sel.Sel.Name == "MintOwnedProof" || sel.Sel.Name == "MintOwnedProofAfterResolve" {
			mintCalls = append(mintCalls, sel.Sel.Name)
		}
		return true
	})
	if len(mintCalls) == 0 {
		t.Fatal("expected REST contact ownership to call shared mint helpers")
	}
	// No local "owned:"+ concatenation in this file.
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"owned:"+`) || strings.Contains(string(raw), "`owned:`+") {
		t.Fatal("REST contact ownership must not concatenate owned: locally")
	}
}
