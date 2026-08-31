package command

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"

	"github.com/sourceant/cli/internal/install"
	"github.com/spf13/cobra"
)

func uiCommand(opts *options) *cobra.Command {
	var stayPut bool
	command := &cobra.Command{
		Use:   "ui",
		Short: "Open the graph in a browser",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Asking the agent first turns "the browser opened on an error
			// page" into a line saying the agent is not running.
			if _, err := opts.client().Status(cmd.Context()); err != nil {
				if err := start(cmd.Context(), opts, cmd.OutOrStdout()); err != nil {
					return err
				}
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), opts.agentURL)
			if stayPut {
				return nil
			}
			if err := open(opts.agentURL); err != nil {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Could not open a browser. Follow the address above.")
			}
			return nil
		},
	}
	command.Flags().BoolVar(&stayPut, "no-open", false, "Print the address instead of opening it")
	return command
}

// start runs the installed agent and waits for it to answer. It outlives this
// process, because the agent is the thing that stays up.
func start(ctx context.Context, opts *options, out interface{ Write([]byte) (int, error) }) error {
	path := install.AgentPath()
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("no agent is running and none is installed here. Run sourceant setup")
	}

	logPath := install.Home() + "/agent.log"
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer log.Close()

	agent := exec.Command(path)
	agent.Stdout = log
	agent.Stderr = log
	agent.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := agent.Start(); err != nil {
		return fmt.Errorf("could not start the agent: %w", err)
	}
	_, _ = fmt.Fprintf(out, "Started the agent. It logs to %s\n", logPath)

	// The agent has to start the core before it answers, which is the slow
	// part on a first run.
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := opts.client().Status(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("the agent did not answer within 90s. See %s", logPath)
}

// open hands a URL to whatever the desktop uses for one.
func open(target string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		name = "xdg-open"
	}
	return exec.Command(name, append(args, target)...).Start()
}
