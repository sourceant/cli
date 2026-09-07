package command

import (
	"fmt"

	"github.com/sourceant/cli/internal/agent"
	"github.com/spf13/cobra"
)

func stopCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the agent and its core",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.client().Stop(cmd.Context()); err != nil {
				if agent.IsConnectionRefused(err) {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "SourceAnt is already stopped.")
					return nil
				}
				return fmt.Errorf("could not stop SourceAnt: %w", err)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Stopped SourceAnt.")
			return nil
		},
	}
}
