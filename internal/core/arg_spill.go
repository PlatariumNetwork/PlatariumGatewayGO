package core

import (
	"os"
	"strings"
)

// Max inline CLI JSON before spilling to a temp file.
// Linux ARG_MAX is typically ~128KiB–2MiB for the whole argv; mempool snapshots
// alone can exceed that during high-throughput admission.
const cliJSONSpillBytes = 12 * 1024

// Flags whose values are JSON blobs and may grow with mempool / block size.
var spillJSONFlags = map[string]bool{
	"--mempool-txs": true,
	"--txs":         true,
	"--tx":          true,
	"--votes":       true,
	"--candidates":  true,
	"--message":     true,
	"--tx-hashes":   true,
	"--reads":       true,
	"--writes":      true,
}

// spillLargeCLIArgs rewrites oversized JSON flag values to @tempfile so
// platarium-cli does not fail with "argument list too long".
// Returns rewritten args and a cleanup that removes temp files.
func spillLargeCLIArgs(args []string) (out []string, cleanup func(), err error) {
	out = make([]string, 0, len(args))
	var paths []string
	cleanup = func() {
		for _, p := range paths {
			_ = os.Remove(p)
		}
	}

	for i := 0; i < len(args); i++ {
		flag := args[i]
		out = append(out, flag)
		if !spillJSONFlags[flag] {
			continue
		}
		if i+1 >= len(args) {
			break
		}
		val := args[i+1]
		i++
		if strings.HasPrefix(val, "@") || len(val) <= cliJSONSpillBytes {
			out = append(out, val)
			continue
		}
		f, createErr := os.CreateTemp("", "platarium-cli-arg-*.json")
		if createErr != nil {
			cleanup()
			return nil, func() {}, createErr
		}
		path := f.Name()
		if _, writeErr := f.WriteString(val); writeErr != nil {
			_ = f.Close()
			_ = os.Remove(path)
			cleanup()
			return nil, func() {}, writeErr
		}
		if closeErr := f.Close(); closeErr != nil {
			_ = os.Remove(path)
			cleanup()
			return nil, func() {}, closeErr
		}
		paths = append(paths, path)
		out = append(out, "@"+path)
	}
	return out, cleanup, nil
}
