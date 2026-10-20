// ostom is a read-only OST/PST recovery exporter.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"ostom/internal/export/gopst"
	"ostom/internal/export/native"
	"ostom/internal/format"
	"ostom/internal/recovery/manifest"
	"ostom/internal/storage"
)

var jsonProgress bool

type report struct {
	Source        string                     `json:"source"`
	SourceBytes   int64                      `json:"source_bytes"`
	Header        format.Header              `json:"header"`
	IndexRoots    format.IndexRootScan       `json:"index_roots"`
	Assessment    format.IntegrityAssessment `json:"assessment"`
	Findings      Findings                   `json:"findings"`
	Storage       *StorageAssessment         `json:"storage,omitempty"`
	RecoveryScan  bool                       `json:"recovery_scan"`
	CheckOnly     bool                       `json:"check_only"`
	RecoveryRun   string                     `json:"recovery_run"`
	Indexed       *gopst.Report              `json:"indexed_export,omitempty"`
	IndexedStatus string                     `json:"indexed_status,omitempty"`
	Native        *native.PipelineReport     `json:"native_recovery,omitempty"`
	Analysis      *AnalysisReport            `json:"analysis,omitempty"`
	Status        string                     `json:"status"`
	Errors        []string                   `json:"errors,omitempty"`
	Warnings      []string                   `json:"warnings,omitempty"`
	Elapsed       string                     `json:"elapsed"`
}

type AnalysisReport struct {
	Mode                  string `json:"mode"`
	Status                string `json:"status"`
	Candidates            int    `json:"candidates"`
	Attachments           int    `json:"attachments"`
	IncompleteMessages    int    `json:"incomplete_messages"`
	IncompleteAttachments int    `json:"incomplete_attachments"`
	Rejected              int    `json:"rejected"`
	Errors                int    `json:"errors"`
	LimitReached          bool   `json:"limit_reached"`
}

// Findings is deliberately evidence-based wording for a GUI. It describes
// what was checked, not what the source file must contain.
type Findings struct {
	Structure   string `json:"structure"`
	Messages    string `json:"messages"`
	Attachments string `json:"attachments"`
}

type StorageAssessment struct {
	Path             string `json:"path"`
	AvailableBytes   uint64 `json:"available_bytes"`
	TotalBytes       uint64 `json:"total_bytes"`
	RequiredEstimate uint64 `json:"required_estimate_bytes"`
	Warning          string `json:"warning,omitempty"`
}

const (
	exitOK             = 0
	exitFailed         = 1
	exitUsage          = 2
	exitInvalidSource  = 3
	exitPartialFailure = 4
)

func main() {
	jsonOutput := flag.Bool("json", false, "write the report as JSON to stdout")
	exportDir := flag.String("export-dir", "", "directory for Thunderbird Local Folders export")
	outputDir := flag.String("output-dir", "", "directory for ostom-report.json")
	recoveryScan := flag.Bool("recovery-scan", false, "force bounded native recovery")
	checkOnly := flag.Bool("check", false, "inspect structure without exporting messages or attachments")
	analyzeOnly := flag.Bool("analyze", false, "bounded read-only content analysis without export")
	quiet := flag.Bool("quiet", false, "suppress progress on stderr")
	progressJSON := flag.Bool("progress-json", false, "write progress events as JSON lines to stderr")
	fullEML := flag.Bool("eml", false, "also write one EML file for every indexed message")
	indexedWorker := flag.Bool("indexed-worker", false, "internal indexed export worker")
	indexedRecovery := flag.Bool("indexed-recovery", false, "internal indexed worker recovery mode")
	nativeWorker := flag.Bool("native-worker", false, "internal native recovery worker")
	nativeAnalyzeWorker := flag.Bool("native-analyze-worker", false, "internal native content-analysis worker")
	flag.Parse()
	jsonProgress = *progressJSON
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "Usage: ostom [--json] [--check|--analyze] [--progress-json] [--output-dir DIR] [--export-dir DIR] [--recovery-scan] [--quiet] file.ost")
		os.Exit(exitUsage)
	}
	if *recoveryScan && *exportDir == "" {
		fmt.Fprintln(os.Stderr, "--recovery-scan requires --export-dir")
		os.Exit(exitUsage)
	}
	if *checkOnly && *recoveryScan {
		fmt.Fprintln(os.Stderr, "--check cannot be combined with --recovery-scan")
		os.Exit(exitUsage)
	}
	if *analyzeOnly && (*checkOnly || *recoveryScan || *exportDir != "") {
		fmt.Fprintln(os.Stderr, "--analyze cannot be combined with --check, --recovery-scan, or --export-dir")
		os.Exit(exitUsage)
	}
	if *indexedWorker {
		runIndexedWorker(flag.Arg(0), *exportDir, *indexedRecovery, *quiet, *fullEML)
		return
	}
	if *nativeWorker {
		runNativeWorker(flag.Arg(0), *exportDir, *quiet, *fullEML)
		return
	}
	if *nativeAnalyzeWorker {
		runNativeAnalyzeWorker(flag.Arg(0), *quiet)
		return
	}

	started := time.Now()
	source := flag.Arg(0)
	info, err := os.Stat(source)
	if err != nil {
		fatalf("open source: %v", err)
	}
	h := format.ReadHeader(source)
	if h.OK {
		h.Allocation = format.ScanAllocationPages(source, h, info.Size())
	}
	roots := format.ScanIndexRoots(source, h, info.Size())
	assessment := format.AssessFile(h, roots, info.Size())
	r := report{Source: source, SourceBytes: info.Size(), Header: h, IndexRoots: roots, Assessment: assessment, RecoveryScan: *recoveryScan, CheckOnly: *checkOnly, RecoveryRun: "not_run", Status: "completed"}
	if destination := existingDestination(*exportDir, *outputDir); destination != "" {
		if space, spaceErr := storage.Query(destination); spaceErr == nil {
			required := uint64(info.Size()) + uint64(info.Size())/5
			storageAssessment := &StorageAssessment{Path: destination, AvailableBytes: space.Available, TotalBytes: space.Total, RequiredEstimate: required}
			if space.Available < required {
				storageAssessment.Warning = "available space is below the conservative export estimate"
			}
			r.Storage = storageAssessment
			if storageAssessment.Warning != "" {
				r.Warnings = append(r.Warnings, storageAssessment.Warning)
			}
		}
	}
	if !h.OK {
		r.Status = "failed"
		r.Errors = append(r.Errors, "source is not a recognized PFF/OST/PST file")
		finish(r, *jsonOutput, *outputDir, *exportDir)
		os.Exit(exitCode(r))
	}

	if *analyzeOnly {
		analysis, analyzeErr := runNativeAnalysisWithTimeout(source, info.Size(), *quiet)
		r.Analysis = &analysis
		if analyzeErr != nil {
			r.Errors = append(r.Errors, "content analysis: "+analyzeErr.Error())
		}
	} else if *exportDir != "" && !*checkOnly {
		progress := func(done, total int) {
			if !*quiet {
				printProgress("standard mailbox export", done, total)
			}
		}
		runRecovery := *recoveryScan || assessment.DeepRecovery != "not_needed"
		if runRecovery {
			if *recoveryScan {
				r.RecoveryRun = "explicit"
			} else {
				r.RecoveryRun = "automatic"
			}
			nativeReport, nativeErr := runNativeExportWithTimeout(source, filepath.Join(*exportDir, "Local Folders"), info.Size(), *quiet, *fullEML)
			r.Native = &nativeReport
			if nativeErr != nil {
				r.Errors = append(r.Errors, "recovery scan: "+nativeErr.Error())
			}
			// Native recovery is the bounded primary path. Persist its manifest
			// before the optional indexed backend gets a chance to stall.
			manifestDir := *outputDir
			if manifestDir == "" {
				manifestDir = *exportDir
			}
			if err := writeRecoveryManifest(manifestDir, source, r); err != nil {
				r.Errors = append(r.Errors, "recovery manifest: "+err.Error())
			}
		}
		// Deep recovery is native-only. go-pst remains an optional ordinary
		// indexed backend; its recovery-mode heuristics are not safe to run on
		// damaged 4 KiB OSTs and have no bounded cancellation path.
		indexed, exportErr := runIndexedExportWithTimeout(source, *exportDir, false, progress, *quiet, *fullEML)
		r.Indexed = &indexed
		if exportErr != nil {
			r.IndexedStatus = "timeout_or_failed"
			if strings.Contains(exportErr.Error(), "exceeded") {
				r.IndexedStatus = "timeout"
			}
			r.Warnings = append(r.Warnings, "indexed export: "+exportErr.Error())
			// A slow indexed traversal must not leave the user with no export.
			// Switch to the bounded native path automatically after the watchdog
			// fires; its output is kept in a separate recovery mailbox tree.
			if r.IndexedStatus == "timeout" && r.Native == nil {
				nativeReport, nativeErr := runNativeExportWithTimeout(source, filepath.Join(*exportDir, "Local Folders"), info.Size(), *quiet, *fullEML)
				r.Native = &nativeReport
				if nativeErr != nil {
					r.Errors = append(r.Errors, "recovery fallback: "+nativeErr.Error())
				}
			}
		} else {
			r.IndexedStatus = "completed"
		}
	} else if *checkOnly {
		r.Warnings = append(r.Warnings, "check-only mode: export was not requested")
	} else {
		r.Warnings = append(r.Warnings, "no export directory supplied; inspection only")
	}
	if *recoveryScan {
		manifestDir := *outputDir
		if manifestDir == "" {
			manifestDir = *exportDir
		}
		if err := writeRecoveryManifest(manifestDir, source, r); err != nil {
			r.Errors = append(r.Errors, "recovery manifest: "+err.Error())
		}
	}
	r.Findings = summarizeFindings(r)
	r.Elapsed = time.Since(started).Round(time.Millisecond).String()
	if len(r.Errors) > 0 {
		r.Status = "completed with errors"
	}
	finish(r, *jsonOutput, *outputDir, *exportDir)
	if code := exitCode(r); code != exitOK {
		os.Exit(code)
	}
}

func summarizeFindings(r report) Findings {
	findings := Findings{Structure: r.Assessment.Classification, Messages: "not_checked", Attachments: "not_checked"}
	if r.Analysis != nil {
		if r.Analysis.Candidates > 0 {
			findings.Messages = "complete"
			if r.Analysis.IncompleteMessages > 0 || r.Analysis.LimitReached {
				findings.Messages = "partial"
			}
			findings.Attachments = "complete"
			if r.Analysis.IncompleteAttachments > 0 {
				findings.Attachments = "partial"
			}
		} else if r.Analysis.Status != "completed" || r.Analysis.Errors > 0 {
			findings.Messages = "failed"
			findings.Attachments = "not_checked"
		}
		return findings
	}
	if r.CheckOnly || r.ExportDirAbsent() {
		return findings
	}
	if r.Indexed != nil {
		if r.IndexedStatus == "timeout" {
			if r.Native != nil && r.Native.Export.Written > 0 {
				findings.Messages = "complete"
				if r.Native.Export.Incomplete > 0 {
					findings.Messages = "partial"
				}
				findings.Attachments = "complete"
				if r.Native.Export.IncompleteAttachments > 0 {
					findings.Attachments = "partial"
				}
			} else {
				findings.Messages = "timeout"
				findings.Attachments = "not_checked"
			}
		} else if r.IndexedStatus == "completed" {
			findings.Messages = "complete"
			if r.Indexed.MessagesPartial > 0 || r.Indexed.SkippedMessages > 0 {
				findings.Messages = "partial"
			}
			findings.Attachments = "complete"
			if r.Indexed.AttachmentsFailed > 0 {
				findings.Attachments = "partial"
			}
		}
	}
	if r.Native != nil {
		if r.Native.Export.Failed > 0 || r.Native.Export.EMLFailed > 0 || r.Native.Export.Incomplete > 0 {
			findings.Messages = worseFinding(findings.Messages, "partial")
		} else if r.Native.Export.Written > 0 {
			findings.Messages = worseFinding(findings.Messages, "complete")
		}
		if r.Native.Export.IncompleteAttachments > 0 {
			findings.Attachments = worseFinding(findings.Attachments, "partial")
		}
	}
	return findings
}

func (r report) ExportDirAbsent() bool { return r.Indexed == nil && r.Native == nil }

func worseFinding(current, candidate string) string {
	rank := map[string]int{"not_checked": 0, "complete": 1, "partial": 2, "timeout": 3}
	if rank[candidate] > rank[current] {
		return candidate
	}
	return current
}

func exitCode(r report) int {
	if r.Status == "failed" {
		return exitInvalidSource
	}
	if len(r.Errors) > 0 {
		return exitPartialFailure
	}
	return exitOK
}

func existingDestination(exportDir, outputDir string) string {
	path := exportDir
	if path == "" {
		path = outputDir
	}
	if path == "" {
		return ""
	}
	for {
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return ""
		}
		path = parent
	}
}

func writeRecoveryManifest(outputDir, source string, r report) error {
	records := make([]manifest.Record, 0)
	if r.Indexed != nil {
		for _, status := range r.Indexed.MessageStatus {
			records = append(records, manifest.Record{
				Kind: "message", Source: "indexed", Identity: fmt.Sprintf("indexed-message-%08d", status.Index),
				Route: "indexed", FolderPath: status.Folder, Status: status.Outcome,
				Errors: nonEmptyErrors(status.Error),
			})
		}
		if len(r.Indexed.MessageStatus) == 0 && len(r.Errors) > 0 {
			records = append(records, manifest.Record{
				Kind: "message", Source: "indexed", Identity: "indexed-export", Route: "indexed",
				Status: "skipped", Errors: []string{"indexed export did not produce per-message records"},
			})
		}
	}
	if r.Native != nil {
		records = append(records, r.Native.Export.ManifestRecords...)
	}
	return manifest.Write(outputDir, source, records)
}

func nonEmptyErrors(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}

const indexedExportTimeout = 60 * time.Second

func defaultNativeLimits(writeEML bool) native.Limits {
	return native.Limits{MaxNodes: 100000, MaxBlocks: 1024, MaxBytes: 64 << 20, MaxRecords: 4096, MaxMessages: 100000, MaxBodyBytes: 4 << 20, MaxAttachmentBytes: 32 << 20, MaxErrorSamples: 256, WriteEML: writeEML}
}

func nativeExportTimeout(fileSize int64) time.Duration {
	// Large OST/PST files get more wall time, while a small damaged file still
	// cannot hold the parent process indefinitely. The worker remains bounded
	// by node/block/byte limits independently of this watchdog.
	timeout := 10 * time.Minute
	if fileSize > 0 {
		timeout += time.Duration(fileSize/(1<<30)) * 5 * time.Minute
	}
	if timeout > 2*time.Hour {
		return 2 * time.Hour
	}
	return timeout
}

func runNativeExportWithTimeout(source, output string, fileSize int64, quiet, fullEML bool) (native.PipelineReport, error) {
	result := native.PipelineReport{}
	executable, err := os.Executable()
	if err != nil {
		return result, fmt.Errorf("locate native worker: %w", err)
	}
	args := []string{"-native-worker", "-export-dir", output}
	if jsonProgress {
		args = append(args, "-progress-json")
	}
	if quiet {
		args = append(args, "-quiet")
	}
	if fullEML {
		args = append(args, "-eml")
	}
	args = append(args, source)
	cmd := exec.Command(executable, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	if err := cmd.Start(); err != nil {
		return result, fmt.Errorf("start native worker: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(nativeExportTimeout(fileSize))
	defer timer.Stop()
	select {
	case err := <-done:
		decodeErr := json.Unmarshal(stdout.Bytes(), &result)
		if err != nil {
			return result, fmt.Errorf("native worker: %w: %s", err, trimWorkerError(stderr.String()))
		}
		if decodeErr != nil {
			return result, fmt.Errorf("decode native worker report: %w", decodeErr)
		}
		return result, nil
	case <-timer.C:
		_ = cmd.Process.Kill()
		<-done
		return result, fmt.Errorf("native worker exceeded %s", nativeExportTimeout(fileSize))
	}
}

func runNativeAnalysisWithTimeout(source string, fileSize int64, quiet bool) (AnalysisReport, error) {
	result := AnalysisReport{}
	executable, err := os.Executable()
	if err != nil {
		return result, fmt.Errorf("locate native analysis worker: %w", err)
	}
	args := []string{"-native-analyze-worker"}
	if jsonProgress {
		args = append(args, "-progress-json")
	}
	if quiet {
		args = append(args, "-quiet")
	}
	args = append(args, source)
	cmd := exec.Command(executable, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	if err := cmd.Start(); err != nil {
		return result, fmt.Errorf("start native analysis worker: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(nativeExportTimeout(fileSize))
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil {
			return result, fmt.Errorf("native analysis worker: %w: %s", err, trimWorkerError(stderr.String()))
		}
		if decodeErr := json.Unmarshal(stdout.Bytes(), &result); decodeErr != nil {
			return result, fmt.Errorf("decode native analysis report: %w", decodeErr)
		}
		return result, nil
	case <-timer.C:
		_ = cmd.Process.Kill()
		<-done
		return result, fmt.Errorf("native analysis exceeded %s", nativeExportTimeout(fileSize))
	}
}

func runIndexedExportWithTimeout(source, output string, recovery bool, progress func(done, total int), quiet, fullEML bool) (gopst.Report, error) {
	result := gopst.Report{Directory: output, RecoveryMode: recovery}
	executable, err := os.Executable()
	if err != nil {
		return result, fmt.Errorf("locate indexed worker: %w", err)
	}
	args := []string{"-indexed-worker", "-export-dir", output}
	if jsonProgress {
		args = append(args, "-progress-json")
	}
	if quiet {
		args = append(args, "-quiet")
	}
	if fullEML {
		args = append(args, "-eml")
	}
	if recovery {
		args = append(args, "-indexed-recovery")
	}
	args = append(args, source)
	cmd := exec.Command(executable, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	if err := cmd.Start(); err != nil {
		return result, fmt.Errorf("start indexed worker: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(indexedExportTimeout)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil {
			return result, fmt.Errorf("indexed worker: %w: %s", err, trimWorkerError(stderr.String()))
		}
	case <-timer.C:
		_ = cmd.Process.Kill()
		<-done
		if !quiet {
			fmt.Fprintln(os.Stderr)
		}
		return result, fmt.Errorf("indexed worker exceeded %s", indexedExportTimeout)
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return result, fmt.Errorf("decode indexed worker report: %w", err)
	}
	if progress != nil {
		progress(result.MessagesAttempted, result.MessagesExpected)
	}
	return result, nil
}

func runIndexedWorker(source, output string, recovery, quiet, fullEML bool) {
	var progress func(done, total int)
	if !quiet {
		progress = func(done, total int) { printProgress("standard mailbox export", done, total) }
	}
	result, err := safeIndexedExport(source, output, recovery, progress, fullEML)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "encode indexed worker report: %v\n", err)
		os.Exit(1)
	}
}

func runNativeWorker(source, output string, quiet, fullEML bool) {
	info, err := os.Stat(source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "native worker: stat source: %v\n", err)
		os.Exit(1)
	}
	h := format.ReadHeader(source)
	if !h.OK {
		fmt.Fprintln(os.Stderr, "native worker: source is not a recognized PFF/OST/PST file")
		os.Exit(1)
	}
	progress := func(phase string, done, total int) {
		if !quiet {
			printProgress(userProgressPhase(phase), done, total)
		}
	}
	result, err := safeNativeExport(source, output, h, info.Size(), defaultNativeLimits(fullEML), progress)
	if encodeErr := json.NewEncoder(os.Stdout).Encode(result); encodeErr != nil {
		fmt.Fprintf(os.Stderr, "encode native worker report: %v\n", encodeErr)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func runNativeAnalyzeWorker(source string, quiet bool) {
	info, err := os.Stat(source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "native analysis worker: stat source: %v\n", err)
		os.Exit(1)
	}
	h := format.ReadHeader(source)
	if !h.OK {
		fmt.Fprintln(os.Stderr, "native analysis worker: source is not a recognized PFF/OST/PST file")
		os.Exit(1)
	}
	progress := func(phase string, done, total int) {
		if !quiet {
			printProgress(userProgressPhase(phase), done, total)
		}
	}
	result, err := safeNativeAnalyze(source, h, info.Size(), defaultNativeLimits(false), progress)
	if encodeErr := json.NewEncoder(os.Stdout).Encode(result); encodeErr != nil {
		fmt.Fprintf(os.Stderr, "encode native analysis report: %v\n", encodeErr)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func printProgress(label string, done, total int) {
	if jsonProgress {
		unit, basis := progressSemantics(label)
		event := struct {
			Event      string `json:"event"`
			Label      string `json:"label"`
			Done       int    `json:"done"`
			Total      int    `json:"total"`
			TotalKnown bool   `json:"total_known"`
			Unit       string `json:"unit"`
			Basis      string `json:"basis"`
		}{Event: "progress", Label: label, Done: done, Total: total, TotalKnown: total > 0, Unit: unit, Basis: basis}
		data, _ := json.Marshal(event)
		fmt.Fprintln(os.Stderr, string(data))
		return
	}
	if total <= 0 {
		fmt.Fprintf(os.Stderr, "\r%-34s %d", label, done)
		return
	}
	if done < 0 {
		done = 0
	}
	if done > total {
		done = total
	}
	const width = 24
	filled := done * width / total
	bar := strings.Repeat("#", filled) + strings.Repeat("-", width-filled)
	fmt.Fprintf(os.Stderr, "\r%-34s [%s] %d/%d", label, bar, done, total)
	if done >= total {
		fmt.Fprintln(os.Stderr)
	}
}

func progressSemantics(label string) (unit, basis string) {
	switch label {
	case "scanning recoverable messages":
		return "candidates", "bounded candidate set from the available index/BBT"
	case "writing recovered messages":
		return "messages", "recovered message set"
	case "standard mailbox export":
		return "messages", "indexed messages; total may be unknown until traversal completes"
	case "analyzing recoverable messages":
		return "messages", "bounded content analysis of recoverable candidates"
	default:
		return "items", "operation-specific count"
	}
}

func userProgressPhase(phase string) string {
	switch phase {
	case "collecting native candidates":
		return "scanning recoverable messages"
	case "analyzing recoverable messages":
		return "analyzing recoverable messages"
	case "writing native recovery output":
		return "writing recovered messages"
	default:
		return phase
	}
}

func trimWorkerError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 512 {
		return value[:512]
	}
	return value
}

func safeIndexedExport(source, output string, recovery bool, progress func(done, total int), fullEML bool) (result gopst.Report, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("go-pst backend panic: %v", recovered)
		}
	}()
	mode := gopst.EMLNone
	if fullEML {
		mode = gopst.EMLAll
	}
	return gopst.ExportWithProgressMode(source, output, nil, 0, recovery, progress, mode)
}

func safeNativeExport(source, output string, h format.Header, fileSize int64, limits native.Limits, progress func(phase string, done, total int)) (result native.PipelineReport, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("native recovery panic: %v", recovered)
		}
	}()
	return native.RecoverAndExportWithPhaseProgress(source, output, h, fileSize, limits, progress)
}

// safeNativeAnalyze performs bounded content collection without creating an export.
func safeNativeAnalyze(source string, h format.Header, fileSize int64, limits native.Limits, progress func(phase string, done, total int)) (result AnalysisReport, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("native analysis panic: %v", recovered)
		}
	}()
	scan, err := format.CollectIndexedRecoveredMessagesWithAttachmentLimit(
		source, h, fileSize, limits.MaxNodes, limits.MaxBlocks, limits.MaxBytes,
		limits.MaxRecords, limits.MaxMessages, limits.MaxBodyBytes, limits.MaxAttachmentBytes,
		limits.MaxErrorSamples, func(done, total int) {
			if progress != nil {
				progress("analyzing recoverable messages", done, total)
			}
		},
	)
	if scan.Cleanup != nil {
		defer func() { _ = scan.Cleanup() }()
	}
	result = AnalysisReport{Mode: scan.Mode, Status: scan.Status, Candidates: len(scan.Messages), Rejected: scan.Rejected, Errors: scan.Errors, LimitReached: scan.LimitReached}
	for _, message := range scan.Messages {
		if message.Incomplete {
			result.IncompleteMessages++
		}
		result.Attachments += len(message.Attachments)
		for _, attachment := range message.Attachments {
			if attachment.Incomplete {
				result.IncompleteAttachments++
			}
		}
	}
	return result, err
}

func finish(r report, jsonOutput bool, outputDir, exportDir string) {
	encoded, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		fatalf("encode report: %v", err)
	}
	if outputDir == "" {
		outputDir = exportDir
	}
	if outputDir != "" {
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			fatalf("create report directory: %v", err)
		}
		if err := os.WriteFile(filepath.Join(outputDir, "ostom-report.json"), append(encoded, '\n'), 0o644); err != nil {
			fatalf("write report: %v", err)
		}
	}
	if jsonOutput {
		fmt.Println(string(encoded))
		return
	}
	fmt.Printf("Status: %s\nSource: %s (%d bytes)\n", r.Status, r.Source, r.SourceBytes)
	fmt.Printf("File assessment: %s\n", r.Assessment.Summary)
	if r.Assessment.DeepRecovery != "not_needed" {
		fmt.Printf("Recovery recommendation: %s\n", r.Assessment.DeepRecovery)
	}
	if r.Indexed != nil {
		fmt.Printf("Indexed export (%s): %d written, %d partial, %d failed\n", r.IndexedStatus, r.Indexed.Messages, r.Indexed.MessagesPartial, r.Indexed.SkippedMessages)
	}
	if r.Native != nil {
		fmt.Printf("Recovery scan: %d messages found; %s\n", len(r.Native.Collection.Messages), r.Native.Export.Status)
		for _, line := range r.Native.Export.Summary {
			fmt.Printf("  %s\n", line)
		}
	}
	for _, message := range r.Errors {
		fmt.Printf("Warning: %s\n", message)
	}
	for _, warning := range r.Warnings {
		fmt.Printf("Note: %s\n", warning)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ostom: "+format+"\n", args...)
	os.Exit(1)
}
