package command

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/sourceant/cli/internal/install"
	"github.com/sourceant/cli/internal/presentation"
	"github.com/sourceant/cli/internal/update"
	"github.com/spf13/cobra"
)

func updateCommand(opts *options) *cobra.Command {
	var (
		checkOnly  bool
		prerelease bool
		to         string
	)
	command := &cobra.Command{
		Use:       "update [cli|agent|core]...",
		Short:     "Bring this machine up to the current release",
		Long:      "Updates this command, the agent and the core. Name parts to update only those. Nothing is replaced until its checksum matches the release it came from.",
		ValidArgs: []string{"cli", "agent", "core"},
		Args:      cobra.OnlyValidArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			wanted := map[string]bool{}
			for _, name := range args {
				wanted[name] = true
			}
			// Named nothing means all of it: somebody asking to update wants a
			// machine that is up to date, not a choice of three.
			if len(wanted) == 0 {
				wanted = map[string]bool{"cli": true, "agent": true, "core": true}
			}
			if to != "" && len(wanted) != 1 {
				return fmt.Errorf("--to names a version of one part, so name the part: sourceant update agent --to %s", to)
			}
			// A machine on a prerelease keeps up with prereleases, whether or
			// not anybody asks: the alternative is telling somebody on beta.4
			// that beta.5 does not exist.
			if strings.Contains(Version, "-") {
				prerelease = true
			}

			rows := make([][]string, 0, len(wanted))
			if wanted["cli"] {
				rows = append(rows, part(cmd.Context(), "cli", install.CLIRepo, Version, cliArchive, replaceSelf, to, prerelease, checkOnly, out))
			}
			if wanted["agent"] {
				rows = append(rows, agentRow(cmd.Context(), opts, to, prerelease, checkOnly, out))
			}
			if wanted["core"] {
				rows = append(rows, coreRow(cmd.Context(), to, prerelease, checkOnly, out))
			}
			presentation.Table(out, []string{"PART", "HAVE", "AVAILABLE", ""}, rows)
			return nil
		},
	}
	command.Flags().BoolVar(&checkOnly, "check", false, "Say what is available and change nothing")
	command.Flags().BoolVar(&prerelease, "prerelease", false, "Include prereleases")
	command.Flags().StringVar(&to, "to", "", "A version other than the newest, for the part named")
	return command
}

// part is one component's row: what is installed, what is published, and what
// was done about it.
func part(ctx context.Context, name, repo, have, archive string, replace func([]byte) error, to string, prerelease, checkOnly bool, out io.Writer) []string {
	release, err := published(ctx, repo, to, prerelease)
	if err != nil {
		return []string{name, have, "", err.Error()}
	}
	there := release.Version()
	if have == "dev" {
		return []string{name, have, there, "a build of your own is left alone"}
	}
	newer, err := update.Newer(there, have)
	if err != nil {
		return []string{name, have, there, err.Error()}
	}
	if !newer && to == "" {
		return []string{name, have, there, "current"}
	}
	if checkOnly {
		return []string{name, have, there, "can be updated"}
	}
	binary, err := update.Binary(ctx, update.Read, release, fmt.Sprintf(archive, there, runtime.GOOS, runtime.GOARCH))
	if err != nil {
		return []string{name, have, there, err.Error()}
	}
	if err := replace(binary); err != nil {
		return []string{name, have, there, err.Error()}
	}
	return []string{name, have, there, "updated"}
}

const cliArchive = "sourceant-%s-%s-%s.tar.gz"

// replaceSelf writes over the command that is running, which is safe on a
// rename: this process keeps the file it opened and the next one gets the new
// binary.
func replaceSelf(binary []byte) error {
	path, err := install.Self()
	if err != nil {
		return err
	}
	return update.Replace(path, binary)
}

const agentArchive = "sourceant-agent-%s-%s-%s.tar.gz"

func agentRow(ctx context.Context, opts *options, to string, prerelease, checkOnly bool, out io.Writer) []string {
	have := "not installed"
	if status, err := opts.plainClient().Status(ctx); err == nil {
		have = status.Version
	} else if _, err := os.Stat(install.AgentPath()); err == nil {
		have = "unknown"
	}
	replace := func(binary []byte) error {
		// Stopped first: a running agent holds the file it was started from,
		// and the new one only runs once something restarts it.
		running := false
		if _, err := opts.plainClient().Status(ctx); err == nil {
			running = true
			_ = opts.plainClient().Stop(ctx)
		}
		if err := update.Replace(install.AgentPath(), binary); err != nil {
			return err
		}
		if !running {
			return nil
		}
		_, err := ensureAgent(ctx, opts, out)
		return err
	}
	if have == "not installed" || have == "unknown" {
		release, err := published(ctx, install.AgentRepo, to, prerelease)
		if err != nil {
			return []string{"agent", have, "", err.Error()}
		}
		there := release.Version()
		if checkOnly {
			return []string{"agent", have, there, "can be installed"}
		}
		binary, err := update.Binary(ctx, update.Read, release, fmt.Sprintf(agentArchive, there, runtime.GOOS, runtime.GOARCH))
		if err != nil {
			return []string{"agent", have, there, err.Error()}
		}
		if err := replace(binary); err != nil {
			return []string{"agent", have, there, err.Error()}
		}
		return []string{"agent", have, there, "installed"}
	}
	return part(ctx, "agent", install.AgentRepo, have, agentArchive, replace, to, prerelease, checkOnly, out)
}

// coreRow updates the core through the installer, which knows whether this
// machine runs it in a container or as a Python program.
func coreRow(ctx context.Context, to string, prerelease, checkOnly bool, out io.Writer) []string {
	release, err := published(ctx, install.CoreRepo, to, prerelease)
	if err != nil {
		return []string{"core", "", "", err.Error()}
	}
	there := release.Version()
	config, err := install.Load(install.ConfigPath())
	if err != nil {
		return []string{"core", "", there, err.Error()}
	}
	have := config.Core.Describe()
	if config.Core.Runtime == "" {
		have = "not installed"
	}
	if checkOnly {
		return []string{"core", have, there, installedOrUpdated(have, "can be")}
	}
	written, err := install.Install(install.Options{
		Runtime: config.Core.Runtime,
		Image:   config.Core.Image,
		Pull:    true,
		Version: there,
		Out:     io.Discard,
	}, install.Run)
	if err != nil {
		return []string{"core", have, there, err.Error()}
	}
	written.Agent = config.Agent
	if err := install.Save(install.ConfigPath(), written); err != nil {
		return []string{"core", have, there, err.Error()}
	}
	return []string{"core", have, there, installedOrUpdated(have, "")}
}

func published(ctx context.Context, repo, to string, prerelease bool) (update.Release, error) {
	if to != "" {
		return update.Named(ctx, update.Read, repo, to)
	}
	return update.Latest(ctx, update.Read, repo, prerelease)
}

// installedOrUpdated says which of the two words fits what was there before.
func installedOrUpdated(have, prefix string) string {
	word := "updated"
	if have == "not installed" {
		word = "installed"
	}
	if prefix == "" {
		return word
	}
	return prefix + " " + word
}
