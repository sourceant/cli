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
			out := cmd.OutOrStdout()
			// Asked first, so a browser does not open on an error page.
			target := opts.agentURL
			if _, err := opts.plainClient().Status(cmd.Context()); err != nil {
				started, err := ensureAgent(cmd.Context(), opts, out)
				if err != nil {
					return err
				}
				target = started
			}
			_, _ = fmt.Fprintln(out, target)
			if stayPut {
				return nil
			}
			if err := open(target); err != nil {
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
