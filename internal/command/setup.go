package command

import (
	"fmt"
	"os"

	"github.com/sourceant/cli/internal/install"
	"github.com/spf13/cobra"
)

func setupCommand() *cobra.Command {
	var (
		runtime string
		image   string
		from    string
		noPull  bool
		noAgent bool
	)
	command := &cobra.Command{
		Use:   "setup",
		Short: "Set this machine up to run SourceAnt",
		Long: "Installs the agent and a core, so the only thing anybody had to " +
			"fetch by hand is this command. Two ways to have the core. As a " +
			"container where there is Docker, and as a Python program where " +
			"there is not. Name a runtime to decide it yourself.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Nobody should have to know which runtime they have. Asked for
			// neither, take the container where Docker answers.
			if !cmd.Flags().Changed("runtime") {
				runtime = string(install.Chosen(install.Run))
			}
			config, err := install.Install(install.Options{
				Runtime: install.Runtime(runtime),
				Image:   image,
				From:    from,
				Pull:    !noPull,
				Version: Version,
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

			if !noAgent {
				agentPath, err := install.InstallAgent(Version, install.Get, out)
				if err != nil {
					return fmt.Errorf("the core is installed, but the agent is not: %w", err)
				}
				_, _ = fmt.Fprintf(out, "Agent at   %s\n\n", agentPath)
			}

			_, _ = fmt.Fprintln(out, "Run sourceant ui to start it and open the view.")
			return nil
		},
	}
	command.Flags().StringVar(&runtime, "runtime", string(install.Docker), "docker or python. Chosen for you when not named")
	command.Flags().StringVar(&image, "image", install.DefaultImage, "The container to use, for the docker runtime")
	command.Flags().StringVar(&from, "from", "", "What pip installs, for the python runtime. Defaults to the wheel this version published")
	command.Flags().BoolVar(&noPull, "no-pull", false, "Use an image already on this machine")
	command.Flags().BoolVar(&noAgent, "no-agent", false, "Leave the agent alone, install only the core")
	return command
}
