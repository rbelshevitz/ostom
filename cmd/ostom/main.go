// ostom is a read-only OST/PST recovery exporter.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"ostom/internal/export/gopst"
	"ostom/internal/export/native"
	"ostom/internal/format"
)

type report struct {
	Source       string                 `json:"source"`
	SourceBytes  int64                  `json:"source_bytes"`
	Header       format.Header          `json:"header"`
	IndexRoots   format.IndexRootScan   `json:"index_roots"`
	RecoveryScan bool                   `json:"recovery_scan"`
	Indexed      *gopst.Report          `json:"indexed_export,omitempty"`
	Native       *native.PipelineReport `json:"native_recovery,omitempty"`
	Status       string                 `json:"status"`
	Errors       []string               `json:"errors,omitempty"`
	Elapsed      string                 `json:"elapsed"`
}

func main() {
	jsonOutput := flag.Bool("json", false, "write the report as JSON to stdout")
	exportDir := flag.String("export-dir", "", "directory for Thunderbird Local Folders export")
	outputDir := flag.String("output-dir", "", "directory for ostom-report.json")
	recoveryScan := flag.Bool("recovery-scan", false, "also run bounded native recovery")
	memoryLimit := flag.Int("memory-limit", 256, "native recovery memory budget hint in MiB")
	idleTimeout := flag.Duration("idle-timeout", 10*time.Minute, "reserved worker idle timeout")
	quiet := flag.Bool("quiet", false, "suppress progress on stderr")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "Usage: ostom [--json] [--export-dir DIR] [--recovery-scan] file.ost")
		os.Exit(2)
	}
	if *memoryLimit < 0 {
		fmt.Fprintln(os.Stderr, "--memory-limit must not be negative")
		os.Exit(2)
	}
	if *idleTimeout < 0 {
		fmt.Fprintln(os.Stderr, "--idle-timeout must not be negative")
		os.Exit(2)
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
	r := report{Source: source, SourceBytes: info.Size(), Header: h, IndexRoots: format.ScanIndexRoots(source, h, info.Size()), RecoveryScan: *recoveryScan, Status: "completed"}
	if !h.OK {
		r.Status = "failed"
		r.Errors = append(r.Errors, "source is not a recognized PFF/OST/PST file")
		finish(r, *jsonOutput, *outputDir, *exportDir)
		os.Exit(1)
	}

	if *exportDir != "" {
		progress := func(done, total int) {
			if !*quiet {
				if total > 0 {
					fmt.Fprintf(os.Stderr, "indexed export: %d/%d messages\n", done, total)
				} else {
					fmt.Fprintf(os.Stderr, "indexed export: %d messages\n", done)
				}
			}
		}
		indexed, exportErr := safeIndexedExport(source, *exportDir, *recoveryScan, progress)
		r.Indexed = &indexed
		if exportErr != nil {
			r.Errors = append(r.Errors, "indexed export: "+exportErr.Error())
		}
		if *recoveryScan {
			limits := native.Limits{MaxNodes: 100000, MaxBlocks: 1024, MaxBytes: 64 << 20, MaxRecords: 4096, MaxMessages: 100000, MaxBodyBytes: 4 << 20, MaxAttachmentBytes: 32 << 20, MaxErrorSamples: 256}
			nativeReport, nativeErr := safeNativeExport(source, filepath.Join(*exportDir, "Local Folders"), h, info.Size(), limits, func(phase string, done, total int) {
				if !*quiet {
					if total > 0 {
						fmt.Fprintf(os.Stderr, "%s: %d/%d\n", phase, done, total)
					} else {
						fmt.Fprintf(os.Stderr, "%s: %d\n", phase, done)
					}
				}
			})
			r.Native = &nativeReport
			if nativeErr != nil {
				r.Errors = append(r.Errors, "native recovery: "+nativeErr.Error())
			}
		}
	} else {
		r.Errors = append(r.Errors, "no export directory supplied; inspection only")
	}
	r.Elapsed = time.Since(started).Round(time.Millisecond).String()
	if len(r.Errors) > 0 {
		r.Status = "completed with errors"
	}
	finish(r, *jsonOutput, *outputDir, *exportDir)
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
	if r.Indexed != nil {
		fmt.Printf("Indexed export: %d written, %d partial, %d failed\n", r.Indexed.Messages, r.Indexed.MessagesPartial, r.Indexed.SkippedMessages)
	}
	if r.Native != nil {
		fmt.Printf("Native recovery: %d candidates; %s\n", len(r.Native.Collection.Messages), r.Native.Export.Status)
		for _, line := range r.Native.Export.Summary {
			fmt.Printf("  %s\n", line)
		}
	}
	for _, message := range r.Errors {
		fmt.Printf("Warning: %s\n", message)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ostom: "+format+"\n", args...)
	os.Exit(1)
}
