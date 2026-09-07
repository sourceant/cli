package command

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStopCommand(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusOK, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusInternalServerError} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" || r.URL.Path != "/api/stop" || r.Header.Get("X-Sourceant-Client") != "cli" {
				t.Errorf("unexpected stop request: %s %s", r.Method, r.URL)
			}
			w.WriteHeader(status)
		}))
		var out, stderr bytes.Buffer
		code := Run([]string{"--agent", server.URL, "stop"}, &out, &stderr)
		server.Close()
		if (code == 0) != (status == http.StatusNoContent) {
			t.Fatalf("status=%d code=%d: %s", status, code, stderr.String())
		}
		if code == 0 && !strings.Contains(out.String(), "Stopped SourceAnt.") {
			t.Fatal(out.String())
		}
	}
}

func TestStopAlreadyStopped(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	var out, stderr bytes.Buffer
	if Run([]string{"--agent", server.URL, "stop"}, &out, &stderr) != 0 {
		t.Fatal(stderr.String())
	}
	if !strings.Contains(out.String(), "already stopped") {
		t.Fatal(out.String())
	}
}
