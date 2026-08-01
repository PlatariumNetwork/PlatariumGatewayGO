package core

import (
	"os"
	"reflect"
	"testing"
)

func TestDagOrderingEnabledDefaultOn(t *testing.T) {
	os.Unsetenv("PLATARIUM_DAG_ORDERING")
	if !DagOrderingEnabled() {
		t.Fatal("expected DAG ordering on by default")
	}
	os.Setenv("PLATARIUM_DAG_ORDERING", "0")
	defer os.Unsetenv("PLATARIUM_DAG_ORDERING")
	if DagOrderingEnabled() {
		t.Fatal("expected off when PLATARIUM_DAG_ORDERING=0")
	}
}

func TestDagP2PEnabledDefaultOn(t *testing.T) {
	os.Unsetenv("PLATARIUM_DAG_P2P")
	if !DagP2PEnabled() {
		t.Fatal("expected DAG P2P on by default")
	}
	os.Setenv("PLATARIUM_DAG_P2P", "0")
	defer os.Unsetenv("PLATARIUM_DAG_P2P")
	if DagP2PEnabled() {
		t.Fatal("expected off when PLATARIUM_DAG_P2P=0")
	}
}

func TestPermuteByDigests(t *testing.T) {
	type item struct{ h string }
	items := []item{{"c"}, {"a"}, {"b"}}
	got := PermuteByDigests([]string{"a", "b", "c"}, items, func(it item) string { return it.h })
	want := []item{{"a"}, {"b"}, {"c"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestPermuteStringsAppendsUnknown(t *testing.T) {
	got := PermuteStrings([]string{"b"}, []string{"a", "b", "c"})
	want := []string{"b", "a", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestVertexFromMapRoundTrip(t *testing.T) {
	v := DagVertexWire{
		ID:        "abc",
		Round:     2,
		Author:    "n0",
		Parents:   []string{"p1"},
		TxDigests: []string{"t1", "t2"},
	}
	m := VertexToMap(v)
	got, ok := VertexFromMap(m)
	if !ok {
		t.Fatal("parse failed")
	}
	if got.ID != v.ID || got.Author != v.Author || got.Round != v.Round {
		t.Fatalf("got %#v", got)
	}
	if !reflect.DeepEqual(got.Parents, v.Parents) || !reflect.DeepEqual(got.TxDigests, v.TxDigests) {
		t.Fatalf("slices got %#v", got)
	}
}

func TestBuildDagCommittee(t *testing.T) {
	got := BuildDagCommittee("n2", []string{"n0", "n2", "n1", ""})
	want := []string{"n0", "n1", "n2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestDigestOverlapCount(t *testing.T) {
	if DigestOverlapCount([]string{"a", "b", "x"}, []string{"b", "a"}) != 2 {
		t.Fatal("expected 2")
	}
	if DigestOverlapCount([]string{"z"}, []string{"a", "b"}) != 0 {
		t.Fatal("expected 0")
	}
}
