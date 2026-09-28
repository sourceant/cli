package command

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMCPCommandUsesTheAgentAndKeepsStdoutAsProtocol(t *testing.T) {
	wire := fixture(t, "mcp-initialize.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp/" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Accept") != "application/json, text/event-stream" {
			t.Error("MCP response formats were not negotiated")
		}
		var request rpcMessage
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if request.Method == "notifications/initialized" {
			if r.Header.Get("MCP-Protocol-Version") != "2025-06-18" {
				t.Error("negotiated protocol version was dropped")
			}
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(wire)
	}))
	defer server.Close()
	t.Setenv("SOURCEANT_UI_URL", "")
	command := mcpCommand(&options{agentURL: server.URL, timeout: time.Second})
	command.SetArgs([]string{})
	command.SetIn(strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2025-06-18\",\"capabilities\":{},\"clientInfo\":{\"name\":\"capability-check\",\"version\":\"1\"}}}\n{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n"))
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&stdout)
	var response rpcMessage
	if err := decoder.Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Result.ProtocolVersion != "2025-06-18" {
		t.Fatalf("unexpected response: %+v", response)
	}
	if err := decoder.Decode(&response); err != io.EOF {
		t.Fatalf("unexpected extra stdout: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", &stderr)
	}
}
