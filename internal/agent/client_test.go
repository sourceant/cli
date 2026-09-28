package agent

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func dead(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	return "http://" + address
}

func TestACallThatFindsNoAgentStartsOneAndAsksAgain(t *testing.T) {
	answers := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		answers++
		_, _ = w.Write([]byte(`{"version":"1.0.0","core_url":"http://127.0.0.1:1","core_up":true}`))
	}))
	defer server.Close()

	client := New(dead(t), 5*time.Second)
	started := 0
	StartWith(client, func(context.Context) (string, error) {
		started++
		return server.URL, nil
	})

	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("the call failed after starting the agent: %v", err)
	}
	if status.Version != "1.0.0" || answers != 1 || started != 1 {
		t.Errorf("version %q, %d answers, %d starts", status.Version, answers, started)
	}
	if client.BaseURL() != server.URL {
		t.Errorf("client still points at %s", client.BaseURL())
	}
}

func TestWithoutAStarterAnAbsentAgentIsStillAnError(t *testing.T) {
	client := New(dead(t), time.Second)

	if _, err := client.Status(context.Background()); !IsConnectionRefused(err) {
		t.Errorf("got %v, want the refusal", err)
	}
}

func TestAnAgentThatCannotBeStartedReportsTheOriginalRefusal(t *testing.T) {
	client := New(dead(t), time.Second)
	StartWith(client, func(context.Context) (string, error) {
		return "", context.DeadlineExceeded
	})

	if _, err := client.Status(context.Background()); !IsConnectionRefused(err) {
		t.Errorf("got %v, want the refusal", err)
	}
}
