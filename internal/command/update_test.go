package command

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// forge answers as the release API does, for whichever repositories a test
// names.
func forge(t *testing.T, versions map[string]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for repo, version := range versions {
			if !strings.HasPrefix(r.URL.Path, "/"+repo+"/releases") {
				continue
			}
			body := map[string]any{"tag_name": "v" + version}
			if strings.Contains(r.URL.RawQuery, "per_page") {
				_ = json.NewEncoder(w).Encode([]map[string]any{body})
				return
			}
			_ = json.NewEncoder(w).Encode(body)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestCheckSaysWhatIsAvailableAndChangesNothing(t *testing.T) {
	server := forge(t, map[string]string{
		"sourceant/cli":       "9.9.9",
		"sourceant/agent":     "9.9.9",
		"sourceant/sourceant": "9.9.9",
	})
	t.Setenv("SOURCEANT_RELEASES_BASE", server.URL)
	t.Setenv("SOURCEANT_INSTALL_HOME", t.TempDir())
	Version = "1.0.0"
	t.Cleanup(func() { Version = "dev" })

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--agent", "http://127.0.0.1:1", "update", "--check"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exited %d: %s", code, stderr.String())
	}
	printed := stdout.String()
	for _, want := range []string{"cli", "agent", "core", "9.9.9", "can be"} {
		if !strings.Contains(printed, want) {
			t.Errorf("got %q, want it to name %q", printed, want)
		}
	}
}

func TestABuildOfYourOwnIsLeftAlone(t *testing.T) {
	server := forge(t, map[string]string{"sourceant/cli": "9.9.9"})
	t.Setenv("SOURCEANT_RELEASES_BASE", server.URL)
	t.Setenv("SOURCEANT_INSTALL_HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--agent", "http://127.0.0.1:1", "update", "cli"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exited %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "a build of your own is left alone") {
		t.Errorf("got %q", stdout.String())
	}
}

func TestNamingAVersionNeedsAPartToNameItFor(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"update", "--to", "1.2.3"}, &stdout, &stderr); code == 0 {
		t.Fatal("took a version without being told what it is for")
	}
	if !strings.Contains(stderr.String(), "name the part") {
		t.Errorf("got %q", stderr.String())
	}
}
