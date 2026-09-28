package command

import (
	"fmt"

	"github.com/spf13/cobra"
)

func startCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the agent and the indexer",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			if _, err := opts.plainClient().Status(cmd.Context()); err == nil {
				_, _ = fmt.Fprintf(out, "Already running at %s\n", opts.agentURL)
				return nil
			}
			target, err := ensureAgent(cmd.Context(), opts, out)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(out, target)
			return nil
		},
	}
}
