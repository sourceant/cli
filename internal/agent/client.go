// Package agent talks to the SourceAnt agent running on this machine.
//
// The CLI never reaches past the agent to the Python core. The agent is the
// process that is always up and the one that knows where the core landed;
// going around it would mean learning both.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
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
//
// IndexedAt is empty until it has been read, which is not the same as nothing
// having changed since, and Reading says a read is under way now.
type Repository struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	IndexedAt string `json:"indexed_at"`
	Reading   bool   `json:"reading"`
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

func IsConnectionRefused(err error) bool {
	var unreachable *Unreachable
	return errors.As(err, &unreachable) && errors.Is(unreachable.Cause, syscall.ECONNREFUSED)
}

// Client talks to one agent.
type Client struct {
	baseURL string
	http    *http.Client
	revive  *reviving
}

// New builds a client for the agent at baseURL.
func New(baseURL string, timeout time.Duration) *Client {
	client := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: timeout},
	}
	client.revive = &reviving{client: client}
	client.http.Transport = client.revive
	return client
}

// Starter brings the agent up and answers with the address it listens on.
type Starter func(ctx context.Context) (string, error)

// StartWith gives this client something to run when nothing answers.
func StartWith(client *Client, start Starter) { client.revive.start = start }

// reviving starts the agent once, for the first call that finds it absent, and
// sends that call again.
type reviving struct {
	client *Client
	start  Starter
	once   sync.Once
	url    string
	err    error
}

func (r *reviving) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err == nil || r.start == nil || !errors.Is(err, syscall.ECONNREFUSED) {
		return resp, err
	}
	r.once.Do(func() { r.url, r.err = r.start(req.Context()) })
	if r.err != nil {
		return nil, err
	}
	retry := req.Clone(req.Context())
	if r.url != "" {
		moved, parsed := url.Parse(r.url)
		if parsed != nil {
			return nil, err
		}
		retry.URL.Scheme, retry.URL.Host, retry.Host = moved.Scheme, moved.Host, ""
		r.client.baseURL = strings.TrimRight(r.url, "/")
	}
	if req.Body != nil {
		if req.GetBody == nil {
			return nil, err
		}
		body, again := req.GetBody()
		if again != nil {
			return nil, err
		}
		retry.Body = body
	}
	return http.DefaultTransport.RoundTrip(retry)
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

func (c *Client) Stop(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/stop", nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Sourceant-Client", "cli")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return &Unreachable{BaseURL: c.baseURL, Cause: err}
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil
	case http.StatusOK, http.StatusNotFound, http.StatusMethodNotAllowed:
		// Only 204 acknowledges shutdown; a generic 200 does not confirm it.
		return &Error{StatusCode: resp.StatusCode, Detail: "this agent does not support stop; update it with sourceant setup"}
	default:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		return &Error{StatusCode: resp.StatusCode, Detail: detail(body)}
	}
}
