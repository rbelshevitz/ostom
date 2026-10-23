package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ostom/internal/export/gopst"
	"ostom/internal/format"
)

func TestNativeExportTimeoutScalesAndCaps(t *testing.T) {
	if got, want := nativeExportTimeout(0), 10*time.Minute; got != want {
		t.Fatalf("zero-size timeout = %s, want %s", got, want)
	}
	if got, want := nativeExportTimeout(2<<30), 20*time.Minute; got != want {
		t.Fatalf("2 GiB timeout = %s, want %s", got, want)
	}
	if got, want := nativeExportTimeout(100<<30), 2*time.Hour; got != want {
		t.Fatalf("large-file timeout = %s, want %s", got, want)
	}
}

func TestBoundedOutputKeepsOnlyWorkerErrorTail(t *testing.T) {
	var output boundedOutput
	input := bytes.Repeat([]byte{'x'}, workerStderrLimit+17)
	if written, err := output.Write(input); err != nil || written != len(input) {
		t.Fatalf("write bounded output: written=%d err=%v", written, err)
	}
	if len(output.data) != workerStderrLimit || output.data[len(output.data)-1] != 'x' {
		t.Fatalf("bounded output size=%d", len(output.data))
	}
}

func TestIndexedExportTimeoutScalesAndCaps(t *testing.T) {
	if got, want := indexedExportTimeout(0), 10*time.Minute; got != want {
		t.Fatalf("zero-size indexed timeout = %s, want %s", got, want)
	}
	if got, want := indexedExportTimeout(2<<30), 20*time.Minute; got != want {
		t.Fatalf("2 GiB indexed timeout = %s, want %s", got, want)
	}
	if got, want := indexedExportTimeout(100<<30), 2*time.Hour; got != want {
		t.Fatalf("large-file indexed timeout = %s, want %s", got, want)
	}
	if indexedIdleTimeout <= 0 {
		t.Fatal("indexed idle timeout must be positive")
	}
}

func TestIndexedExportDirectoryIsolatedFromNativeRecovery(t *testing.T) {
	if got, want := indexedExportDirectory("export", true), filepath.Join("export", "Indexed"); got != want {
		t.Fatalf("recovery indexed directory = %q, want %q", got, want)
	}
	if got, want := indexedExportDirectory("export", false), "export"; got != want {
		t.Fatalf("ordinary indexed directory = %q, want %q", got, want)
	}
}

func TestIndexedFailureAlwaysUsesNativeFallback(t *testing.T) {
	for _, status := range []string{"timeout", "timeout_or_failed"} {
		if !indexedFailureUsesNativeFallback(status) {
			t.Fatalf("indexed status %q did not select native fallback", status)
		}
	}
}

func TestNativeFallbackDirectoryIsolatedFromPartialIndexedOutput(t *testing.T) {
	if got, want := nativeFallbackDirectory("export", false), filepath.Join("export", "Native Recovery", "Local Folders"); got != want {
		t.Fatalf("fallback directory = %q, want %q", got, want)
	}
	if got, want := nativeFallbackDirectory("export", true), filepath.Join("export", "Local Folders"); got != want {
		t.Fatalf("preselected native directory = %q, want %q", got, want)
	}
}

func TestExitCodeDistinguishesPartialAndInvalidRuns(t *testing.T) {
	if got := exitCode(report{Status: "completed"}); got != exitOK {
		t.Fatalf("completed exit code = %d, want %d", got, exitOK)
	}
	if got := exitCode(report{Status: "completed with errors", Errors: []string{"timeout"}}); got != exitPartialFailure {
		t.Fatalf("partial exit code = %d, want %d", got, exitPartialFailure)
	}
	if got := exitCode(report{Status: "failed", Errors: []string{"bad source"}}); got != exitInvalidSource {
		t.Fatalf("invalid-source exit code = %d, want %d", got, exitInvalidSource)
	}
}

func TestPrintProgressJSONIsOneLineEvent(t *testing.T) {
	previous := jsonProgress
	jsonProgress = true
	defer func() { jsonProgress = previous }()
	old := os.Stderr
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = write
	printProgress("scan", 2, 5)
	_ = write.Close()
	os.Stderr = old
	data, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"event":"progress"`)) || !bytes.Contains(data, []byte(`"done":2`)) || !bytes.Contains(data, []byte(`"total_known":true`)) || !bytes.Contains(data, []byte(`"unit":"items"`)) || bytes.Count(data, []byte("\n")) != 1 {
		t.Fatalf("unexpected progress event: %q", data)
	}
}

func TestSummarizeFindingsSeparatesPartialResults(t *testing.T) {
	r := report{
		Assessment:    format.IntegrityAssessment{Classification: "degraded"},
		Indexed:       &gopst.Report{MessagesPartial: 2, AttachmentsFailed: 1},
		IndexedStatus: "completed",
	}
	findings := summarizeFindings(r)
	if findings.Structure != "degraded" || findings.Messages != "partial" || findings.Attachments != "partial" {
		t.Fatalf("unexpected findings: %+v", findings)
	}
}

func TestFirstNonEmptyKeepsStableSourceFallback(t *testing.T) {
	if got := firstNonEmpty("", "source.ost"); got != "source.ost" {
		t.Fatalf("fallback source = %q", got)
	}
	if got := firstNonEmpty("session-source", "source.ost"); got != "session-source" {
		t.Fatalf("record source = %q", got)
	}
}
