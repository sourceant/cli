package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Ask is what to review and how.
type Ask struct {
	Repository  string   `json:"repository"`
	Against     string   `json:"against"`
	Head        string   `json:"head,omitempty"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Skills      []string `json:"skills"`
	UseModel    bool     `json:"use_model"`
}

// Finding is one thing a skill says is wrong with a change.
type Finding struct {
	Detail   string `json:"detail"`
	Severity string `json:"severity"`
	Path     string `json:"path"`
	Line     *int   `json:"line"`
}

// Verdict is what one skill made of a change.
type Verdict struct {
	Skill    string    `json:"skill"`
	Passed   bool      `json:"passed"`
	Note     string    `json:"note"`
	Findings []Finding `json:"findings"`
}

// ChangedFile is one file the work touches, and what changed in it.
type ChangedFile struct {
	Path   string `json:"path"`
	Change string `json:"change"`
	Patch  string `json:"patch"`
}

// Commit is one commit the branch has that its base does not.
type Commit struct {
	SHA     string `json:"sha"`
	Author  string `json:"author"`
	At      string `json:"at"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// Skill is one rule the review read the change against.
type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Origin      string   `json:"origin"`
	Path        string   `json:"path"`
	Paths       []string `json:"paths"`
	Reviews     *bool    `json:"reviews"`
	Automatic   bool     `json:"automatic"`
}

// Recorded is one thing known about the repository being reviewed.
type Recorded struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Summary string `json:"summary"`
}

// Where is the checkout the review read, and what it was compared against.
type Where struct {
	Path    string `json:"path"`
	Branch  string `json:"branch"`
	Against string `json:"against"`
	Base    string `json:"base"`
	Commits int    `json:"commits"`
}

// Suggestion is one thing to change, and the code to put there.
type Suggestion struct {
	Path          string `json:"path"`
	StartLine     int    `json:"start_line"`
	EndLine       int    `json:"end_line"`
	Side          string `json:"side"`
	Comment       string `json:"comment"`
	Category      string `json:"category"`
	ExistingCode  string `json:"existing_code"`
	SuggestedCode string `json:"suggested_code"`
}

// Summary is the review in the order a person reads it.
type Summary struct {
	Overview         string   `json:"overview"`
	KeyImprovements  []string `json:"key_improvements"`
	MinorSuggestions []string `json:"minor_suggestions"`
	CriticalIssues   []string `json:"critical_issues"`
}

// Read is the review proper, from the same generator a pull request gets.
type Read struct {
	Verdict     string            `json:"verdict"`
	Summary     Summary           `json:"summary"`
	Suggestions []Suggestion      `json:"suggestions"`
	Notes       map[string]string `json:"notes"`
	Execution   json.RawMessage   `json:"execution,omitempty"`
}

// Review is whether a checkout's work is ready to be proposed to anyone.
type Review struct {
	Ready     bool          `json:"ready"`
	Note      string        `json:"note"`
	Base      string        `json:"base"`
	Where     Where         `json:"where"`
	Changed   []ChangedFile `json:"changed"`
	Commits   []Commit      `json:"commits"`
	Skills    []Skill       `json:"skills"`
	Knowledge []Recorded    `json:"knowledge"`
	Verdicts  []Verdict     `json:"verdicts"`
	Read      Read          `json:"review"`
}

// Reading is one review, whether it has finished or not.
type Reading struct {
	ID         string `json:"id"`
	Repository string `json:"repository"`
	Status     string `json:"status"`
	Title      string `json:"title"`
	Error      string `json:"error"`
	Started    string `json:"started"`
	Finished   string `json:"finished"`
	Review     Review `json:"review"`
	// Where to send somebody who wants to read it.
	Path string `json:"path"`
}

// What a review's status can be.
const (
	Running = "running"
	Done    = "done"
	Failed  = "failed"
)

// Review asks for a review and answers with where to find it, before the
// reading is done.
func (c *Client) Review(ctx context.Context, ask Ask) (Reading, error) {
	if ask.Skills == nil {
		ask.Skills = []string{}
	}
	return post[Reading](ctx, c, "/api/reviews", ask)
}

// Reviewed is one review by name, however long ago it ran.
func (c *Client) Reviewed(ctx context.Context, id string) (Reading, error) {
	return get[Reading](ctx, c, "/api/reviews/"+url.PathEscape(id), nil)
}

func post[T any](ctx context.Context, c *Client, path string, body any) (T, error) {
	var zero T
	encoded, err := json.Marshal(body)
	if err != nil {
		return zero, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return zero, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return zero, &Unreachable{BaseURL: c.baseURL, Cause: err}
	}
	defer func() { _ = resp.Body.Close() }()

	answer, err := io.ReadAll(resp.Body)
	if err != nil {
		return zero, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return zero, &Error{StatusCode: resp.StatusCode, Detail: detail(answer)}
	}
	if err := json.Unmarshal(answer, &zero); err != nil {
		return zero, fmt.Errorf("the agent answered %s with something other than JSON: %w", path, err)
	}
	return zero, nil
}
