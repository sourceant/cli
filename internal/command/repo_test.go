package command

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestRepoAddRegistersThroughTheAgent(t *testing.T) {
	path := t.TempDir()
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodPost || r.URL.Path != "/api/repositories" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var request map[string]string
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if request["path"] != filepath.Clean(path) || request["name"] != "local/capabilities" {
			t.Errorf("unexpected registration: %v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture(t, "registered.json"))
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--agent", server.URL, "repo", "add", path, "--name", "local/capabilities", "--json"}, &stdout, &stderr)
	if code != 0 || !called {
		t.Fatalf("registration exited %d: %s", code, &stderr)
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["name"] != "local/capabilities" {
		t.Fatalf("unexpected output: %s", &stdout)
	}
}
