// Package agent talks to the SourceAnt agent running on this machine.
//
// The CLI never reaches past the agent to the Python core. The agent is the
// process that is always up and the one that knows where the core landed;
// going around it would mean learning both.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Status is what the agent says about itself.
type Status struct {
	Version    string `json:"version"`
	CoreURL    string `json:"core_url"`
	CoreUp     bool   `json:"core_up"`
	CoreStarts int    `json:"core_starts"`
	LastExit   string `json:"last_exit,omitempty"`
}

// Repository is one repository indexed on this machine.
type Repository struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Node is one file, import or symbol.
type Node struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Kind   string   `json:"kind"`
	Labels []string `json:"labels"`
	Path   string   `json:"path"`
}

// Link is a typed edge between two nodes.
type Link struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}

// Graph is one repository's whole scope.
type Graph struct {
	Nodes     []Node `json:"nodes"`
	Links     []Link `json:"links"`
	Truncated bool   `json:"truncated"`
}

// Error is a non-2xx answer from the agent.
type Error struct {
	StatusCode int
	Detail     string
}

func (e *Error) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("the agent returned %d", e.StatusCode)
	}
	return e.Detail
}

// Unreachable says the agent is not running, or not where we looked.
type Unreachable struct {
	BaseURL string
	Cause   error
}

func (e *Unreachable) Error() string {
	return fmt.Sprintf("no agent answering at %s: %v", e.BaseURL, e.Cause)
}

func (e *Unreachable) Unwrap() error { return e.Cause }

// Client talks to one agent.
type Client struct {
	baseURL string
	http    *http.Client
}

// New builds a client for the agent at baseURL.
func New(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: timeout},
	}
}

// BaseURL is the agent this client talks to.
func (c *Client) BaseURL() string { return c.baseURL }

// Status asks the agent how it and the core are doing.
func (c *Client) Status(ctx context.Context) (Status, error) {
	return get[Status](ctx, c, "/health", nil)
}

// Repositories lists what is indexed on this machine.
func (c *Client) Repositories(ctx context.Context) ([]Repository, error) {
	return get[[]Repository](ctx, c, "/api/repositories", nil)
}

// GraphOptions narrows what a drawing covers.
type GraphOptions struct {
	PathPrefix   string
	IncludeTests bool
	NodeLimit    int
}

// Graph reads one repository's whole scope.
func (c *Client) Graph(ctx context.Context, repository string, opts GraphOptions) (Graph, error) {
	query := url.Values{"repository": {repository}}
	if opts.PathPrefix != "" {
		query.Set("path_prefix", opts.PathPrefix)
	}
	if opts.IncludeTests {
		query.Set("include_tests", "true")
	}
	if opts.NodeLimit > 0 {
		query.Set("node_limit", strconv.Itoa(opts.NodeLimit))
	}
	return get[Graph](ctx, c, "/api/graph", query)
}

func get[T any](ctx context.Context, c *Client, path string, query url.Values) (T, error) {
	var zero T
	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return zero, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return zero, &Unreachable{BaseURL: c.baseURL, Cause: err}
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return zero, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return zero, &Error{StatusCode: resp.StatusCode, Detail: detail(body)}
	}
	if err := json.Unmarshal(body, &zero); err != nil {
		return zero, fmt.Errorf("the agent answered %s with something other than JSON: %w", path, err)
	}
	return zero, nil
}

func detail(body []byte) string {
	var parsed struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Error != "" {
		return parsed.Error
	}
	return strings.TrimSpace(string(body))
}
