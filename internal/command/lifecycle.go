package command

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"github.com/sourceant/cli/internal/agent"
	"github.com/sourceant/cli/internal/install"
)

// ports tried after the one asked for, when something else holds it.
const ports = 10

// ensureAgent answers with an agent that is running, starting one if it has to.
func ensureAgent(ctx context.Context, opts *options, out io.Writer) (string, error) {
	if _, err := agent.New(opts.agentURL, opts.timeout).Status(ctx); err == nil {
		return opts.agentURL, nil
	}
	// An address somebody named is the address they meant, so it is started there
	// or not at all.
	listen, err := address(opts.agentURL, !opts.agentNamed)
	if err != nil {
		return "", err
	}
	target := "http://" + listen
	if err := run(listen, out); err != nil {
		return "", err
	}
	if err := answering(ctx, target, opts.timeout); err != nil {
		return "", err
	}
	if !opts.agentNamed {
		if err := install.SaveAgentURL(target); err != nil {
			_, _ = fmt.Fprintf(out, "The agent is at %s, which could not be written down: %v\n", target, err)
		}
	}
	opts.agentURL = target
	return target, nil
}

// address is what to listen on, passing over a port something else holds.
func address(from string, move bool) (string, error) {
	parsed, err := url.Parse(from)
	if err != nil {
		return "", fmt.Errorf("%s is not an address: %w", from, err)
	}
	host, port := parsed.Hostname(), parsed.Port()
	if host == "" {
		host = "127.0.0.1"
	}
	first, err := strconv.Atoi(port)
	if err != nil {
		first = 8930
	}
	last := first
	if move {
		last = first + ports - 1
	}
	for candidate := first; candidate <= last; candidate++ {
		wanted := net.JoinHostPort(host, strconv.Itoa(candidate))
		listener, err := net.Listen("tcp", wanted)
		if err != nil {
			continue
		}
		_ = listener.Close()
		return wanted, nil
	}
	if !move {
		return net.JoinHostPort(host, strconv.Itoa(first)), nil
	}
	return "", fmt.Errorf("nothing is free between %d and %d on %s", first, last, host)
}

// run starts the installed agent so it outlives this process.
func run(listen string, out io.Writer) error {
	path := install.AgentPath()
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("no agent is running and none is installed here. Run sourceant setup")
	}
	logPath := install.Home() + "/agent.log"
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = log.Close() }()

	started := exec.Command(path)
	started.Env = append(os.Environ(), "SOURCEANT_AGENT_LISTEN="+listen)
	started.Stdout, started.Stderr = log, log
	started.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := started.Start(); err != nil {
		return fmt.Errorf("could not start the agent: %w", err)
	}
	_, _ = fmt.Fprintf(out, "Started the agent on %s. It logs to %s\n", listen, logPath)
	return nil
}

// answering waits for the agent to serve, which means waiting for the core it
// starts first.
func answering(ctx context.Context, target string, timeout time.Duration) error {
	client := agent.New(target, timeout)
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := client.Status(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("the agent did not answer within 90s. See %s/agent.log", install.Home())
}
