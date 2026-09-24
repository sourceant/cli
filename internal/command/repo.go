package command

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
)

func repoCommand(opts *options) *cobra.Command {
	var name string
	command := &cobra.Command{Use: "repo", Short: "Manage locally registered repositories"}
	add := &cobra.Command{
		Use: "add <path>", Short: "Register a repository with the local agent", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			repository, err := opts.client().Register(cmd.Context(), path, name)
			if err != nil {
				return err
			}
			if opts.asJSON {
				return writeJSON(cmd.OutOrStdout(), repository)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Registered %s at %s\n", repository.Name, repository.Path)
			return err
		},
	}
	add.Flags().StringVar(&name, "name", "", "Name used to address the repository")
	command.AddCommand(add)
	return command
}
