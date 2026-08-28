package command

import (
	"fmt"
	"os"

	"github.com/sourceant/cli/internal/install"
	"github.com/spf13/cobra"
)

func installCommand() *cobra.Command {
	var (
		runtime string
		image   string
		from    string
		noPull  bool
	)
	command := &cobra.Command{
		Use:   "install",
		Short: "Put a SourceAnt core on this machine",
		Long: "Two ways to have the core. As a container, which is what exists today. " +
			"Or as a Python program, for when the core is published as a package.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			config, err := install.Install(install.Options{
				Runtime: install.Runtime(runtime),
				Image:   image,
				From:    from,
				Pull:    !noPull,
				Out:     cmd.OutOrStdout(),
			}, install.Run)
			if err != nil {
				return err
			}

			path := install.ConfigPath()
			if err := os.MkdirAll(config.Core.DataDir, 0o755); err != nil {
				return fmt.Errorf("could not make %s, where the index lives: %w", config.Core.DataDir, err)
			}
			if err := install.Save(path, config); err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "\nInstalled %s\n", config.Core.Describe())
			_, _ = fmt.Fprintf(out, "Index at   %s\n", config.Core.DataDir)
			_, _ = fmt.Fprintf(out, "Written to %s\n\n", path)
			_, _ = fmt.Fprintln(out, "Start it with sourceant-agent, then sourceant ui.")
			return nil
		},
	}
	command.Flags().StringVar(&runtime, "runtime", string(install.Docker), "docker or python")
	command.Flags().StringVar(&image, "image", install.DefaultImage, "The container to use, for the docker runtime")
	command.Flags().StringVar(&from, "from", install.DefaultPackage, "What pip installs, for the python runtime")
	command.Flags().BoolVar(&noPull, "no-pull", false, "Use an image already on this machine")
	return command
}
