package command

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/sourceant/cli/internal/presentation"
	"github.com/spf13/cobra"
)

func architectureCommand(opts *options) *cobra.Command {
	var depth int
	var includeTests bool
	var baselinePath string
	command := &cobra.Command{
		Use: "architecture <repository>", Short: "Read components and their dependencies from the local index", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if depth < 1 || depth > 4 {
				return fmt.Errorf("depth must be between 1 and 4")
			}
			var data json.RawMessage
			var err error
			if baselinePath == "" {
				data, err = opts.client().Architecture(cmd.Context(), args[0], depth, includeTests)
			} else {
				if cmd.Flags().Changed("depth") || cmd.Flags().Changed("tests") {
					return fmt.Errorf("a comparison uses the baseline's depth and test selection")
				}
				file, openErr := os.Open(baselinePath)
				if openErr != nil {
					return openErr
				}
				defer func() { _ = file.Close() }()
				baseline, readErr := io.ReadAll(io.LimitReader(file, (8<<20)+1))
				if readErr != nil {
					return readErr
				}
				if len(baseline) > 8<<20 {
					return fmt.Errorf("baseline exceeds 8 MiB")
				}
				var header struct {
					Repository string `json:"repository"`
				}
				if json.Unmarshal(baseline, &header) != nil || header.Repository != args[0] {
					return fmt.Errorf("baseline must be a snapshot of %s", args[0])
				}
				data, err = opts.client().CompareArchitecture(cmd.Context(), baseline)
			}
			if err != nil {
				return err
			}
			if opts.asJSON {
				return writeJSON(cmd.OutOrStdout(), data)
			}
			return showArchitecture(cmd.OutOrStdout(), data, baselinePath != "")
		},
	}
	command.Flags().IntVar(&depth, "depth", 1, "Directory depth to group by (1 to 4)")
	command.Flags().BoolVar(&includeTests, "tests", false, "Include test code")
	command.Flags().StringVar(&baselinePath, "baseline", "", "Compare the current index with a previously exported JSON snapshot")
	return command
}

func showArchitecture(out io.Writer, data json.RawMessage, comparison bool) error {
	var result struct {
		Components []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Files    int    `json:"files"`
			Incoming int    `json:"incoming"`
			Outgoing int    `json:"outgoing"`
			Status   string `json:"status"`
		} `json:"components"`
		Relationships []struct {
			Source     string `json:"source"`
			Target     string `json:"target"`
			SourceName string `json:"source_name"`
			TargetName string `json:"target_name"`
			Type       string `json:"type"`
			Status     string `json:"status"`
		} `json:"relationships"`
		Coverage struct {
			Truncated  bool `json:"truncated"`
			Unplaced   int  `json:"unplaced_nodes"`
			Unresolved int  `json:"unresolved_edges"`
		} `json:"coverage"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return err
	}
	if comparison {
		_, _ = fmt.Fprintf(out, "%d changed components, %d changed relationships\n\n", len(result.Components), len(result.Relationships))
	} else {
		_, _ = fmt.Fprintf(out, "%d components, %d relationships in the current index\n\n", len(result.Components), len(result.Relationships))
	}
	rows := make([][]string, 0, len(result.Components))
	names := make(map[string]string, len(result.Components))
	for _, part := range result.Components {
		names[part.ID] = part.Name
		rows = append(rows, []string{part.Name, fmt.Sprint(part.Files), fmt.Sprint(part.Incoming), fmt.Sprint(part.Outgoing), part.Status})
	}
	presentation.Table(out, []string{"COMPONENT", "FILES", "INCOMING", "OUTGOING", "CHANGE"}, rows)
	if len(result.Relationships) > 0 {
		links := make([][]string, 0, len(result.Relationships))
		for _, edge := range result.Relationships {
			source, target := names[edge.Source], names[edge.Target]
			if comparison {
				source, target = edge.SourceName, edge.TargetName
			}
			links = append(links, []string{source, target, edge.Type, edge.Status})
		}
		_, _ = fmt.Fprintln(out)
		presentation.Table(out, []string{"FROM", "TO", "RELATIONSHIP", "CHANGE"}, links)
	}
	if result.Coverage.Truncated || result.Coverage.Unplaced > 0 || result.Coverage.Unresolved > 0 {
		_, _ = fmt.Fprintln(out, "\nThis index reading is incomplete and cannot establish architecture changes.")
	}
	return nil
}
