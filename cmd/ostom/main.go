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
)

type report struct {
	Source        string                     `json:"source"`
	SourceBytes   int64                      `json:"source_bytes"`
	Header        format.Header              `json:"header"`
	IndexRoots    format.IndexRootScan       `json:"index_roots"`
	Assessment    format.IntegrityAssessment `json:"assessment"`
	RecoveryScan  bool                       `json:"recovery_scan"`
	RecoveryRun   string                     `json:"recovery_run"`
	Indexed       *gopst.Report              `json:"indexed_export,omitempty"`
	IndexedStatus string                     `json:"indexed_status,omitempty"`
	Native        *native.PipelineReport     `json:"native_recovery,omitempty"`
	Status        string                     `json:"status"`
	Errors        []string                   `json:"errors,omitempty"`
	Warnings      []string                   `json:"warnings,omitempty"`
	Elapsed       string                     `json:"elapsed"`
}

func main() {
	jsonOutput := flag.Bool("json", false, "write the report as JSON to stdout")
	exportDir := flag.String("export-dir", "", "directory for Thunderbird Local Folders export")
	outputDir := flag.String("output-dir", "", "directory for ostom-report.json")
	recoveryScan := flag.Bool("recovery-scan", false, "force bounded native recovery")
	quiet := flag.Bool("quiet", false, "suppress progress on stderr")
	indexedWorker := flag.Bool("indexed-worker", false, "internal indexed export worker")
	indexedRecovery := flag.Bool("indexed-recovery", false, "internal indexed worker recovery mode")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "Usage: ostom [--json] [--output-dir DIR] [--export-dir DIR] [--recovery-scan] [--quiet] file.ost")
		os.Exit(2)
	}
	if *recoveryScan && *exportDir == "" {
		fmt.Fprintln(os.Stderr, "--recovery-scan requires --export-dir")
		os.Exit(2)
	}
	if *indexedWorker {
		runIndexedWorker(flag.Arg(0), *exportDir, *indexedRecovery, *quiet)
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
	r := report{Source: source, SourceBytes: info.Size(), Header: h, IndexRoots: roots, Assessment: assessment, RecoveryScan: *recoveryScan, RecoveryRun: "not_run", Status: "completed"}
	if !h.OK {
		r.Status = "failed"
		r.Errors = append(r.Errors, "source is not a recognized PFF/OST/PST file")
		finish(r, *jsonOutput, *outputDir, *exportDir)
		os.Exit(1)
	}

	if *exportDir != "" {
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
			limits := native.Limits{MaxNodes: 100000, MaxBlocks: 1024, MaxBytes: 64 << 20, MaxRecords: 4096, MaxMessages: 100000, MaxBodyBytes: 4 << 20, MaxAttachmentBytes: 32 << 20, MaxErrorSamples: 256}
			nativeReport, nativeErr := safeNativeExport(source, filepath.Join(*exportDir, "Local Folders"), h, info.Size(), limits, func(phase string, done, total int) {
				if !*quiet {
					printProgress(userProgressPhase(phase), done, total)
				}
			})
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
		indexed, exportErr := runIndexedExportWithTimeout(source, *exportDir, false, progress, *quiet)
		r.Indexed = &indexed
		if exportErr != nil {
			r.IndexedStatus = "timeout_or_failed"
			if strings.Contains(exportErr.Error(), "exceeded") {
				r.IndexedStatus = "timeout"
			}
			r.Warnings = append(r.Warnings, "indexed export: "+exportErr.Error())
		} else {
			r.IndexedStatus = "completed"
		}
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
	r.Elapsed = time.Since(started).Round(time.Millisecond).String()
	if len(r.Errors) > 0 {
		r.Status = "completed with errors"
	}
	finish(r, *jsonOutput, *outputDir, *exportDir)
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

func runIndexedExportWithTimeout(source, output string, recovery bool, progress func(done, total int), quiet bool) (gopst.Report, error) {
	result := gopst.Report{Directory: output, RecoveryMode: recovery}
	executable, err := os.Executable()
	if err != nil {
		return result, fmt.Errorf("locate indexed worker: %w", err)
	}
	args := []string{"-indexed-worker", "-export-dir", output}
	if quiet {
		args = append(args, "-quiet")
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

func runIndexedWorker(source, output string, recovery, quiet bool) {
	var progress func(done, total int)
	if !quiet {
		progress = func(done, total int) { printProgress("standard mailbox export", done, total) }
	}
	result, err := safeIndexedExport(source, output, recovery, progress)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "encode indexed worker report: %v\n", err)
		os.Exit(1)
	}
}

func printProgress(label string, done, total int) {
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

func userProgressPhase(phase string) string {
	switch phase {
	case "collecting native candidates":
		return "scanning recoverable messages"
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

func safeIndexedExport(source, output string, recovery bool, progress func(done, total int)) (result gopst.Report, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("go-pst backend panic: %v", recovered)
		}
	}()
	return gopst.ExportWithProgress(source, output, nil, 0, recovery, progress)
}

func safeNativeExport(source, output string, h format.Header, fileSize int64, limits native.Limits, progress func(phase string, done, total int)) (result native.PipelineReport, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("native recovery panic: %v", recovered)
		}
	}()
	return native.RecoverAndExportWithPhaseProgress(source, output, h, fileSize, limits, progress)
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
