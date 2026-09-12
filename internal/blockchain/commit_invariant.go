package blockchain

import "fmt"

// CheckCommitHeightHashInvariant asserts committed block B pairs with state after B (#58).
// height/hash (and optional state root) must agree across explorer tip and Rocks tip layers.
func CheckCommitHeightHashInvariant(
	explorerHeight uint64, explorerHash string,
	rocksHeight uint64, rocksHash string,
	explorerStateRoot, rocksStateRoot string,
) error {
	if explorerHeight != rocksHeight {
		return fmt.Errorf("commit invariant height diverge: explorer=%d rocks=%d", explorerHeight, rocksHeight)
	}
	if explorerHash == "" || rocksHash == "" {
		return fmt.Errorf("commit invariant missing hash: explorer=%q rocks=%q", explorerHash, rocksHash)
	}
	if explorerHash != rocksHash {
		return fmt.Errorf("commit invariant hash diverge: explorer=%s rocks=%s", explorerHash, rocksHash)
	}
	if explorerStateRoot != "" && rocksStateRoot != "" && explorerStateRoot != rocksStateRoot {
		return fmt.Errorf("commit invariant state_root diverge: explorer=%s rocks=%s", explorerStateRoot, rocksStateRoot)
	}
	return nil
}
