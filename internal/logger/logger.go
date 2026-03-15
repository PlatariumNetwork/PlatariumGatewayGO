// Package logger writes diagnostic and error logs to log/ folder for debugging.
package logger

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	file    *os.File
	mu      sync.Mutex
	prefix  = "[gateway]"
	nodeID  string // short node id for multi-node logs (set from main)
)

// SetNodeID sets a short node identifier (e.g. first 6 chars) so log lines can be filtered per node.
func SetNodeID(id string) {
	if len(id) > 8 {
		id = id[:6] + "…"
	}
	mu.Lock()
	nodeID = id
	mu.Unlock()
}

func init() {
	dir := "log"
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("%s failed to create log dir %s: %v", prefix, dir, err)
		return
	}
	fpath := filepath.Join(dir, "gateway.log")
	f, err := os.OpenFile(fpath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("%s failed to open log file %s: %v", prefix, fpath, err)
		return
	}
	file = f
	log.Printf("%s logging to %s", prefix, fpath)
}

func write(level, format string, args ...interface{}) {
	mu.Lock()
	ts := time.Now().Format("2006-01-02 15:04:05.000")
	msg := fmt.Sprintf(format, args...)
	nodePrefix := ""
	if nodeID != "" {
		nodePrefix = " " + nodeID + " "
	}
	line := fmt.Sprintf("%s %s%s%s\n", ts, level, nodePrefix, msg)
	if file != nil {
		_, _ = file.WriteString(line)
	}
	mu.Unlock()
	switch level {
	case "ERROR":
		log.Printf("[ERROR] "+format, args...)
	case "WARN":
		log.Printf("[WARN] "+format, args...)
	default:
		log.Printf(format, args...)
	}
}

// Error writes an error line to log file and stderr.
func Error(format string, args ...interface{}) { write("ERROR", format, args...) }

// Errorf is alias for Error.
func Errorf(format string, args ...interface{}) { write("ERROR", format, args...) }

// Warn writes a warning line to log file and stderr.
func Warn(format string, args ...interface{}) { write("WARN", format, args...) }

// Warnf is alias for Warn.
func Warnf(format string, args ...interface{}) { write("WARN", format, args...) }

// Info writes an info line to log file and stdout.
func Info(format string, args ...interface{}) { write("INFO", format, args...) }

// Infof is alias for Info.
func Infof(format string, args ...interface{}) { write("INFO", format, args...) }

// Close closes the log file (e.g. on shutdown).
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if file != nil {
		_ = file.Close()
		file = nil
	}
}
