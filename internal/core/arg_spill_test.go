package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpillLargeCLIArgsKeepsSmallInline(t *testing.T) {
	args := []string{"mempool-admit", "--state-file", "s.json", "--tx", `{"a":1}`, "--mempool-txs", `[]`}
	out, cleanup, err := spillLargeCLIArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if strings.Join(out, " ") != strings.Join(args, " ") {
		t.Fatalf("unexpected rewrite: %#v", out)
	}
}

func TestSpillLargeCLIArgsWritesAtFile(t *testing.T) {
	big := strings.Repeat("x", cliJSONSpillBytes+1)
	args := []string{"mempool-admit", "--mempool-txs", big}
	out, cleanup, err := spillLargeCLIArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(out) != 3 || out[0] != "mempool-admit" || out[1] != "--mempool-txs" {
		t.Fatalf("bad out: %#v", out)
	}
	if !strings.HasPrefix(out[2], "@") {
		t.Fatalf("expected @file, got %q", out[2])
	}
	path := strings.TrimPrefix(out[2], "@")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != big {
		t.Fatalf("file content mismatch len=%d", len(data))
	}
}

func TestSpillUsesAllowlistedDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PLATARIUM_CLI_SPILL_DIR", dir)
	big := strings.Repeat("x", cliJSONSpillBytes+10)
	out, cleanup, err := spillLargeCLIArgs([]string{"state-apply-tx", "--tx", big})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(out) < 3 || !strings.HasPrefix(out[2], "@") {
		t.Fatalf("expected @spill path, got %#v", out)
	}
	path := strings.TrimPrefix(out[2], "@")
	if !strings.HasPrefix(path, dir+string(os.PathSeparator)) && filepath.Dir(path) != dir {
		t.Fatalf("spill path %q not under %q", path, dir)
	}
}
