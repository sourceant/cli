package command

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixtures are answers captured from a running agent, not written here, so
// a change to what it serves fails these rather than passing against our guess.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return data
}

type answer struct {
	status int
	body   []byte
}

// running starts a stand-in agent and returns a run function that drives the
// CLI the way a person does: arguments in, streams and an exit code out.
func running(t *testing.T, answers map[string]answer) func(args ...string) (string, string, int) {
	t.Helper()
	mux := http.NewServeMux()
	for path, reply := range answers {
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if reply.status != 0 {
				w.WriteHeader(reply.status)
			}
			_, _ = w.Write(reply.body)
		})
	}
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return func(args ...string) (string, string, int) {
		var stdout, stderr bytes.Buffer
		code := Run(append([]string{"--agent", server.URL}, args...), &stdout, &stderr)
		return stdout.String(), stderr.String(), code
	}
}

func TestStatusSaysWhetherTheIndexerIsAnswering(t *testing.T) {
	run := running(t, map[string]answer{
		"/health": {body: fixture(t, "health.json")},
	})

	stdout, stderr, code := run("status")

	if code != 0 {
		t.Fatalf("exited %d: %s", code, stderr)
	}
	for _, want := range []string{"agent", "indexer", "answering", "0.1.0"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("%q is missing from:\n%s", want, stdout)
		}
	}
}

func TestReposListsWhatIsIndexedHere(t *testing.T) {
	run := running(t, map[string]answer{
		"/api/repositories": {body: fixture(t, "repositories.json")},
	})

	stdout, stderr, code := run("repos")

	if code != 0 {
		t.Fatalf("exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "local/sourceant") {
		t.Errorf("the repository is missing from:\n%s", stdout)
	}
}

func TestAnEmptyMachineIsToldWhatToDoNext(t *testing.T) {
	run := running(t, map[string]answer{
		"/api/repositories": {body: []byte("[]")},
	})

	stdout, _, code := run("repos")

	if code != 0 {
		t.Fatalf("exited %d", code)
	}
	if !strings.Contains(stdout, "sourceant repo add") {
		t.Errorf("an empty machine was not told how to fill it:\n%s", stdout)
	}
}

func TestGraphCountsWhatTheIndexerFound(t *testing.T) {
	run := running(t, map[string]answer{
		"/api/graph": {body: fixture(t, "graph.json")},
	})

	stdout, stderr, code := run("graph", "local/sourceant")

	if code != 0 {
		t.Fatalf("exited %d: %s", code, stderr)
	}
	for _, want := range []string{"nodes", "links", "KIND", "python", "EDGE", "imports"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("%q is missing from:\n%s", want, stdout)
		}
	}
}

func TestGraphAsJSONIsTheAgentsOwnAnswer(t *testing.T) {
	run := running(t, map[string]answer{
		"/api/graph": {body: fixture(t, "graph.json")},
	})

	stdout, _, code := run("--json", "graph", "local/sourceant")

	if code != 0 {
		t.Fatalf("exited %d", code)
	}
	if !strings.Contains(stdout, `"labels"`) || !strings.Contains(stdout, `"truncated"`) {
		t.Errorf("the raw graph was not passed through:\n%s", stdout)
	}
}

func TestAnUnregisteredRepositoryIsReportedInItsOwnWords(t *testing.T) {
	run := running(t, map[string]answer{
		"/api/graph": {
			status: http.StatusNotFound,
			body:   []byte(`{"error":"acme/nope is not registered on this machine"}`),
		},
	})

	_, stderr, code := run("graph", "acme/nope")

	if code != 1 {
		t.Fatalf("exited %d, want 1", code)
	}
	if !strings.Contains(stderr, "not registered on this machine") {
		t.Errorf("got %q, want the reason the agent gave", stderr)
	}
}

func TestAnAgentThatIsNotRunningSaysHowToStartIt(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := Run([]string{"--agent", "http://127.0.0.1:1", "status"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("exited %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "Start it with sourceant-agent") {
		t.Errorf("got %q, want what to do about it", stderr.String())
	}
}

func TestUIPrintsTheAddressWithoutOpeningAnything(t *testing.T) {
	run := running(t, map[string]answer{
		"/health": {body: fixture(t, "health.json")},
	})

	stdout, stderr, code := run("ui", "--no-open")

	if code != 0 {
		t.Fatalf("exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "http://127.0.0.1:") {
		t.Errorf("got %q, want the address to open", stdout)
	}
}

func TestUISaysWhatToInstallRatherThanOpeningAnErrorPage(t *testing.T) {
	// A home with no agent in it, so the test cannot start one that happens to
	// be installed on the machine running it.
	t.Setenv("SOURCEANT_INSTALL_HOME", t.TempDir())
	var stdout, stderr bytes.Buffer

	code := Run([]string{"--agent", "http://127.0.0.1:1", "ui", "--no-open"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("exited %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "sourceant setup") {
		t.Errorf("got %q, want what to do about it", stderr.String())
	}
}

func TestVersionNeedsNoAgent(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := Run([]string{"--agent", "http://127.0.0.1:1", "version"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exited %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "sourceant ") {
		t.Errorf("got %q, want the build", stdout.String())
	}
}
