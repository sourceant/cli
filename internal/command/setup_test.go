package command

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupVersionsAreIndependent(t *testing.T) {
	previous := Version
	Version = "unrelated-cli-version"
	t.Cleanup(func() { Version = previous })
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"docker default", []string{"--runtime", "docker"}, "ghcr.io/sourceant/sourceant:latest"},
		{"docker pinned", []string{"--runtime", "docker", "--core-version", "1.0.0-beta.2"}, "ghcr.io/sourceant/sourceant:v1.0.0-beta.2"},
		{"docker custom", []string{"--runtime", "docker", "--image", "custom:local"}, "custom:local"},
		{"python default", []string{"--runtime", "python"}, "v1.0.0-beta.2/sourceant-1.0.0b2-py3-none-any.whl"},
		{"python pinned", []string{"--runtime", "python", "--core-version", "v1.0.0-beta.2"}, "v1.0.0-beta.2/sourceant-1.0.0b2-py3-none-any.whl"},
		{"python custom", []string{"--runtime", "python", "--from", "./local-core"}, "./local-core"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("SOURCEANT_INSTALL_HOME", home)
			t.Setenv("SOURCEANT_HOME", filepath.Join(home, "data"))
			t.Setenv("CALLS", filepath.Join(home, "calls"))
			bin := filepath.Join(home, "bin")
			if err := os.MkdirAll(bin, 0755); err != nil {
				t.Fatal(err)
			}
			for name, body := range map[string]string{
				"docker":  "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$CALLS\"\n",
				"python3": "#!/bin/sh\nexit 0\n",
			} {
				if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0755); err != nil {
					t.Fatal(err)
				}
			}
			runtimeBin := filepath.Join(home, "runtime", "bin")
			if err := os.MkdirAll(runtimeBin, 0755); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"pip", "sourceant"} {
				if err := os.WriteFile(filepath.Join(runtimeBin, name), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$CALLS\"\n"), 0755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/latest" {
					t.Errorf("unexpected lookup: %s", r.URL)
					http.NotFound(w, r)
					return
				}
				data, err := os.ReadFile("testdata/releases/core.json")
				if err != nil {
					t.Error(err)
					return
				}
				_, _ = w.Write(data)
			}))
			defer server.Close()
			t.Setenv("SOURCEANT_CORE_API_BASE", server.URL)
			var out, stderr bytes.Buffer
			if code := Run(append([]string{"setup", "--no-agent"}, tc.args...), &out, &stderr); code != 0 {
				t.Fatalf("%s", stderr.String())
			}
			calls, err := os.ReadFile(filepath.Join(home, "calls"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(calls), tc.want) {
				t.Fatalf("wanted %s in %s", tc.want, calls)
			}
		})
	}
}

func TestSetupResolvesAgentIndependently(t *testing.T) {
	previous := Version
	Version = "unrelated-cli-version"
	t.Cleanup(func() { Version = previous })
	for _, pinned := range []bool{false, true} {
		t.Run(map[bool]string{false: "latest", true: "pinned"}[pinned], func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("SOURCEANT_INSTALL_HOME", home)
			t.Setenv("SOURCEANT_HOME", filepath.Join(home, "data"))
			if err := os.WriteFile(filepath.Join(home, "docker"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", home+string(os.PathListSeparator)+os.Getenv("PATH"))
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.RequestURI())
				if r.URL.RawQuery == "per_page=1" {
					data, err := os.ReadFile("testdata/releases/agent.json")
					if err != nil {
						t.Error(err)
						return
					}
					_, _ = w.Write(data)
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			t.Setenv("SOURCEANT_API_BASE", server.URL)
			t.Setenv("SOURCEANT_DOWNLOAD_BASE", server.URL)
			args := []string{"setup", "--runtime", "docker"}
			if pinned {
				args = append(args, "--agent-version", "v1.0.0-beta.2")
			}
			var out, stderr bytes.Buffer
			if Run(args, &out, &stderr) == 0 {
				t.Fatal("download should fail")
			}
			if !strings.Contains(stderr.String(), "could not fetch the agent") {
				t.Fatal(stderr.String())
			}
			if !strings.Contains(paths[len(paths)-1], "/v1.0.0-beta.2/sourceant-agent-1.0.0-beta.2-") {
				t.Fatal(paths)
			}
			if pinned && len(paths) != 1 {
				t.Fatal(paths)
			}
			if !pinned && (len(paths) != 3 || paths[0] != "/latest" || paths[1] != "/?per_page=1") {
				t.Fatal(paths)
			}
		})
	}
}

func TestInstallerResolvesPrerelease(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		if r.URL.RawQuery == "per_page=1" {
			data, err := os.ReadFile("testdata/releases/cli.json")
			if err != nil {
				t.Error(err)
				return
			}
			_, _ = w.Write(data)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	t.Setenv("SOURCEANT_VERSION", "latest")
	t.Setenv("SOURCEANT_API_BASE", server.URL)
	t.Setenv("SOURCEANT_DOWNLOAD_BASE", server.URL)
	cmd := exec.Command("sh", "../../scripts/install.sh")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("download should fail")
	}
	if !strings.Contains(string(out), "could not download") {
		t.Fatal(string(out))
	}
	if len(paths) != 3 || paths[0] != "/latest" || paths[1] != "/?per_page=1" || !strings.Contains(paths[2], "/v1.0.0-beta.2/sourceant-1.0.0-beta.2-") {
		t.Fatal(paths)
	}
}

func TestSetupRejectsConflictingCoreSelection(t *testing.T) {
	for _, flag := range []string{"--image", "--from"} {
		var out, stderr bytes.Buffer
		if Run([]string{"setup", "--core-version", "1.0.0-beta.2", flag, "custom"}, &out, &stderr) == 0 {
			t.Fatal("accepted conflicting selectors")
		}
		if !strings.Contains(stderr.String(), "none of the others can be") {
			t.Fatal(stderr.String())
		}
	}
}
