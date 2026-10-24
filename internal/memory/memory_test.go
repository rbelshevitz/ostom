package memory

import (
	"testing"
	"time"
)

func TestMonitorCapturesRuntimeMemory(t *testing.T) {
	monitor := Start(time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	report := monitor.Stop()
	if report.Samples == 0 || report.Final.RuntimeBytes == 0 || report.Peak.RuntimeBytes == 0 {
		t.Fatalf("incomplete memory report: %+v", report)
	}
	if report.Peak.RuntimeBytes < report.Final.RuntimeBytes {
		t.Fatalf("peak runtime memory is inconsistent: %+v", report)
	}
}
