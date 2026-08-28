// Package command is the sourceant command tree.
package command

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/sourceant/cli/internal/agent"
	"github.com/sourceant/cli/internal/presentation"
	"github.com/spf13/cobra"
)

// Set at build time. See the Makefile.
var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

// EnvAgent points the CLI at an agent somewhere other than the default.
const EnvAgent = "SOURCEANT_AGENT_URL"

// DefaultAgent is where the agent listens unless it was told otherwise.
const DefaultAgent = "http://127.0.0.1:8930"

type options struct {
	agentURL string
	timeout  time.Duration
	asJSON   bool
}

// Run executes the command tree and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	opts := &options{}
	root := &cobra.Command{
		Use:           "sourceant",
		Short:         "Your code, indexed on this machine",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(args)

	root.PersistentFlags().StringVar(&opts.agentURL, "agent", agentDefault(), "The agent to talk to")
	root.PersistentFlags().DurationVar(&opts.timeout, "timeout", 30*time.Second, "How long to wait for the agent")
	root.PersistentFlags().BoolVar(&opts.asJSON, "json", false, "Print the agent's answer as JSON")

	root.AddCommand(
		statusCommand(opts),
		reposCommand(opts),
		graphCommand(opts),
		uiCommand(opts),
		versionCommand(),
	)

	if err := root.Execute(); err != nil {
		_, _ = fmt.Fprintln(stderr, "sourceant:", message(err))
		return 1
	}
	return 0
}

// message turns an error into the one line a person needs, which for an agent
// that is not running is what to do about it rather than the socket error.
func message(err error) string {
	var unreachable *agent.Unreachable
	if errors.As(err, &unreachable) {
		return fmt.Sprintf("no agent answering at %s. Start it with sourceant-agent", unreachable.BaseURL)
	}
	return err.Error()
}

func agentDefault() string {
	if value := os.Getenv(EnvAgent); value != "" {
		return value
	}
	return DefaultAgent
}

func (o *options) client() *agent.Client {
	return agent.New(o.agentURL, o.timeout)
}

func statusCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Whether the agent and the indexer are running",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			status, err := opts.client().Status(cmd.Context())
			if err != nil {
				return err
			}
			if opts.asJSON {
				return writeJSON(cmd.OutOrStdout(), status)
			}
			rows := [][]string{
				{"agent", opts.agentURL, "version " + status.Version},
				{"indexer", status.CoreURL, upOrDown(status.CoreUp)},
			}
			if status.CoreStarts > 1 {
				rows = append(rows, []string{"", "", presentation.Count(status.CoreStarts, "start", "starts")})
			}
			if status.LastExit != "" {
				rows = append(rows, []string{"", "", "last exit: " + status.LastExit})
			}
			presentation.Table(cmd.OutOrStdout(), nil, rows)
			return nil
		},
	}
}

func upOrDown(up bool) string {
	if up {
		return "answering"
	}
	return "not answering"
}

func reposCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:     "repos",
		Aliases: []string{"repositories"},
		Short:   "Repositories indexed on this machine",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			repositories, err := opts.client().Repositories(cmd.Context())
			if err != nil {
				return err
			}
			if opts.asJSON {
				return writeJSON(cmd.OutOrStdout(), repositories)
			}
			if len(repositories) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Nothing indexed yet. Add a repository with: sourceant repo add <path>")
				return nil
			}
			rows := make([][]string, 0, len(repositories))
			for _, repository := range repositories {
				rows = append(rows, []string{repository.Name, repository.Path})
			}
			presentation.Table(cmd.OutOrStdout(), []string{"REPOSITORY", "PATH"}, rows)
			return nil
		},
	}
}

func graphCommand(opts *options) *cobra.Command {
	var (
		pathPrefix   string
		includeTests bool
		nodeLimit    int
	)
	command := &cobra.Command{
		Use:   "graph <repository>",
		Short: "What the indexer found in one repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			graph, err := opts.client().Graph(cmd.Context(), args[0], agent.GraphOptions{
				PathPrefix:   pathPrefix,
				IncludeTests: includeTests,
				NodeLimit:    nodeLimit,
			})
			if err != nil {
				return err
			}
			if opts.asJSON {
				return writeJSON(cmd.OutOrStdout(), graph)
			}
			summarise(cmd.OutOrStdout(), graph)
			return nil
		},
	}
	command.Flags().StringVar(&pathPrefix, "path", "", "Only what sits under this directory")
	command.Flags().BoolVar(&includeTests, "tests", false, "Include the test suite")
	command.Flags().IntVar(&nodeLimit, "limit", 0, "Stop after this many nodes")
	return command
}

// summarise prints what a terminal can say about a graph it cannot draw.
func summarise(out io.Writer, graph agent.Graph) {
	_, _ = fmt.Fprintf(out, "%s, %s\n\n",
		presentation.Count(len(graph.Nodes), "node", "nodes"),
		presentation.Count(len(graph.Links), "link", "links"))

	presentation.Table(out, []string{"KIND", "COUNT"}, tally(kinds(graph)))
	if len(graph.Links) > 0 {
		_, _ = fmt.Fprintln(out)
		presentation.Table(out, []string{"EDGE", "COUNT"}, tally(edgeTypes(graph)))
	}
	if graph.Truncated {
		_, _ = fmt.Fprintln(out, "\nThis repository is larger than the limit, so this is part of it. Raise --limit to see more.")
	}
}

func kinds(graph agent.Graph) map[string]int {
	counts := map[string]int{}
	for _, node := range graph.Nodes {
		counts[node.Kind]++
	}
	return counts
}

func edgeTypes(graph agent.Graph) map[string]int {
	counts := map[string]int{}
	for _, link := range graph.Links {
		counts[link.Type]++
	}
	return counts
}

// tally orders by count, then by name, so the same graph always prints the same.
func tally(counts map[string]int) [][]string {
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})
	rows := make([][]string, 0, len(names))
	for _, name := range names {
		rows = append(rows, []string{name, fmt.Sprint(counts[name])})
	}
	return rows
}

func versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "What this build is",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "sourceant %s (%s, built %s)\n", Version, GitCommit, BuildTime)
			return nil
		},
	}
}

func writeJSON(out io.Writer, body any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(body)
}
