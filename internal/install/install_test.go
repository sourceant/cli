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

/* A pip install can succeed and still leave nothing to run. What matters is
 * that it says so rather than recording a runtime the agent would fail to
 * start. */
func TestThePythonRuntimeRefusesAnInstallThatLeftNoCommand(t *testing.T) {
	t.Setenv("SOURCEANT_INSTALL_HOME", t.TempDir())
	run := &recorder{}

	_, err := Install(Options{Runtime: Python, From: "sourceant"}, run.run)

	if err == nil {
		t.Fatal("recorded a python runtime with no command behind it")
	}
	if !strings.Contains(err.Error(), "without a sourceant command") {
		t.Errorf("got %q, want what was wrong with it", err)
	}
	if !strings.Contains(err.Error(), "sourceant") {
		t.Errorf("got %q, want what it tried to install", err)
	}
}

func TestThePythonRuntimeTakesTheWheelThisVersionPublished(t *testing.T) {
	t.Setenv("SOURCEANT_INSTALL_HOME", t.TempDir())
	run := &recorder{}

	_, _ = Install(Options{Runtime: Python, Version: "1.0.0-beta.2"}, run.run)

	var installed string
	for _, call := range run.ran {
		for _, arg := range call {
			if strings.HasSuffix(arg, ".whl") {
				installed = arg
			}
		}
	}
	// Pip normalises the version, so the tag and the file name differ.
	want := "/v1.0.0-beta.2/sourceant-1.0.0b2-py3-none-any.whl"
	if !strings.HasSuffix(installed, want) {
		t.Errorf("installed %q, want it to end in %q", installed, want)
	}
}

func TestARuntimeThatIsNeitherIsRefused(t *testing.T) {
	if _, err := Install(Options{Runtime: "podman"}, (&recorder{}).run); err == nil {
		t.Fatal("accepted a runtime that is neither")
	}
}

func TestTheRuntimeIsChosenByWhatIsHere(t *testing.T) {
	if got := Chosen((&recorder{}).run); got != Docker {
		t.Errorf("chose %q where docker answers, want docker", got)
	}

	noDocker := &recorder{fail: map[string]error{"docker version": errors.New("not found")}}
	if got := Chosen(noDocker.run); got != Python {
		t.Errorf("chose %q where docker is absent, want python", got)
	}
}
