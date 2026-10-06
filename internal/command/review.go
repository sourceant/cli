package command

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sourceant/cli/internal/agent"
	"github.com/sourceant/cli/internal/presentation"
	"github.com/spf13/cobra"
)

// How often to ask whether a review has finished, and how long to keep asking.
var (
	beat     = 2 * time.Second
	patience = 10 * time.Minute
)

func reviewCommand(opts *options) *cobra.Command {
	var (
		against         string
		title           string
		skills          []string
		noWait          bool
		noModel         bool
		repoPath        string
		base            string
		head            string
		descriptionFile string
		remote          snapshotOptions
		format          string
	)
	command := &cobra.Command{
		Use:   "review [path]",
		Short: "Read the work in a checkout before anyone else has to",
		Long: "Read what a checkout has that its default branch does not, whether " +
			"it is committed or not, and say whether it is ready to propose.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if format != "" && format != "json" && format != "text" {
				return fmt.Errorf("format must be json or text")
			}
			if format != "" {
				opts.asJSON = format == "json"
			}
			folder := "."
			if len(args) == 1 {
				folder = args[0]
			}
			if repoPath != "" {
				if len(args) != 0 {
					return fmt.Errorf("use either --repo or a positional path")
				}
				folder = repoPath
			}
			if remote.endpoint != "" {
				if against != "" || noWait || noModel || len(skills) != 0 || descriptionFile != "" {
					return fmt.Errorf("remote snapshots require committed input and --pr-metadata for context")
				}
				remote.base, remote.head, remote.title = base, head, title
				return remoteReview(cmd, opts, folder, remote)
			}
			if remote.repository != "" || remote.diffFile != "" || remote.metadataFile != "" || len(remote.configuration) != 0 {
				return fmt.Errorf("snapshot options require --reviewer or SOURCEANT_REVIEW_URL")
			}
			if base != "" {
				if against != "" {
					return fmt.Errorf("use either --base or --against")
				}
				against = base
			}
			if head != "" && base == "" {
				return fmt.Errorf("--head requires --base")
			}
			description := ""
			if descriptionFile != "" {
				content, err := os.ReadFile(descriptionFile)
				if err != nil {
					return fmt.Errorf("the description file could not be read")
				}
				description = string(content)
			}
			folder, err := filepath.Abs(folder)
			if err != nil {
				return err
			}
			client := opts.client()
			repository, err := indexed(cmd.Context(), client, folder)
			if err != nil {
				return err
			}
			started, err := client.Review(cmd.Context(), agent.Ask{
				Repository:  repository,
				Against:     against,
				Head:        head,
				Description: description,
				// Named, so a list of reviews says where each came from.
				Title:    or(title, "From the terminal"),
				Skills:   skills,
				UseModel: !noModel,
			})
			if err != nil {
				return err
			}
			link := opts.agentURL + started.Path

			if noWait {
				if opts.asJSON {
					return writeJSON(cmd.OutOrStdout(), started)
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), link)
				return nil
			}

			// Before waiting, so a long review can be opened while it runs.
			if !opts.asJSON {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), link)
			}
			found, err := settled(cmd.Context(), client, started.ID)
			if err != nil {
				return err
			}
			if opts.asJSON {
				if err := writeJSON(cmd.OutOrStdout(), found); err != nil {
					return err
				}
			} else {
				report(cmd.OutOrStdout(), found)
			}
			if found.Status == agent.Failed {
				return fmt.Errorf("%s", refusal(found))
			}
			if !found.Review.Ready {
				return &unready{}
			}
			return nil
		},
	}
	command.Flags().StringVar(&against, "against", "", "Compare against this instead of the default branch")
	command.Flags().StringVar(&title, "title", "", "What this change is called")
	command.Flags().StringArrayVar(&skills, "skill", nil, "Read it against this skill, repeatable")
	command.Flags().BoolVar(&noWait, "no-wait", false, "Print the link and leave it running")
	command.Flags().BoolVar(&noModel, "no-model", false, "Say what changed without judging it")
	command.Flags().StringVar(&repoPath, "repo", "", "The checkout to review")
	command.Flags().StringVar(&base, "base", "", "Compare against this commit")
	command.Flags().StringVar(&head, "head", "", "Require a clean checkout at this full commit SHA")
	command.Flags().StringVar(&descriptionFile, "description-file", "", "Read the change description from this file")
	command.Flags().StringVar(&remote.endpoint, "reviewer", os.Getenv("SOURCEANT_REVIEW_URL"), "The remote snapshot review API URL")
	command.Flags().StringVar(&remote.repository, "repository", "", "Repository identity as owner/name")
	command.Flags().StringVar(&remote.diffFile, "diff-file", "", "Use a patch matching the committed comparison")
	command.Flags().StringVar(&remote.metadataFile, "pr-metadata", "", "Read title and body from pull request JSON")
	command.Flags().StringArrayVar(&remote.configuration, "review-option", nil, "Remote review option as label=value, repeatable")
	command.Flags().StringVar(&format, "format", "", "Output format: json or text")
	return command
}

// unready says the review found something blocking, which is an answer rather
// than a failure, so it exits non-zero without a second line about it.
type unready struct{}

func (e *unready) Error() string { return "" }

func (e *unready) code() int { return 2 }

func refusal(found agent.Reading) string {
	if found.Error != "" {
		return found.Error
	}
	return "the review failed and said nothing about why"
}

// indexed is the repository a folder belongs to.
func indexed(ctx context.Context, client *agent.Client, folder string) (string, error) {
	repositories, err := client.Repositories(ctx)
	if err != nil {
		return "", err
	}
	folder = resolved(folder)
	name, held := "", ""
	for _, repository := range repositories {
		path := resolved(repository.Path)
		if path != folder && !strings.HasPrefix(folder, path+string(os.PathSeparator)) {
			continue
		}
		// The innermost wins, for a checkout indexed inside another one.
		if len(path) > len(held) {
			name, held = repository.Name, path
		}
	}
	if name == "" {
		return "", fmt.Errorf("%s is not indexed on this machine. Add it with: sourceant repo add %s", folder, folder)
	}
	return name, nil
}

func resolved(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(real)
	}
	return filepath.Clean(path)
}

func settled(ctx context.Context, client *agent.Client, id string) (agent.Reading, error) {
	giveUp := time.Now().Add(patience)
	for {
		found, err := client.Reviewed(ctx, id)
		if err != nil {
			return found, err
		}
		if found.Status != agent.Running {
			return found, nil
		}
		if time.Now().After(giveUp) {
			return found, fmt.Errorf("this review is still running after %s. It carries on without us; the link has it", patience)
		}
		select {
		case <-ctx.Done():
			return found, ctx.Err()
		case <-time.After(beat):
		}
	}
}

// report prints the shape of the answer: where it looked, what it made of the
// change, and every finding.
func report(out io.Writer, found agent.Reading) {
	review := found.Review
	if found.Status == agent.Failed {
		return
	}

	_, _ = fmt.Fprintf(out, "\n%s\n", locate(review))
	if review.Note != "" {
		_, _ = fmt.Fprintln(out, review.Note)
	}
	if overview := review.Read.Summary.Overview; overview != "" {
		_, _ = fmt.Fprintf(out, "\n%s\n", overview)
	}

	findings := listed(review.Verdicts)
	if len(findings) > 0 {
		_, _ = fmt.Fprintln(out)
		rows := make([][]string, 0, len(findings))
		for _, finding := range findings {
			rows = append(rows, []string{finding.Severity, at(finding), finding.Detail})
		}
		presentation.Table(out, nil, rows)
	}

	_, _ = fmt.Fprintf(out, "\n%s\n", verdict(review, findings))
}

// locate says which branch was read and what it was read against, because
// origin/HEAD goes stale and a wrong base is otherwise invisible.
func locate(review agent.Review) string {
	where := review.Where
	against := where.Against
	if against == "" {
		against = "the default branch"
	}
	if where.Base != "" {
		against = fmt.Sprintf("%s (%s)", against, short(where.Base))
	}
	parts := []string{fmt.Sprintf("%s against %s", or(where.Branch, "this checkout"), against)}
	if len(review.Changed) > 0 {
		parts = append(parts, presentation.Count(len(review.Changed), "file", "files")+" changed")
	}
	if where.Commits > 0 {
		parts = append(parts, presentation.Count(where.Commits, "commit", "commits"))
	}
	return strings.Join(parts, ", ")
}

func verdict(review agent.Review, findings []agent.Finding) string {
	blocking := 0
	for _, finding := range findings {
		if finding.Severity == "blocking" {
			blocking++
		}
	}
	counts := []string{}
	if blocking > 0 {
		counts = append(counts, fmt.Sprintf("%d blocking", blocking))
	}
	if advisory := len(findings) - blocking; advisory > 0 {
		counts = append(counts, fmt.Sprintf("%d advisory", advisory))
	}
	if suggestions := len(review.Read.Suggestions); suggestions > 0 {
		counts = append(counts, presentation.Count(suggestions, "suggestion", "suggestions"))
	}

	answer := "Ready."
	if !review.Ready {
		answer = "Not ready."
	}
	if len(counts) == 0 {
		return answer
	}
	return answer + " " + strings.Join(counts, ", ") + "."
}

// listed is every finding across the skills, blocking first, then in the order
// somebody would read the files.
func listed(verdicts []agent.Verdict) []agent.Finding {
	findings := []agent.Finding{}
	for _, one := range verdicts {
		findings = append(findings, one.Findings...)
	}
	sort.SliceStable(findings, func(i, j int) bool {
		left, right := findings[i], findings[j]
		if (left.Severity == "blocking") != (right.Severity == "blocking") {
			return left.Severity == "blocking"
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		return line(left) < line(right)
	})
	return findings
}

func at(finding agent.Finding) string {
	if finding.Path == "" {
		return "the change"
	}
	if finding.Line == nil {
		return finding.Path
	}
	return fmt.Sprintf("%s:%d", finding.Path, *finding.Line)
}

func line(finding agent.Finding) int {
	if finding.Line == nil {
		return 0
	}
	return *finding.Line
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func or(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
