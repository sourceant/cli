package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recorder stands in for running real commands, so an install can be driven
// without pulling 1.4 GB on every test run.
type recorder struct {
	ran    [][]string
	fail   map[string]error
	output []byte
}

func (r *recorder) run(name string, args ...string) ([]byte, error) {
	r.ran = append(r.ran, append([]string{name}, args...))
	if err, found := r.fail[name+" "+strings.Join(args, " ")]; found {
		return r.output, err
	}
	for key, err := range r.fail {
		if strings.HasPrefix(name+" "+strings.Join(args, " "), key) {
			return r.output, err
		}
	}
	return nil, nil
}

func (r *recorder) commands() string {
	lines := make([]string, 0, len(r.ran))
	for _, command := range r.ran {
		lines = append(lines, strings.Join(command, " "))
	}
	return strings.Join(lines, "\n")
}

/* The agent reads this file. These are its field names, spelled out, because
 * renaming one here and not there leaves an agent that starts nothing and says
 * only that no runtime is installed. */
func TestTheFileMatchesWhatTheAgentReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	err := Save(path, Config{Core: Core{
		Runtime: Docker,
		Command: "/somewhere/sourceant",
		Image:   "ghcr.io/sourceant/sourceant:v1",
		DataDir: "/data",
		Mount:   "/home/someone",
		User:    "1000:1000",
	}})
	if err != nil {
		t.Fatalf("saving: %v", err)
	}

	var raw map[string]map[string]any
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("reading back: %v", err)
	}

	core, found := raw["core"]
	if !found {
		t.Fatalf("no core in %s", data)
	}
	for field, want := range map[string]any{
		"runtime":  "docker",
		"command":  "/somewhere/sourceant",
		"image":    "ghcr.io/sourceant/sourceant:v1",
		"data_dir": "/data",
		"mount":    "/home/someone",
		"user":     "1000:1000",
	} {
		if core[field] != want {
			t.Errorf("core.%s is %v, want %v", field, core[field], want)
		}
	}
}

func TestInstallingTheImagePullsItAndRecordsWhoIsInstalling(t *testing.T) {
	t.Setenv("SOURCEANT_INSTALL_HOME", t.TempDir())
	t.Setenv("SOURCEANT_HOME", "/somewhere/data")
	run := &recorder{}

	config, err := Install(Options{Runtime: Docker, Image: "img:1", Pull: true}, run.run)
	if err != nil {
		t.Fatalf("installing: %v", err)
	}

	if config.Core.Runtime != Docker || config.Core.Image != "img:1" {
		t.Errorf("got %+v, want the image asked for", config.Core)
	}
	if config.Core.DataDir != "/somewhere/data" {
		t.Errorf("got data dir %q, want the core's own", config.Core.DataDir)
	}
	if config.Core.User != fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()) {
		t.Errorf("got user %q, want whoever is installing", config.Core.User)
	}
	if !strings.Contains(run.commands(), "docker pull img:1") {
		t.Errorf("the image was never pulled:\n%s", run.commands())
	}
}

func TestNoPullUsesAnImageAlreadyHere(t *testing.T) {
	t.Setenv("SOURCEANT_INSTALL_HOME", t.TempDir())
	run := &recorder{}

	if _, err := Install(Options{Runtime: Docker, Image: "img:1", Pull: false}, run.run); err != nil {
		t.Fatalf("installing: %v", err)
	}

	if strings.Contains(run.commands(), "docker pull") {
		t.Errorf("pulled anyway:\n%s", run.commands())
	}
	if !strings.Contains(run.commands(), "docker image inspect img:1") {
		t.Errorf("never checked the image is here:\n%s", run.commands())
	}
}

func TestNoDockerSaysWhatToDoInstead(t *testing.T) {
	t.Setenv("SOURCEANT_INSTALL_HOME", t.TempDir())
	run := &recorder{fail: map[string]error{"docker version": errors.New("not found")}}

	_, err := Install(Options{Runtime: Docker, Pull: true}, run.run)

	if err == nil {
		t.Fatal("installed without docker")
	}
	if !strings.Contains(err.Error(), "python runtime instead") {
		t.Errorf("got %q, want the other way out", err)
	}
}

func TestAnImageThatIsNotHereAndWasNotPulledIsRefused(t *testing.T) {
	t.Setenv("SOURCEANT_INSTALL_HOME", t.TempDir())
	run := &recorder{fail: map[string]error{"docker image inspect": errors.New("no such image")}}

	_, err := Install(Options{Runtime: Docker, Image: "img:1", Pull: false}, run.run)

	if err == nil {
		t.Fatal("recorded an image that is not on this machine")
	}
	if !strings.Contains(err.Error(), "--no-pull") {
		t.Errorf("got %q, want how to fetch it", err)
	}
}

/* The core has no packaging metadata and nothing is on PyPI, so this path
 * cannot work yet. What matters is that it says so rather than recording a
 * runtime the agent would fail to start. */
func TestThePythonRuntimeSaysWhyItCannotWorkYet(t *testing.T) {
	t.Setenv("SOURCEANT_INSTALL_HOME", t.TempDir())
	run := &recorder{}

	_, err := Install(Options{Runtime: Python, From: "sourceant"}, run.run)

	if err == nil {
		t.Fatal("recorded a python runtime with no command behind it")
	}
	if !strings.Contains(err.Error(), "not packaged for PyPI yet") {
		t.Errorf("got %q, want why it cannot work", err)
	}
	if !strings.Contains(err.Error(), "--runtime docker") {
		t.Errorf("got %q, want the way that does work", err)
	}
}

func TestARuntimeThatIsNeitherIsRefused(t *testing.T) {
	if _, err := Install(Options{Runtime: "podman"}, (&recorder{}).run); err == nil {
		t.Fatal("accepted a runtime that is neither")
	}
}
