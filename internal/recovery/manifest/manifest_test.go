package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	csvData, err := os.ReadFile(filepath.Join(dir, "recovery-manifest.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(string(csvData)), "\n"); len(lines) != 3 || !strings.Contains(lines[1], "indexed") || !strings.Contains(lines[2], "a; z") {
		t.Fatalf("unexpected CSV manifest: %s", csvData)
	}
}
