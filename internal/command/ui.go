package command

import (
	"fmt"
	"os/exec"
	"runtime"

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
				return err
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
