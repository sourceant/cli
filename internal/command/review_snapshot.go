package command

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sourceant/cli/internal/agent"
	"github.com/spf13/cobra"
)

const snapshotLimit = 64 * 1024 * 1024

type snapshotOptions struct {
	endpoint, repository, base, head, diffFile, metadataFile, title string
	configuration                                                   []string
}

type reviewSnapshot struct {
	Repository    string            `json:"repository"`
	Base          string            `json:"base"`
	Head          string            `json:"head"`
	Diff          string            `json:"diff"`
	Files         map[string]string `json:"files"`
	Omitted       []string          `json:"omitted"`
	Title         string            `json:"title"`
	Description   string            `json:"description"`
	Configuration map[string]string `json:"configuration"`
}

type snapshotIdentity struct {
	Repository string   `json:"repository"`
	Base       string   `json:"base"`
	Head       string   `json:"head"`
	Omitted    []string `json:"omitted"`
}

type snapshotResult struct {
	Status        string            `json:"status"`
	Review        agent.Review      `json:"review"`
	Snapshot      snapshotIdentity  `json:"snapshot"`
	Configuration map[string]string `json:"configuration"`
}

func remoteReview(cmd *cobra.Command, opts *options, folder string, settings snapshotOptions) error {
	endpoint, err := url.Parse(settings.endpoint)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Path != "" && endpoint.Path != "/") {
		return fmt.Errorf("--host must be a server URL without credentials, an API path, query or fragment")
	}
	loopback := endpoint.Hostname() == "localhost"
	if ip := net.ParseIP(endpoint.Hostname()); ip != nil {
		loopback = ip.IsLoopback()
	}
	if endpoint.Scheme != "https" && !(endpoint.Scheme == "http" && loopback) {
		return fmt.Errorf("the reviewer API requires HTTPS")
	}
	endpoint.Path = "/api/reviews/snapshots"

	token := os.Getenv("SOURCEANT_REVIEW_TOKEN")
	if token == "" {
		return fmt.Errorf("SOURCEANT_REVIEW_TOKEN is required for a remote review")
	}
	limit := 14 * time.Minute
	if cmd.Flags().Changed("timeout") {
		limit = opts.timeout
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), limit)
	defer cancel()
	snapshot, err := checkoutSnapshot(ctx, folder, settings)
	if err != nil {
		return err
	}
	body, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("the snapshot could not be encoded")
	}
	if len(body) > snapshotLimit {
		return fmt.Errorf("the review snapshot exceeds 64 MiB")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("the review request could not be created")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if workspace := os.Getenv("SOURCEANT_REVIEW_WORKSPACE"); workspace != "" {
		request.Header.Set("X-SourceAnt-Workspace", workspace)
	}
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("the remote review request did not complete")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("the reviewer API refused the request (HTTP %d)", response.StatusCode)
	}
	encoded, err := io.ReadAll(io.LimitReader(response.Body, snapshotLimit+1))
	if err != nil || len(encoded) > snapshotLimit {
		return fmt.Errorf("the reviewer API returned an unreadable response")
	}
	var envelope struct {
		Status string         `json:"status"`
		Data   snapshotResult `json:"data"`
	}
	if json.Unmarshal(encoded, &envelope) != nil || envelope.Status != "success" || envelope.Data.Status != agent.Done {
		return fmt.Errorf("the reviewer API did not return a completed review")
	}
	result := envelope.Data
	if result.Snapshot.Repository != snapshot.Repository || result.Snapshot.Base != snapshot.Base || result.Snapshot.Head != snapshot.Head || result.Review.Base != snapshot.Base {
		return fmt.Errorf("the reviewer API returned a different snapshot")
	}
	if opts.asJSON {
		if err := writeJSON(cmd.OutOrStdout(), result); err != nil {
			return err
		}
	} else {
		report(cmd.OutOrStdout(), agent.Reading{Status: result.Status, Review: result.Review})
		if len(result.Snapshot.Omitted) != 0 {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "\nFiles omitted from the review snapshot:")
			for _, file := range result.Snapshot.Omitted {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  "+file)
			}
		}
	}
	if !result.Review.Ready {
		return &unready{}
	}
	return nil
}

func checkoutGit(ctx context.Context, folder string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", folder}, args...)...)
	command.Env = append(os.Environ(), "GIT_NO_REPLACE_OBJECTS=1", "GIT_OPTIONAL_LOCKS=0")
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("the checkout could not supply the requested commit comparison")
	}
	return output, nil
}

func checkoutSnapshot(ctx context.Context, folder string, settings snapshotOptions) (reviewSnapshot, error) {
	snapshot := reviewSnapshot{Repository: settings.repository, Base: settings.base, Head: settings.head, Files: map[string]string{}, Omitted: []string{}, Configuration: map[string]string{}, Title: settings.title}
	sha := regexp.MustCompile(`^[a-f0-9]{40}$`)
	if !sha.MatchString(settings.base) || !sha.MatchString(settings.head) || !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(settings.repository) {
		return snapshot, fmt.Errorf("remote review requires --repository owner/name and full --base and --head commit SHAs")
	}
	for _, option := range settings.configuration {
		label, value, found := strings.Cut(option, "=")
		if !found || label == "" || value == "" {
			return snapshot, fmt.Errorf("review options must be label=value")
		}
		if _, duplicate := snapshot.Configuration[label]; duplicate {
			return snapshot, fmt.Errorf("a review option was specified more than once")
		}
		snapshot.Configuration[label] = value
	}
	head, err := checkoutGit(ctx, folder, "rev-parse", "HEAD")
	if err != nil {
		return snapshot, err
	}
	if strings.TrimSpace(string(head)) != settings.head {
		return snapshot, fmt.Errorf("the checkout HEAD does not match --head")
	}
	status, err := checkoutGit(ctx, folder, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return snapshot, err
	}
	if len(status) != 0 {
		return snapshot, fmt.Errorf("commit tracked changes before submitting a remote snapshot")
	}
	diff, err := checkoutGit(ctx, folder, "diff", "--no-color", "--no-ext-diff", "--no-textconv", settings.base+"..."+settings.head)
	if err != nil {
		return snapshot, err
	}
	if len(diff) == 0 || len(diff) > 8*1024*1024 || !utf8.Valid(diff) {
		return snapshot, fmt.Errorf("the snapshot requires a nonempty UTF-8 diff of at most 8 MiB")
	}
	if settings.diffFile != "" {
		mounted, err := os.ReadFile(settings.diffFile)
		if err != nil || !bytes.Equal(mounted, diff) {
			return snapshot, fmt.Errorf("the supplied patch does not match the committed comparison")
		}
	}
	snapshot.Diff = string(diff)
	if settings.metadataFile != "" {
		encoded, err := os.ReadFile(settings.metadataFile)
		if err != nil {
			return snapshot, fmt.Errorf("pull request metadata could not be read")
		}
		var metadata struct{ NWO, Base, Head, Title, Body string }
		if json.Unmarshal(encoded, &metadata) != nil || metadata.NWO != settings.repository || metadata.Base != settings.base || metadata.Head != settings.head {
			return snapshot, fmt.Errorf("pull request metadata does not match the snapshot")
		}
		if snapshot.Title == "" {
			snapshot.Title = metadata.Title
		}
		snapshot.Description = metadata.Body
	}
	tree, err := checkoutGit(ctx, folder, "ls-tree", "-r", "-z", settings.head)
	if err != nil {
		return snapshot, err
	}
	batchContext, stopBatch := context.WithCancel(ctx)
	defer stopBatch()
	batch := exec.CommandContext(batchContext, "git", "-C", folder, "cat-file", "--batch")
	batch.Env = append(os.Environ(), "GIT_NO_REPLACE_OBJECTS=1", "GIT_OPTIONAL_LOCKS=0")
	input, err := batch.StdinPipe()
	if err != nil {
		return snapshot, err
	}
	output, err := batch.StdoutPipe()
	if err != nil {
		return snapshot, err
	}
	if err := batch.Start(); err != nil {
		return snapshot, fmt.Errorf("the snapshot objects could not be read")
	}
	defer func() { _ = input.Close(); stopBatch(); _ = batch.Wait() }()
	reader := bufio.NewReader(output)
	total := len(diff)
	entries := bytes.Split(tree, []byte{0})
	if len(entries) > 50001 {
		return snapshot, fmt.Errorf("the snapshot exceeds 50000 files")
	}
	for _, entry := range entries {
		if len(entry) == 0 {
			continue
		}
		info, file, ok := strings.Cut(string(entry), "\t")
		fields := strings.Fields(info)
		if !ok || len(fields) != 3 || !utf8.ValidString(file) || path.Clean(file) != file || strings.HasPrefix(file, "/") || strings.Contains(file, "\\") || strings.Contains("/"+file+"/", "/.git/") {
			return snapshot, fmt.Errorf("the snapshot contains an unsupported file path")
		}
		if fields[0] != "100644" && fields[0] != "100755" {
			snapshot.Omitted = append(snapshot.Omitted, file)
			continue
		}
		if _, err := io.WriteString(input, fields[2]+"\n"); err != nil {
			return snapshot, fmt.Errorf("a snapshot object could not be read")
		}
		header, err := reader.ReadString('\n')
		parts := strings.Fields(header)
		if err != nil || len(parts) != 3 || parts[1] != "blob" || parts[0] != fields[2] {
			return snapshot, fmt.Errorf("a snapshot object could not be read")
		}
		size, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil || size < 0 {
			return snapshot, fmt.Errorf("a snapshot object has an invalid size")
		}
		if size > 1_000_000 {
			if _, err := io.CopyN(io.Discard, reader, size+1); err != nil {
				return snapshot, fmt.Errorf("a snapshot object could not be read")
			}
			snapshot.Omitted = append(snapshot.Omitted, file)
			continue
		}
		content := make([]byte, int(size)+1)
		if _, err := io.ReadFull(reader, content); err != nil || content[len(content)-1] != '\n' {
			return snapshot, fmt.Errorf("a snapshot object could not be read")
		}
		content = content[:size]
		if bytes.ContainsRune(content, 0) || !utf8.Valid(content) {
			snapshot.Omitted = append(snapshot.Omitted, file)
			continue
		}
		total += len(content)
		if total > snapshotLimit {
			return snapshot, fmt.Errorf("the review snapshot exceeds 64 MiB")
		}
		snapshot.Files[file] = string(content)
	}
	return snapshot, nil
}
