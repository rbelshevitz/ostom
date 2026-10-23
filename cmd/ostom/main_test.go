package main

import (
	"bytes"
	"encoding/json"
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

func TestPrintProgressJSONIncludesReadyETA(t *testing.T) {
	previousProgress := jsonProgress
	previousEstimators := progressEstimators
	jsonProgress = true
	progressEstimators = map[string]*progressEstimator{
		"eta": {
			startedAt: time.Now().Add(-6 * time.Second),
			lastAt:    time.Now().Add(-1 * time.Second),
			lastDone:  9,
			rate:      1,
			samples:   3,
		},
	}
	defer func() {
		jsonProgress = previousProgress
		progressEstimators = previousEstimators
	}()

	old := os.Stderr
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = write
	printProgress("eta", 10, 20)
	_ = write.Close()
	os.Stderr = old
	data, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	var event struct {
		ETAKnown   bool `json:"eta_known"`
		ETASeconds int  `json:"eta_seconds"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(data), &event); err != nil {
		t.Fatalf("decode progress event %q: %v", data, err)
	}
	if !event.ETAKnown || event.ETASeconds <= 0 {
		t.Fatalf("progress ETA = known %v seconds %d, want a positive ETA", event.ETAKnown, event.ETASeconds)
	}
}

func TestEstimateProgressUsesWarmupAndSmoothedRate(t *testing.T) {
	previous := progressEstimators
	progressEstimators = make(map[string]*progressEstimator)
	defer func() { progressEstimators = previous }()

	base := time.Unix(100, 0)
	if eta, rate := estimateProgressAt("test", 0, 10, base); eta >= 0 || rate != 0 {
		t.Fatalf("initial estimate = eta %d rate %f, want unknown and zero", eta, rate)
	}
	for index := 1; index <= 3; index++ {
		if eta, _ := estimateProgressAt("test", index, 10, base.Add(time.Duration(index)*time.Second)); eta >= 0 {
			t.Fatalf("warmup estimate at %d seconds = %d, want unknown", index, eta)
		}
	}
	eta, rate := estimateProgressAt("test", 4, 10, base.Add(6*time.Second))
	if eta < 4 || eta > 8 {
		t.Fatalf("smoothed ETA = %d, want roughly 6 seconds (rate %f)", eta, rate)
	}
	if rate <= 0 {
		t.Fatalf("smoothed rate = %f, want positive", rate)
	}
}

func TestEstimateProgressHandlesUnknownTotalAndReset(t *testing.T) {
	previous := progressEstimators
	progressEstimators = make(map[string]*progressEstimator)
	defer func() { progressEstimators = previous }()

	base := time.Unix(200, 0)
	estimateProgressAt("unknown", 0, 0, base)
	for index := 1; index <= 4; index++ {
		estimateProgressAt("unknown", index, 0, base.Add(time.Duration(index+5)*time.Second))
	}
	if eta, rate := estimateProgressAt("unknown", 5, 0, base.Add(10*time.Second)); eta >= 0 || rate <= 0 {
		t.Fatalf("unknown-total estimate = eta %d rate %f, want unknown ETA and positive rate", eta, rate)
	}

	if eta, _ := estimateProgressAt("reset", 0, 10, base); eta >= 0 {
		t.Fatalf("reset initial ETA = %d, want unknown", eta)
	}
	estimateProgressAt("reset", 5, 10, base.Add(6*time.Second))
	if eta, _ := estimateProgressAt("reset", 1, 10, base.Add(7*time.Second)); eta >= 0 {
		t.Fatalf("backward-progress ETA = %d, want warmup after reset", eta)
	}
}

func TestParseIndexedEstimateRejectsUnusableWorkerResponses(t *testing.T) {
	tests := []struct {
		name string
		data string
		want int
		ok   bool
	}{
		{name: "known", data: `{"messages":2178,"known":true}`, want: 2178, ok: true},
		{name: "unknown", data: `{"messages":2178,"known":false}`},
		{name: "zero", data: `{"messages":0,"known":true}`},
		{name: "malformed", data: `{not-json}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got, ok := parseIndexedEstimate([]byte(test.data)); got != test.want || ok != test.ok {
				t.Fatalf("parseIndexedEstimate = %d, %v; want %d, %v", got, ok, test.want, test.ok)
			}
		})
	}
}

func TestFormatProgressDuration(t *testing.T) {
	for _, test := range []struct {
		seconds int
		want    string
	}{
		{seconds: 7, want: "7s"},
		{seconds: 65, want: "1m05s"},
		{seconds: 3660, want: "1h01m"},
	} {
		if got := formatProgressDuration(test.seconds); got != test.want {
			t.Fatalf("formatProgressDuration(%d) = %q, want %q", test.seconds, got, test.want)
		}
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
