package command

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestArchitectureSummarizesTheAgentSnapshot(t *testing.T) {
	run := running(t, map[string]answer{"/api/architecture": {body: fixture(t, "architecture.json")}})
	stdout, stderr, code := run("architecture", "acme/billing")
	if code != 0 {
		t.Fatalf("%d: %s", code, stderr)
	}
	for _, word := range []string{"2 components", "1 relationships", "payments", "identity", "FILES"} {
		if !strings.Contains(stdout, word) {
			t.Errorf("missing %q from %s", word, stdout)
		}
	}
	stdout, stderr, code = run("architecture", "acme/billing", "--json")
	if code != 0 || !json.Valid([]byte(stdout)) || !strings.Contains(stdout, "fingerprint") {
		t.Fatalf("invalid export: %s %s", stdout, stderr)
	}
}

func TestArchitectureComparesAnExportedBaseline(t *testing.T) {
	run := running(t, map[string]answer{"/api/architecture/compare": {body: fixture(t, "architecture-comparison.json")}})
	stdout, stderr, code := run("architecture", "acme/billing", "--baseline", "testdata/architecture.json")
	if code != 0 || !strings.Contains(stdout, "0 changed components") {
		t.Fatalf("%d: %s %s", code, stdout, stderr)
	}
	for _, args := range [][]string{
		{"architecture", "other/repo", "--baseline", "testdata/architecture.json"},
		{"architecture", "acme/billing", "--baseline", "testdata/architecture.json", "--depth", "2"},
		{"architecture", "acme/billing", "--depth", "5"},
	} {
		_, _, code = run(args...)
		if code == 0 {
			t.Fatalf("invalid request succeeded: %v", args)
		}
	}
}
