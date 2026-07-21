package handlers

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	metricsWindowMs   = 10 * 60 * 1000 // 10 min rolling window for TPS peaks
	metricsMaxSamples = 4096
)

type exploreMetrics struct {
	mu sync.Mutex

	txReceived uint64
	txAccepted uint64
	txRejected uint64
	l1Rounds   uint64
	l2Rounds   uint64

	admitAt   map[string]int64 // tx hash -> unix ms
	confirmed []int64          // confirmation timestamps (ms)
	admitted  []int64          // admit timestamps (ms)

	confirmMsSamples []int64
	confirmMsTotal   int64
	confirmMsCount   uint64

	peakConfirmTps1s  float64
	peakConfirmTps10s float64
	peakIngressTps1s  float64
	peakIngressTps10s float64

	lastCPUUserTicks int64
	lastCPUTime      time.Time
	lastCPUPct       float64
}

func newExploreMetrics() *exploreMetrics {
	return &exploreMetrics{
		admitAt: make(map[string]int64),
	}
}

func (m *exploreMetrics) recordTxReceived() {
	m.mu.Lock()
	m.txReceived++
	m.mu.Unlock()
}

func (m *exploreMetrics) recordTxAccepted(hash string) {
	now := time.Now().UnixMilli()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.txAccepted++
	if hash != "" {
		m.admitAt[hash] = now
	}
	m.admitted = append(m.admitted, now)
	m.trimTimesLocked(&m.admitted, now)
	m.updateIngressPeaksLocked(now)
}

func (m *exploreMetrics) recordTxRejected() {
	m.mu.Lock()
	m.txRejected++
	m.mu.Unlock()
}

func (m *exploreMetrics) recordL1Round() {
	m.mu.Lock()
	m.l1Rounds++
	m.mu.Unlock()
}

func (m *exploreMetrics) recordL2Confirm(hashes []string) {
	now := time.Now().UnixMilli()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.l2Rounds++
	for _, h := range hashes {
		if h == "" {
			continue
		}
		if admitAt, ok := m.admitAt[h]; ok && admitAt > 0 {
			delta := now - admitAt
			if delta >= 0 {
				m.confirmMsSamples = append(m.confirmMsSamples, delta)
				if len(m.confirmMsSamples) > 512 {
					m.confirmMsSamples = m.confirmMsSamples[len(m.confirmMsSamples)-512:]
				}
				m.confirmMsTotal += delta
				m.confirmMsCount++
			}
			delete(m.admitAt, h)
		}
		m.confirmed = append(m.confirmed, now)
	}
	m.trimTimesLocked(&m.confirmed, now)
	m.updateConfirmPeaksLocked(now)
}

func (m *exploreMetrics) trimTimesLocked(slice *[]int64, now int64) {
	cutoff := now - metricsWindowMs
	i := 0
	for _, t := range *slice {
		if t >= cutoff {
			(*slice)[i] = t
			i++
		}
	}
	if i > metricsMaxSamples {
		i = metricsMaxSamples
	}
	*slice = (*slice)[:i]
}

func countInWindow(times []int64, now, windowMs int64) int {
	cutoff := now - windowMs
	n := 0
	for i := len(times) - 1; i >= 0; i-- {
		if times[i] < cutoff {
			break
		}
		n++
	}
	return n
}

func (m *exploreMetrics) updateConfirmPeaksLocked(now int64) {
	if tps := float64(countInWindow(m.confirmed, now, 1000)); tps > m.peakConfirmTps1s {
		m.peakConfirmTps1s = tps
	}
	if tps := float64(countInWindow(m.confirmed, now, 10_000)) / 10.0; tps > m.peakConfirmTps10s {
		m.peakConfirmTps10s = tps
	}
}

func (m *exploreMetrics) updateIngressPeaksLocked(now int64) {
	if tps := float64(countInWindow(m.admitted, now, 1000)); tps > m.peakIngressTps1s {
		m.peakIngressTps1s = tps
	}
	if tps := float64(countInWindow(m.admitted, now, 10_000)) / 10.0; tps > m.peakIngressTps10s {
		m.peakIngressTps10s = tps
	}
}

func (m *exploreMetrics) snapshot() map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UnixMilli()
	currentConfirmTps := float64(countInWindow(m.confirmed, now, 1000))
	currentIngressTps := float64(countInWindow(m.admitted, now, 1000))
	currentConfirmTps10 := float64(countInWindow(m.confirmed, now, 10_000)) / 10.0
	currentIngressTps10 := float64(countInWindow(m.admitted, now, 10_000)) / 10.0

	if currentConfirmTps > m.peakConfirmTps1s {
		m.peakConfirmTps1s = currentConfirmTps
	}
	if currentConfirmTps10 > m.peakConfirmTps10s {
		m.peakConfirmTps10s = currentConfirmTps10
	}
	if currentIngressTps > m.peakIngressTps1s {
		m.peakIngressTps1s = currentIngressTps
	}
	if currentIngressTps10 > m.peakIngressTps10s {
		m.peakIngressTps10s = currentIngressTps10
	}

	avgConfirmMs := float64(0)
	if m.confirmMsCount > 0 {
		avgConfirmMs = float64(m.confirmMsTotal) / float64(m.confirmMsCount)
	}

	memAlloc, memSys, cpuPct := readProcessResources(&m.lastCPUUserTicks, &m.lastCPUTime, &m.lastCPUPct)

	return map[string]interface{}{
		"txReceived":            m.txReceived,
		"txAccepted":            m.txAccepted,
		"txRejected":            m.txRejected,
		"l1Rounds":              m.l1Rounds,
		"l2Rounds":              m.l2Rounds,
		"currentTps":            round2(currentIngressTps),
		"currentConfirmTps":     round2(currentConfirmTps),
		"peakTps1s":             round2(m.peakIngressTps1s),
		"peakTps10s":            round2(m.peakIngressTps10s),
		"peakConfirmTps1s":      round2(m.peakConfirmTps1s),
		"peakConfirmTps10s":     round2(m.peakConfirmTps10s),
		"avgConfirmationTimeMs": round0(avgConfirmMs),
		"memAllocBytes":         memAlloc,
		"memSysBytes":           memSys,
		"cpuUsagePct":           round2(cpuPct),
	}
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func round0(v float64) float64 {
	return float64(int64(v + 0.5))
}

func readProcessResources(lastTicks *int64, lastAt *time.Time, lastPct *float64) (alloc uint64, sys uint64, cpuPct float64) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	alloc = ms.Alloc
	sys = ms.Sys

	ticks, ok := linuxProcessCPUTicks()
	now := time.Now()
	if ok && !lastAt.IsZero() {
		elapsed := now.Sub(*lastAt).Seconds()
		if elapsed > 0.05 {
			deltaTicks := ticks - *lastTicks
			if deltaTicks >= 0 {
				// USER_HZ is typically 100 on Linux.
				cpuPct = (float64(deltaTicks) / 100.0) / elapsed * 100.0
				if cpuPct < 0 {
					cpuPct = 0
				}
				if cpuPct > 100*float64(runtime.NumCPU()) {
					cpuPct = 100 * float64(runtime.NumCPU())
				}
				*lastPct = cpuPct
			}
		}
	}
	if ok {
		*lastTicks = ticks
		*lastAt = now
	}
	if cpuPct == 0 && *lastPct > 0 {
		cpuPct = *lastPct
	}
	return alloc, sys, cpuPct
}

func linuxProcessCPUTicks() (int64, bool) {
	f, err := os.Open("/proc/self/stat")
	if err != nil {
		return 0, false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return 0, false
	}
	line := scanner.Text()
	// comm may contain spaces inside parens — find closing paren.
	closeIdx := strings.LastIndex(line, ")")
	if closeIdx < 0 || closeIdx+2 >= len(line) {
		return 0, false
	}
	fields := strings.Fields(line[closeIdx+2:])
	if len(fields) < 12 {
		return 0, false
	}
	utime, err1 := strconv.ParseInt(fields[11], 10, 64)
	stime, err2 := strconv.ParseInt(fields[12], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return utime + stime, true
}
