package command

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func snapshotCheckout(t *testing.T) (string, string, string) {
	t.Helper()
	folder := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", folder}, args...)...)
		encoded, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git failed: %s", encoded)
		}
		return strings.TrimSpace(string(encoded))
	}
	git("init", "-q")
	git("config", "user.email", "review@example.com")
	git("config", "user.name", "Reviewer")
	if err := os.WriteFile(filepath.Join(folder, "changed.go"), []byte("package example\nvar Value = 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "context.go"), []byte("package example\nvar Context = Value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "feat: Add the example")
	base := git("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(folder, "changed.go"), []byte("package example\nvar Value = 2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "feat: Change the example")
	head := git("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(folder, "untracked.env"), []byte("PRIVATE=local\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return folder, base, head
}

func TestRemoteReviewUploadsCommittedContextAndUsesWorkspaceCredentials(t *testing.T) {
	folder, base, head := snapshotCheckout(t)
	t.Setenv("SOURCEANT_REVIEW_TOKEN", "your-api-key-here")
	t.Setenv("SOURCEANT_REVIEW_WORKSPACE", "benchmark")
	var captured reviewSnapshot
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/reviews/snapshots" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer your-api-key-here" || r.Header.Get("X-SourceAnt-Workspace") != "benchmark" {
			t.Error("workspace credentials did not reach the API")
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Error(err)
		}
		encoded, err := os.ReadFile(filepath.Join("testdata", "review-snapshot-response.json"))
		if err != nil {
			t.Fatal(err)
		}
		var response struct {
			Status string         `json:"status"`
			Data   snapshotResult `json:"data"`
		}
		if err := json.Unmarshal(encoded, &response); err != nil {
			t.Fatal(err)
		}
		response.Data.Review.Base = base
		response.Data.Snapshot = snapshotIdentity{Repository: "acme/example", Base: base, Head: head}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"review", "--repo", folder, "--base", base, "--head", head, "--repository", "acme/example", "--reviewer", server.URL + "/api/reviews/snapshots", "--format", "json", "--review-option", "discovery-passes=3"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exited %d: %s", code, stderr.String())
	}
	if captured.Files["context.go"] != "package example\nvar Context = Value\n" {
		t.Error("unchanged context was not uploaded")
	}
	if captured.Files["changed.go"] != "package example\nvar Value = 2\n" || !strings.Contains(captured.Diff, "+var Value = 2") {
		t.Error("head source or committed diff was lost")
	}
	if _, found := captured.Files["untracked.env"]; found {
		t.Error("an untracked file was uploaded")
	}
	for file := range captured.Files {
		if strings.HasPrefix(file, ".git/") {
			t.Error("Git metadata was uploaded")
		}
	}
	if captured.Configuration["discovery-passes"] != "3" {
		t.Error("review settings were lost")
	}
	if !strings.Contains(stdout.String(), "review-0") {
		t.Error("execution metadata from the API was lost")
	}
	if !json.Valid(stdout.Bytes()) {
		t.Fatalf("invalid JSON: %s", stdout.String())
	}
}

func TestRemoteReviewRefusesWrongHeadBeforeUploading(t *testing.T) {
	folder, base, _ := snapshotCheckout(t)
	t.Setenv("SOURCEANT_REVIEW_TOKEN", "your-api-key-here")
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"review", "--repo", folder, "--base", base, "--head", strings.Repeat("a", 40), "--repository", "acme/example", "--reviewer", server.URL}, &stdout, &stderr)
	if code != 1 || called || stdout.Len() != 0 {
		t.Fatalf("invalid input was uploaded: code=%d called=%v", code, called)
	}
}

func TestRemoteReviewDoesNotFollowRedirects(t *testing.T) {
	folder, base, head := snapshotCheckout(t)
	t.Setenv("SOURCEANT_REVIEW_TOKEN", "your-api-key-here")
	forwarded := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded = true }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"review", "--repo", folder, "--base", base, "--head", head, "--repository", "acme/example", "--reviewer", server.URL}, &stdout, &stderr)
	if code != 1 || forwarded || stdout.Len() != 0 {
		t.Fatalf("the request followed a redirect: code=%d forwarded=%v", code, forwarded)
	}
}
