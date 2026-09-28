package command

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPortSomethingElseHoldsIsPassedOver(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()
	taken := "http://" + held.Addr().String()

	chosen, err := address(taken, true)
	if err != nil {
		t.Fatal(err)
	}
	if chosen == held.Addr().String() {
		t.Errorf("chose %s, which something else is already listening on", chosen)
	}
}

func TestAnAddressSomebodyNamedIsNotMoved(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()

	chosen, err := address("http://"+held.Addr().String(), false)
	if err != nil {
		t.Fatal(err)
	}
	if chosen != held.Addr().String() {
		t.Errorf("got %s, want the address that was named", chosen)
	}
}

func TestStartSaysItIsAlreadyRunning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"1.0.0","core_url":"http://127.0.0.1:1","core_up":true}`))
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--agent", server.URL, "start"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exited %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Already running at "+server.URL) {
		t.Errorf("got %q, want it to say what is already up", stdout.String())
	}
}

func TestStopDoesNotStartAnAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SOURCEANT_INSTALL_HOME", home)
	// An agent that would be started if anything tried to.
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(home, "bin", "sourceant-agent")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+filepath.Join(home, "started")+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	Run([]string{"--agent", "http://127.0.0.1:1", "stop"}, &stdout, &stderr)

	if _, err := os.Stat(filepath.Join(home, "started")); err == nil {
		t.Error("stop started the agent it was asked to stop")
	}
}
