package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteStreamKeepsBoundedChunksAndDuplicateEvidence(t *testing.T) {
	dir := t.TempDir()
	const total = 2049
	if err := WriteStream(dir, "sample.ost", func(visit func(Record) error) error {
		for index := total - 1; index >= 0; index-- {
			record := Record{Kind: "message", Source: "native", Identity: fmt.Sprintf("%04d", index), Route: "native", Status: "recovered"}
			if index == total-1 {
				record.MessageID = "<same@example.test>"
				record.Errors = []string{"z", "a"}
			}
			if err := visit(record); err != nil {
				return err
			}
		}
		duplicate := Record{Kind: "message", Source: "native", Identity: "duplicate", Route: "native", MessageID: "<same@example.test>", Status: "recovered"}
		return visit(duplicate)
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "recovery-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document Document
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.Summary.Total != total+1 || document.Summary.DuplicateCandidates != 1 || document.Summary.UniqueMessages != total {
		t.Fatalf("unexpected streamed summary: %+v", document.Summary)
	}
	if len(document.Records) != total+1 || document.Records[0].Identity != "0000" || document.Records[len(document.Records)-1].Identity != "duplicate" {
		t.Fatalf("unexpected streamed record order: first=%q last=%q count=%d", document.Records[0].Identity, document.Records[len(document.Records)-1].Identity, len(document.Records))
	}
	if document.Records[total].DuplicateOf == "" {
		t.Fatal("streamed duplicate evidence was not recorded")
	}
}

func TestWriteProducesSortedEquivalentManifests(t *testing.T) {
	dir := t.TempDir()
	records := []Record{
		{Kind: "attachment", Source: "native", Identity: "b", Route: "native-recovered", Status: "partial", Errors: []string{"z", "a"}},
		{Kind: "message", Source: "indexed", Identity: "a", Route: "indexed", Status: "recovered"},
	}
	if err := Write(dir, "input.ost", records); err != nil {
		t.Fatal(err)
	}
	jsonData, err := os.ReadFile(filepath.Join(dir, "recovery-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(jsonData), "\"version\": 1") || strings.Index(string(jsonData), "\"indexed\"") > strings.Index(string(jsonData), "\"native\"") || !strings.Contains(string(jsonData), "\"errors\": [\n        \"a\",\n        \"z\"") {
		t.Fatalf("unexpected deterministic JSON manifest: %s", jsonData)
	}
	if !strings.Contains(string(jsonData), "\"total\": 2") || !strings.Contains(string(jsonData), "\"messages\": 1") || !strings.Contains(string(jsonData), "\"attachments\": 1") || !strings.Contains(string(jsonData), "\"partial\": 1") || !strings.Contains(string(jsonData), "\"recovered\": 1") || !strings.Contains(string(jsonData), "\"message\": {\n        \"recovered\": 1") || !strings.Contains(string(jsonData), "\"orphans\": 1") {
		t.Fatalf("unexpected manifest summary: %s", jsonData)
	}
	csvData, err := os.ReadFile(filepath.Join(dir, "recovery-manifest.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(string(csvData)), "\n"); len(lines) != 3 || !strings.Contains(lines[1], "indexed") || !strings.Contains(lines[2], "a; z") {
		t.Fatalf("unexpected CSV manifest: %s", csvData)
	}
}

func TestWriteOwnedManifests(t *testing.T) {
	dir := t.TempDir()
	records := []Record{{Kind: "message", Source: "native", Identity: "1", Route: "native", Status: "exported", Errors: []string{"z", "a"}}}
	if err := WriteOwned(dir, "sample.ost", records); err != nil {
		t.Fatal(err)
	}
	if len(records[0].Errors) != 2 || records[0].Errors[0] != "a" {
		t.Fatalf("owned records were not normalized: %+v", records)
	}
	if _, err := os.Stat(filepath.Join(dir, "recovery-manifest.json")); err != nil {
		t.Fatal(err)
	}
}

func TestWriteMarksMessageIDDuplicateCandidatesWithoutDroppingRecords(t *testing.T) {
	dir := t.TempDir()
	records := []Record{
		{Kind: "message", Source: "native", Identity: "native-1", Route: "native-recovered", MessageID: "<same@example.test>", Status: "recovered"},
		{Kind: "message", Source: "indexed", Identity: "indexed-1", Route: "indexed", MessageID: "<same@example.test>", Status: "exported"},
	}
	if err := Write(dir, "sample.ost", records); err != nil {
		t.Fatal(err)
	}
	document, err := os.ReadFile(filepath.Join(dir, "recovery-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(document)
	if !strings.Contains(text, "\"duplicate_candidates\": 1") || !strings.Contains(text, "\"unique_messages\": 1") || !strings.Contains(text, "\"duplicate_of\"") {
		t.Fatalf("duplicate candidate was not recorded: %s", document)
	}
	if !strings.Contains(text, "\"total\": 2") {
		t.Fatalf("duplicate candidate dropped a record: %s", document)
	}
}
