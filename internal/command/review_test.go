package command

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// indexing is the captured answer with the folder the test is standing in, so
// the review is asked for the repository that folder belongs to.
func indexing(t *testing.T, folder string) []byte {
	t.Helper()
	return bytes.Replace(fixture(t, "repositories.json"), []byte("/app"), []byte(folder), 1)
}

// reviewing starts a stand-in agent that answers each path in turn, so a review
// can be running on one call and finished on the next.
func reviewing(t *testing.T, answers map[string][]answer) (func(args ...string) (string, string, int), *[]string, *[]byte) {
	t.Helper()
	asked := []string{}
	var sent []byte
	mux := http.NewServeMux()
	for path, replies := range answers {
		replies := replies
		turn := 0
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			asked = append(asked, r.Method+" "+r.URL.Path)
			if r.Method == http.MethodPost {
				sent, _ = readAll(r)
			}
			reply := replies[min(turn, len(replies)-1)]
			turn++
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
	}, &asked, &sent
}

func readAll(r *http.Request) ([]byte, error) {
	var body bytes.Buffer
	_, err := body.ReadFrom(r.Body)
	return body.Bytes(), err
}

func TestReviewPrintsTheLinkAndWhatWasMadeOfTheChange(t *testing.T) {
	folder := t.TempDir()
	run, _, _ := reviewing(t, map[string][]answer{
		"/api/repositories": {{body: indexing(t, folder)}},
		"/api/reviews":      {{status: http.StatusAccepted, body: fixture(t, "review-started.json")}},
		"/api/reviews/":     {{body: fixture(t, "review-done.json")}},
	})

	stdout, stderr, code := run("review", folder)

	if code != 0 {
		t.Fatalf("exited %d: %s", code, stderr)
	}
	for _, want := range []string{"/reviews/", "feat/subtract against main (bd51156)", "1 file changed", "Ready."} {
		if !strings.Contains(stdout, want) {
			t.Errorf("%q is missing from:\n%s", want, stdout)
		}
	}
}

func TestCommittedReviewFlagsReachTheAPI(t *testing.T) {
	folder := t.TempDir()
	run, _, sent := reviewing(t, map[string][]answer{
		"/api/repositories": {{body: indexing(t, folder)}},
		"/api/reviews":      {{status: http.StatusAccepted, body: fixture(t, "review-started.json")}},
		"/api/reviews/":     {{body: fixture(t, "review-done.json")}},
	})
	stdout, stderr, code := run("review", "--dir", folder, "--base", strings.Repeat("a", 40), "--head", strings.Repeat("b", 40), "--format", "json")
	if code != 0 {
		t.Fatalf("exited %d: %s", code, stderr)
	}
	var request map[string]any
	if err := json.Unmarshal(*sent, &request); err != nil {
		t.Fatal(err)
	}
	if request["against"] != strings.Repeat("a", 40) || request["head"] != strings.Repeat("b", 40) {
		t.Fatalf("commit comparison was lost: %s", *sent)
	}
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("invalid JSON output: %s", stdout)
	}
}

func TestABlockingFindingIsPrintedAndExitsNonZero(t *testing.T) {
	folder := t.TempDir()
	run, _, _ := reviewing(t, map[string][]answer{
		"/api/repositories": {{body: indexing(t, folder)}},
		"/api/reviews":      {{status: http.StatusAccepted, body: fixture(t, "review-started.json")}},
		"/api/reviews/":     {{body: fixture(t, "review-blocked.json")}},
	})

	stdout, stderr, code := run("review", folder)

	if code != 2 {
		t.Fatalf("exited %d, want 2: %s", code, stderr)
	}
	if strings.Contains(stderr, "sourceant:") {
		t.Errorf("a review that found something is not an error: %q", stderr)
	}
	for _, want := range []string{"blocking", "calc.py:5", "Not ready.", "4 blocking"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("%q is missing from:\n%s", want, stdout)
		}
	}
}

func TestReviewKeepsAskingUntilItIsFinished(t *testing.T) {
	was := beat
	beat = time.Millisecond
	t.Cleanup(func() { beat = was })

	folder := t.TempDir()
	run, asked, _ := reviewing(t, map[string][]answer{
		"/api/repositories": {{body: indexing(t, folder)}},
		"/api/reviews":      {{status: http.StatusAccepted, body: fixture(t, "review-started.json")}},
		"/api/reviews/": {
			{body: fixture(t, "review-started.json")},
			{body: fixture(t, "review-done.json")},
		},
	})

	stdout, stderr, code := run("review", folder)

	if code != 0 {
		t.Fatalf("exited %d: %s", code, stderr)
	}
	reads := 0
	for _, one := range *asked {
		if strings.HasPrefix(one, "GET /api/reviews/") {
			reads++
		}
	}
	if reads != 2 {
		t.Errorf("read the review %d times, want it asked again while it ran", reads)
	}
	if !strings.Contains(stdout, "Ready.") {
		t.Errorf("the finished review is missing from:\n%s", stdout)
	}
}

func TestNoWaitPrintsTheLinkAndLeavesItRunning(t *testing.T) {
	folder := t.TempDir()
	run, asked, _ := reviewing(t, map[string][]answer{
		"/api/repositories": {{body: indexing(t, folder)}},
		"/api/reviews":      {{status: http.StatusAccepted, body: fixture(t, "review-started.json")}},
		"/api/reviews/":     {{body: fixture(t, "review-done.json")}},
	})

	stdout, stderr, code := run("review", folder, "--no-wait")

	if code != 0 {
		t.Fatalf("exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "/reviews/4d4ca793285c467dbc406713a2601b00") {
		t.Errorf("got %q, want the link to the review", stdout)
	}
	for _, one := range *asked {
		if strings.HasPrefix(one, "GET /api/reviews/") {
			t.Error("waited for a review it was told not to wait for")
		}
	}
}

func TestWhatToCompareAgainstReachesTheAgent(t *testing.T) {
	folder := t.TempDir()
	run, _, sent := reviewing(t, map[string][]answer{
		"/api/repositories": {{body: indexing(t, folder)}},
		"/api/reviews":      {{status: http.StatusAccepted, body: fixture(t, "review-started.json")}},
		"/api/reviews/":     {{body: fixture(t, "review-done.json")}},
	})

	_, stderr, code := run("review", folder, "--against", "dev", "--title", "Add subtract", "--no-model", "--no-wait")

	if code != 0 {
		t.Fatalf("exited %d: %s", code, stderr)
	}
	var ask struct {
		Repository string `json:"repository"`
		Against    string `json:"against"`
		Title      string `json:"title"`
		UseModel   bool   `json:"use_model"`
	}
	if err := json.Unmarshal(*sent, &ask); err != nil {
		t.Fatalf("could not read what was asked: %v", err)
	}
	if ask.Against != "dev" || ask.Title != "Add subtract" || ask.UseModel {
		t.Errorf("asked %+v, want the flags as given", ask)
	}
	if ask.Repository != "local/sourceant" {
		t.Errorf("asked for %q, want the repository the folder belongs to", ask.Repository)
	}
}

func TestAReviewNobodyNamedSaysItCameFromTheTerminal(t *testing.T) {
	folder := t.TempDir()
	run, _, sent := reviewing(t, map[string][]answer{
		"/api/repositories": {{body: indexing(t, folder)}},
		"/api/reviews":      {{status: http.StatusAccepted, body: fixture(t, "review-started.json")}},
		"/api/reviews/":     {{body: fixture(t, "review-done.json")}},
	})

	_, stderr, code := run("review", folder, "--no-wait")

	if code != 0 {
		t.Fatalf("exited %d: %s", code, stderr)
	}
	var ask struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(*sent, &ask); err != nil {
		t.Fatalf("could not read what was asked: %v", err)
	}
	if ask.Title != "From the terminal" {
		t.Errorf("asked with title %q, want where it came from", ask.Title)
	}
}

func TestAFolderNobodyIndexedNamesTheCommandThatAddsIt(t *testing.T) {
	run, _, _ := reviewing(t, map[string][]answer{
		"/api/repositories": {{body: []byte("[]")}},
	})

	_, stderr, code := run("review", t.TempDir())

	if code != 1 {
		t.Fatalf("exited %d, want 1", code)
	}
	if !strings.Contains(stderr, "sourceant repo add") {
		t.Errorf("got %q, want how to index it", stderr)
	}
}

func TestAReviewThatFailedSaysWhyInItsOwnWords(t *testing.T) {
	folder := t.TempDir()
	run, _, _ := reviewing(t, map[string][]answer{
		"/api/repositories": {{body: indexing(t, folder)}},
		"/api/reviews":      {{status: http.StatusAccepted, body: fixture(t, "review-started.json")}},
		"/api/reviews/":     {{body: fixture(t, "review-failed.json")}},
	})

	_, stderr, code := run("review", folder)

	if code != 1 {
		t.Fatalf("exited %d, want 1", code)
	}
	if !strings.Contains(stderr, "No model is configured") {
		t.Errorf("got %q, want the reason the agent gave", stderr)
	}
}

func TestReviewAsJSONIsTheAgentsOwnAnswer(t *testing.T) {
	folder := t.TempDir()
	run, _, _ := reviewing(t, map[string][]answer{
		"/api/repositories": {{body: indexing(t, folder)}},
		"/api/reviews":      {{status: http.StatusAccepted, body: fixture(t, "review-started.json")}},
		"/api/reviews/":     {{body: fixture(t, "review-blocked.json")}},
	})

	stdout, _, code := run("--json", "review", folder)

	if code != 2 {
		t.Fatalf("exited %d, want 2", code)
	}
	for _, want := range []string{`"patch"`, `"verdicts"`, `"severity"`, `"suggestions"`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("%s was dropped on the way through:\n%s", want, stdout)
		}
	}
}
