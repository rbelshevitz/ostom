// Package memory provides bounded-process memory telemetry for recovery runs.
package memory

import (
	"runtime"
	"sync"
	"time"
)

// Snapshot contains Go runtime memory and, where available, the process
// working set. RuntimeBytes is the quantity used by Go's memory limit model:
// runtime.Sys minus runtime.HeapReleased.
type Snapshot struct {
	HeapAllocBytes    uint64 `json:"heap_alloc_bytes"`
	HeapInuseBytes    uint64 `json:"heap_inuse_bytes"`
	HeapReleasedBytes uint64 `json:"heap_released_bytes"`
	RuntimeBytes      uint64 `json:"runtime_bytes"`
	WorkingSetBytes   uint64 `json:"working_set_bytes,omitempty"`
}

// Report records the observed peak and final memory state of one operation.
type Report struct {
	Samples int      `json:"samples"`
	Peak    Snapshot `json:"peak"`
	Final   Snapshot `json:"final"`
}

// Monitor samples memory while a recovery operation is running. It is
// deliberately independent of recovery limits: telemetry must never change
// what data can be recovered.
type Monitor struct {
	stop    chan struct{}
	done    chan struct{}
	mu      sync.Mutex
	peak    Snapshot
	final   Snapshot
	samples int
}

func Start(interval time.Duration) *Monitor {
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	m := &Monitor{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(m.done)
		m.record()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.record()
			case <-m.stop:
				m.record()
				return
			}
		}
	}()
	return m
}

func (m *Monitor) record() {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	snapshot := Snapshot{
		HeapAllocBytes:    stats.HeapAlloc,
		HeapInuseBytes:    stats.HeapInuse,
		HeapReleasedBytes: stats.HeapReleased,
		RuntimeBytes:      stats.Sys - stats.HeapReleased,
		WorkingSetBytes:   processWorkingSet(),
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.final = snapshot
	m.samples++
	if snapshot.RuntimeBytes > m.peak.RuntimeBytes {
		m.peak = snapshot
	}
	if snapshot.WorkingSetBytes > m.peak.WorkingSetBytes {
		m.peak.WorkingSetBytes = snapshot.WorkingSetBytes
	}
}

func (m *Monitor) Stop() Report {
	if m == nil {
		return Report{}
	}
	close(m.stop)
	<-m.done
	m.mu.Lock()
	defer m.mu.Unlock()
	return Report{Samples: m.samples, Peak: m.peak, Final: m.final}
}
