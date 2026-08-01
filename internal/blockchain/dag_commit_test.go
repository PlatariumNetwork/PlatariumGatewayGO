package blockchain

import (
	"reflect"
	"testing"
)

func TestDagCommitDigestsCache(t *testing.T) {
	bc := NewBlockchain()
	bc.SetDagCommitDigests("anchor1", []string{"h2", "h1"})
	a, d := bc.DagCommitDigests()
	if a != "anchor1" || !reflect.DeepEqual(d, []string{"h2", "h1"}) {
		t.Fatalf("got %s %#v", a, d)
	}
	bc.ClearDagCommitDigests()
	a, d = bc.DagCommitDigests()
	if a != "" || len(d) != 0 {
		t.Fatalf("expected clear, got %s %#v", a, d)
	}
}
